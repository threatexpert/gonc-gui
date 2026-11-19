// Package receivecompare implements an opt-in, metadata-only local difference
// scan. Results and selections stay in Go; the UI receives bounded pages.
package receivecompare

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
	"gonc-gui/internal/httpdownload"
)

const PageSize = 200
const LocalOnly = "localOnly"
const LocalMissing = "localMissing"
const SizeDifferent = "sizeDifferent"
const Differences = "differences"
const maxIndexBytes = 256 << 20 // conservative accounting; fail closed on oversized jobs

type Item struct {
	LinkTarget        string `json:"linkTarget,omitempty"`
	PreserveLink      bool   `json:"preserveLink,omitempty"`
	PermissionsDiffer bool   `json:"permissionsDiffer,omitempty"`
	ID                int    `json:"id"`
	Path              string `json:"path"`
	Size              int64  `json:"size"`
	LocalSize         *int64 `json:"localSize"`
	RemoteSize        *int64 `json:"remoteSize"`
	Modified          string `json:"modified"`
	LocalModified     string `json:"localModified"`
	RemoteModified    string `json:"remoteModified"`
	Selected          bool   `json:"selected"`
	Outcome           string `json:"outcome"`
	Error             string `json:"error"`
}

type ActiveReceive struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	Checking   bool   `json:"checking"`
	DoneBytes  int64  `json:"doneBytes"`
	TotalBytes int64  `json:"totalBytes"`
}

type Snapshot struct {
	BatchTotal             int             `json:"batchTotal"`
	BatchFinished          int             `json:"batchFinished"`
	ActiveReceives         []ActiveReceive `json:"activeReceives"`
	ID                     string          `json:"id"`
	Root                   string          `json:"root"`
	Scope                  string          `json:"scope"`
	Kind                   string          `json:"kind"`
	Phase                  string          `json:"phase"`
	Error                  string          `json:"error"`
	Remote                 int             `json:"remote"`
	Scanned                int             `json:"scanned"`
	Candidates             int             `json:"candidates"`
	Bytes                  int64           `json:"bytes"`
	Unchecked              int             `json:"unchecked"`
	Warnings               []string        `json:"warnings"`
	Matched                int             `json:"matched"`
	Selected               int             `json:"selected"`
	SelectedBytes          int64           `json:"selectedBytes"`
	SelectedLocalOnly      int             `json:"selectedLocalOnly"`
	SelectedLocalOnlyBytes int64           `json:"selectedLocalOnlyBytes"`
	SelectedReceive        int             `json:"selectedReceive"`
	SelectedReceiveBytes   int64           `json:"selectedReceiveBytes"`
	Deleted                int             `json:"deleted"`
	Received               int             `json:"received"`
	Skipped                int             `json:"skipped"`
	Failed                 int             `json:"failed"`
	DeleteMode             string          `json:"deleteMode"`
	Page                   int             `json:"page"`
	Items                  []Item          `json:"items"`
}

type entry struct {
	Item
	info              os.FileInfo
	file              httpdownload.FileInfo
	finalizeDirectory bool
}
type remotePath struct {
	isDir  bool
	listed bool
}

type Job struct {
	activeReceives map[string]*ActiveReceive
	mu             sync.Mutex
	state          Snapshot
	entries        []entry
	rootInfo       os.FileInfo
	remote         map[string]remotePath // all paths, including directories and implicit ancestors
	links          map[string]bool
	remoteFiles    []httpdownload.FileInfo
	indexBytes     int
	query          string
	matched        []int
	queryValid     bool
	cancel         context.CancelFunc
}

func New(id, root, scope, mode string, kinds ...string) (*Job, error) {
	kind := LocalOnly
	if len(kinds) > 0 {
		kind = kinds[0]
	}
	if kind != LocalOnly && kind != LocalMissing && kind != SizeDifferent && kind != Differences {
		return nil, errors.New("invalid comparison purpose")
	}
	if root == "" {
		return nil, errors.New("select a save directory")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("save location is not a directory")
	}
	scope, err = cleanRemote(scope)
	if err != nil {
		return nil, err
	}
	return &Job{state: Snapshot{ID: id, Root: abs, Scope: scope, Kind: kind, Phase: "remote", DeleteMode: mode, Warnings: []string{}, Items: []Item{}}, rootInfo: info, links: make(map[string]bool), remote: make(map[string]remotePath)}, nil
}

func cleanRemote(p string) (string, error) {
	if p == "" {
		p = "/"
	}
	if strings.ContainsAny(p, "\\\x00:") {
		return "", fmt.Errorf("unsupported comparison path: %q", p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", errors.New("comparison path must stay within the save directory")
		}
	}
	return path.Clean("/" + strings.TrimPrefix(p, "/")), nil
}

func key(p string) string {
	if runtime.GOOS == "windows" && (strings.Contains(p, "./") || strings.Contains(p, " /") || strings.HasSuffix(p, ".") || strings.HasSuffix(p, " ")) {
		parts := strings.Split(p, "/")
		for i := range parts {
			parts[i] = strings.TrimRight(parts[i], " .")
		}
		p = strings.Join(parts, "/")
	}
	// Conservative on macOS: a case-sensitive volume can omit a candidate, but
	// a case-insensitive volume must never offer an alias of a remote file.
	if runtime.GOOS == "darwin" {
		p = norm.NFC.String(p)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}

func (j *Job) Scope() string { return j.state.Scope }
func (j *Job) Root() string  { return j.state.Root }

func (j *Job) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancel != nil {
		j.cancel()
	}
}

func (j *Job) Scan(ctx context.Context, list func(context.Context, func(httpdownload.FileInfo) error) error) {
	ctx, cancel := context.WithCancel(ctx)
	j.mu.Lock()
	j.cancel = cancel
	j.mu.Unlock()
	defer cancel()
	err := list(ctx, func(file httpdownload.FileInfo) error {
		p, err := cleanRemote(file.Path)
		if err != nil {
			return err
		}
		j.mu.Lock()
		defer j.mu.Unlock()
		j.state.Remote++
		// Keep the scoped index small even when the sender lists its entire tree.
		scope := key(j.state.Scope)
		if scope != "/" && key(p) != scope && !strings.HasPrefix(key(p), scope+"/") {
			return ctx.Err()
		}
		if file.LinkTarget != "" {
			j.links[key(p)] = file.PreserveLink() || file.Unavailable()
		}
		if file.Unavailable() {
			j.state.Unchecked++
			if len(j.state.Warnings) < 20 {
				j.state.Warnings = append(j.state.Warnings, p+": link target unavailable")
			}
		}
		compareDirMode := file.Mode != nil && p != "/" && (j.state.Kind == Differences || j.state.Kind == LocalMissing)
		if j.state.Kind != LocalOnly && !file.Unavailable() && (!file.IsDir || file.LinkTarget != "" || compareDirMode) {
			if file.Size < 0 {
				return errors.New("remote file size is missing or invalid")
			}
			if j.remote[key(p)].listed {
				return fmt.Errorf("ambiguous remote file path: %s", p)
			}
			file.Path = p
			j.indexBytes += len(p) + 192
			j.remoteFiles = append(j.remoteFiles, file)
		}
		listed := true
		for {
			k := key(p)
			if _, ok := j.remote[k]; !ok {
				j.indexBytes += len(k) + 128
				if j.indexBytes > maxIndexBytes {
					return errors.New("comparison index too large; compare a smaller subfolder")
				}
				j.remote[k] = remotePath{isDir: file.IsDir, listed: listed}
			} else if j.remote[k].isDir != file.IsDir {
				return fmt.Errorf("conflicting remote paths: %s", p)
			} else if listed {
				j.remote[k] = remotePath{isDir: file.IsDir, listed: true}
			}
			if p == "/" {
				break
			}
			listed = false
			p = path.Dir(p)
			file.IsDir = true
		}
		return ctx.Err()
	})
	if err == nil {
		j.mu.Lock()
		j.state.Phase = "local"
		j.mu.Unlock()
		if j.state.Kind == LocalOnly {
			err = j.walk(ctx, j.state.Scope)
		} else {
			err = j.scanRemoteFiles(ctx)
			if err == nil && j.state.Kind == Differences {
				err = j.walk(ctx, j.state.Scope)
			}
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.links = nil
	j.remote = nil // release the large index before the review/delete phase
	j.remoteFiles = nil
	j.cancel = nil
	switch {
	case errors.Is(err, context.Canceled):
		j.state.Phase = "cancelled"
	case err != nil:
		j.state.Phase = "error"
		j.state.Error = err.Error()
	default:
		j.state.Phase = "ready"
	}
}

func (j *Job) warn(p string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.state.Unchecked++
	if len(j.state.Warnings) < 20 {
		j.state.Warnings = append(j.state.Warnings, p+": "+err.Error())
	}
}

func (j *Job) walk(ctx context.Context, remoteDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if remote, exists := j.remote[key(remoteDir)]; exists && !remote.isDir {
		j.warn(remoteDir, errors.New("remote file/local directory conflict"))
		return nil
	}
	full := filepath.Join(j.state.Root, filepath.FromSlash(strings.TrimPrefix(remoteDir, "/")))
	if err := j.checkParents(full); err != nil {
		if j.state.Kind == Differences && os.IsNotExist(err) {
			return nil // the entire scope can be missing locally
		}
		return err
	}
	f, err := os.Open(full)
	if err != nil {
		j.warn(remoteDir, err)
		return nil
	}
	defer f.Close()
	for {
		batch, readErr := f.ReadDir(256) // no full-directory sort/allocation
		for _, de := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			p := path.Join(remoteDir, de.Name())
			if _, err := cleanRemote(p); err != nil {
				j.warn(p, err)
				continue
			}
			if opaque, linked := j.links[key(p)]; opaque || linked && de.Type()&os.ModeSymlink != 0 {
				continue
			}
			if de.Type()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				j.warn(p, errors.New("link or special file skipped"))
				continue
			}
			if de.IsDir() {
				if remote, exists := j.remote[key(p)]; exists && !remote.isDir {
					j.warn(p, errors.New("remote file/local directory conflict"))
					continue
				}
				if err := j.walk(ctx, p); err != nil {
					return err
				}
				continue
			}
			j.mu.Lock()
			if _, exists := j.remote[key(p)]; j.state.Kind != Differences || !exists {
				j.state.Scanned++
			}
			j.mu.Unlock()
			if _, exists := j.remote[key(p)]; exists {
				continue
			}
			info, err := de.Info()
			if err != nil {
				j.warn(p, err)
				continue
			}
			if !info.Mode().IsRegular() {
				j.warn(p, errors.New("special file skipped"))
				continue
			}
			j.mu.Lock()
			j.indexBytes += len(p) + 768
			if j.indexBytes > maxIndexBytes {
				j.mu.Unlock()
				return errors.New("comparison index too large; compare a smaller subfolder")
			}
			size := info.Size()
			j.entries = append(j.entries, entry{Item: Item{ID: len(j.entries), Path: p, Size: size, LocalSize: &size, Modified: info.ModTime().Format(time.RFC3339Nano), LocalModified: info.ModTime().Format(time.RFC3339Nano)}, info: info})
			j.state.Candidates++
			j.state.Bytes += info.Size()
			j.queryValid = false
			j.mu.Unlock()
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			j.warn(remoteDir, readErr)
			return nil
		}
	}
}

// Do not follow links/junctions on either the initial walk or final deletion.
func (j *Job) checkParents(full string) error {
	root, err := os.Lstat(j.state.Root)
	if err != nil {
		return err
	}
	if root.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || !os.SameFile(root, j.rootInfo) {
		return errors.New("save directory changed")
	}
	rel, err := filepath.Rel(j.state.Root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("path escapes save directory")
	}
	p := j.state.Root
	if rel == "." {
		return nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, part)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return errors.New("linked path skipped")
		}
	}
	return nil
}

func (j *Job) filter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	if j.queryValid && j.query == query {
		return
	}
	j.query = query
	j.matched = j.matched[:0]
	for i := range j.entries {
		if strings.Contains(strings.ToLower(j.entries[i].Path), query) {
			j.matched = append(j.matched, i)
		}
	}
	j.queryValid = true
}

func (j *Job) Page(query string, page int) Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.state
	s.ActiveReceives = make([]ActiveReceive, 0, len(j.activeReceives))
	for _, active := range j.activeReceives {
		s.ActiveReceives = append(s.ActiveReceives, *active)
	}
	sort.Slice(s.ActiveReceives, func(i, k int) bool { return s.ActiveReceives[i].ID < s.ActiveReceives[k].ID })
	s.Warnings = append([]string{}, j.state.Warnings...)
	s.Items = []Item{}
	if s.Phase == "remote" || s.Phase == "local" {
		return s
	}
	j.filter(query)
	s.Matched = len(j.matched)
	if page < 0 {
		page = 0
	}
	maxPage := (len(j.matched) - 1) / PageSize
	if page > maxPage {
		page = maxPage
	}
	s.Page = page
	for i := page * PageSize; i < len(j.matched) && len(s.Items) < PageSize; i++ {
		s.Items = append(s.Items, j.entries[j.matched[i]].Item)
	}
	return s
}

func (j *Job) Select(query string, ids []int, all, selected bool) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state.Phase != "ready" && j.state.Phase != "done" {
		return errors.New("comparison is not ready")
	}
	if all {
		j.filter(query)
		ids = j.matched
	}
	for _, id := range ids {
		if id < 0 || id >= len(j.entries) {
			continue
		}
		e := &j.entries[id]
		if e.Outcome == "deleted" || e.Outcome == "received" || e.Outcome == "skipped" || e.Selected == selected {
			continue
		}
		j.setSelected(e, selected)
	}
	return nil
}

// Caller holds mu. Keep action counts incremental, including across pages.
func (j *Job) setSelected(e *entry, selected bool) {
	if e.Selected == selected {
		return
	}
	e.Selected = selected
	delta := 1
	if !selected {
		delta = -1
	}
	j.state.Selected += delta
	j.state.SelectedBytes += int64(delta) * e.Size
	if e.RemoteSize == nil {
		j.state.SelectedLocalOnly += delta
		j.state.SelectedLocalOnlyBytes += int64(delta) * e.Size
	} else {
		j.state.SelectedReceive += delta
		j.state.SelectedReceiveBytes += int64(delta) * e.Size
	}
}

func (j *Job) BeginDelete(ctx context.Context, mode string) (context.Context, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state.Kind != LocalOnly && j.state.Kind != Differences {
		return nil, errors.New("deletion is only available for local-only files")
	}
	if j.state.Phase != "ready" && j.state.Phase != "done" {
		return nil, errors.New("comparison is not ready")
	}
	if mode != "trash" && mode != "permanent" {
		return nil, errors.New("invalid deletion mode")
	}
	if j.state.SelectedLocalOnly == 0 {
		return nil, errors.New("no files selected")
	}
	ctx, j.cancel = context.WithCancel(ctx)
	j.state.Phase = "deleting"
	j.state.Error = ""
	return ctx, nil
}

// Four workers bound verification requests and filesystem operations. Results
// are retained per file; cancellation never erases completed outcomes.
func (j *Job) Delete(ctx context.Context, missing func(context.Context, string) (bool, error), remove func(string) error) {
	queue := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range queue {
				j.deleteOne(ctx, id, missing, remove)
			}
		}()
	}
	for id := range j.entries {
		j.mu.Lock()
		selected := j.entries[id].Selected && j.entries[id].RemoteSize == nil
		j.mu.Unlock()
		if !selected {
			continue
		}
		select {
		case queue <- id:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(queue)
	wg.Wait()
	j.mu.Lock()
	defer j.mu.Unlock()
	if ctx.Err() != nil {
		j.state.Error = "deletion cancelled; completed actions are preserved"
	}
	j.state.Phase = "done"
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
}

func (j *Job) deleteOne(ctx context.Context, id int, missing func(context.Context, string) (bool, error), remove func(string) error) {
	if ctx.Err() != nil {
		return
	}
	j.mu.Lock()
	e := j.entries[id]
	j.mu.Unlock()
	outcome := "failed"
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	absent, err := missing(checkCtx, e.Path)
	cancel()
	if ctx.Err() != nil {
		return
	}
	if err == nil && !absent {
		outcome = "skipped"
		err = errors.New("path exists on the sender")
	}
	if err == nil {
		full := filepath.Join(j.state.Root, filepath.FromSlash(strings.TrimPrefix(e.Path, "/")))
		err = j.checkParents(full)
		if err == nil {
			var info os.FileInfo
			info, err = os.Lstat(full)
			if err == nil && (!info.Mode().IsRegular() || !os.SameFile(e.info, info) || info.Size() != e.info.Size() || !info.ModTime().Equal(e.info.ModTime())) {
				err = errors.New("local file changed since comparison")
			}
		}
		if err != nil {
			outcome = "skipped"
		} else if ctx.Err() == nil {
			err = remove(full)
			if err == nil {
				outcome = "deleted"
			}
		} else {
			return
		}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	current := &j.entries[id]
	// Failed rows remain selectable for an explicit retry (including changing
	// from recycle to permanent deletion); successes are never retried.
	if current.Outcome == "failed" {
		j.state.Failed--
	}
	current.Outcome = outcome
	current.Error = ""
	if err != nil {
		current.Error = err.Error()
	}
	j.setSelected(current, false)
	switch outcome {
	case "deleted":
		j.state.Deleted++
	case "skipped":
		j.state.Skipped++
	default:
		j.state.Failed++
	}
}
