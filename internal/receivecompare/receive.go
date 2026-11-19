package receivecompare

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gonc-gui/internal/httpdownload"
)

// Probe remote files and cache parent checks
// so a directory with many files is not re-statted for every descendant.
// Missing parents are valid; inaccessible parents and links are not missing files.
func (j *Job) scanRemoteFiles(ctx context.Context) error {
	parents := make(map[string]error)
	var parentOK func(string) error
	parentOK = func(dir string) error {
		if err, ok := parents[dir]; ok {
			return err
		}
		if dir != j.state.Root {
			if err := parentOK(filepath.Dir(dir)); err != nil {
				parents[dir] = err
				return err
			}
		}
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) && dir != j.state.Root {
			err = nil
		} else if err == nil && (!info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0) {
			err = errors.New("linked path or file/directory conflict skipped")
		}
		if err == nil && dir == j.state.Root && !os.SameFile(info, j.rootInfo) {
			err = errors.New("save directory changed")
		}
		parents[dir] = err
		return err
	}
	for _, file := range j.remoteFiles {
		if err := ctx.Err(); err != nil {
			return err
		}
		full, err := httpdownload.ResolveLocalPath(j.state.Root, file)
		if err != nil {
			return err
		}
		j.mu.Lock()
		j.state.Scanned++
		j.mu.Unlock()
		if err = parentOK(filepath.Dir(full)); err != nil {
			j.warn(file.Path, err)
			continue
		}
		info, err := os.Lstat(full)
		missing := os.IsNotExist(err)
		if err != nil && !missing {
			j.warn(file.Path, err)
			continue
		}
		if !missing && !info.Mode().IsRegular() {
			j.warn(file.Path, errors.New("link or file/directory conflict skipped"))
			continue
		}
		timeDifferent := false
		if j.state.Kind == Differences && !missing {
			if file.ModTime.IsZero() {
				j.warn(file.Path, errors.New("sender modification time unavailable; time comparison skipped"))
			} else {
				// Preserve all precision supplied by the list and filesystem.
				// Equal compares instants, independently of timezone formatting.
				timeDifferent = !info.ModTime().Equal(file.ModTime)
			}
		}
		match := j.state.Kind == LocalMissing && missing ||
			j.state.Kind == SizeDifferent && !missing && info.Size() != file.Size ||
			j.state.Kind == Differences && (missing || info.Size() != file.Size || timeDifferent)
		if !match {
			continue
		}
		item := Item{Path: file.Path, Size: file.Size, RemoteSize: &file.Size}
		if !file.ModTime.IsZero() {
			item.Modified = file.ModTime.Format(time.RFC3339Nano)
			item.RemoteModified = item.Modified
		}
		if !missing {
			size := info.Size()
			item.LocalSize = &size
			item.LocalModified = info.ModTime().Format(time.RFC3339Nano)
		}
		j.mu.Lock()
		j.indexBytes += len(file.Path) + 768
		if j.indexBytes > maxIndexBytes {
			j.mu.Unlock()
			return errors.New("comparison index too large; compare a smaller subfolder")
		}
		item.ID = len(j.entries)
		j.entries = append(j.entries, entry{Item: item, info: info, file: file})
		j.state.Candidates++
		j.state.Bytes += file.Size
		j.queryValid = false
		j.mu.Unlock()
	}
	return nil
}

func (j *Job) BeginReceive(ctx context.Context) (context.Context, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state.Kind != LocalMissing && j.state.Kind != SizeDifferent && j.state.Kind != Differences {
		return nil, errors.New("receiving is not available for local-only files")
	}
	if j.state.Phase != "ready" && j.state.Phase != "done" {
		return nil, errors.New("comparison is not ready")
	}
	if j.state.SelectedReceive == 0 {
		return nil, errors.New("no files selected")
	}
	ctx, j.cancel = context.WithCancel(ctx)
	j.state.Phase = "receiving"
	j.state.Error = ""
	return ctx, nil
}

// Each selected file uses the existing resumable/repairing HTTP downloader.
// The fixed worker pool keeps selection lists off the JS bridge and limits I/O.
func (j *Job) Receive(ctx context.Context, refresh func(context.Context, httpdownload.FileInfo) (httpdownload.FileInfo, error), receive func(context.Context, string, httpdownload.FileInfo) error) {
	queue := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range queue {
				j.receiveOne(ctx, id, refresh, receive)
			}
		}()
	}
	for id := range j.entries {
		j.mu.Lock()
		selected := j.entries[id].Selected && j.entries[id].RemoteSize != nil
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
		j.state.Error = "receiving cancelled; completed actions are preserved"
	}
	j.state.Phase = "done"
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
}

// Check the root identity and every existing parent again before writing.
// Missing parents can be created by the downloader; links are never followed.
func (j *Job) receiveTarget(file httpdownload.FileInfo) (string, error) {
	full, err := httpdownload.ResolveLocalPath(j.state.Root, file)
	if err != nil {
		return "", err
	}
	if err = j.checkParents(j.state.Root); err != nil {
		return "", err
	}
	rel, _ := filepath.Rel(j.state.Root, filepath.Dir(full))
	p := j.state.Root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			p = filepath.Join(p, part)
			info, err := os.Lstat(p)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return "", err
			}
			if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return "", errors.New("linked path or file/directory conflict skipped")
			}
		}
	}
	return full, nil
}

func (j *Job) receiveOne(ctx context.Context, id int, refresh func(context.Context, httpdownload.FileInfo) (httpdownload.FileInfo, error), receive func(context.Context, string, httpdownload.FileInfo) error) {
	if ctx.Err() != nil {
		return
	}
	j.mu.Lock()
	e := j.entries[id]
	j.mu.Unlock()
	fresh, err := refresh(ctx, e.file)
	if ctx.Err() != nil {
		return
	}
	outcome := "failed"
	full := ""
	if err == nil {
		outcome = "skipped"
		// Keep the original path: metadata refresh cannot redirect a selection.
		fresh.Path = e.file.Path
		full, err = j.receiveTarget(e.file)
	}
	if err == nil {
		info, statErr := os.Lstat(full)
		if e.info == nil {
			if !os.IsNotExist(statErr) {
				err = errors.New("local file appeared since comparison")
			}
		} else if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(e.info, info) || info.Size() != e.info.Size() || !info.ModTime().Equal(e.info.ModTime()) {
			err = errors.New("local file changed since comparison")
		}
	}
	attempted := false
	if err == nil {
		if ctx.Err() != nil {
			return
		}
		attempted = true
		err = receive(ctx, j.state.Root, fresh)
		outcome = "received"
		if err != nil {
			outcome = "failed"
		}
	}
	var partial os.FileInfo
	if attempted && err != nil {
		partial, _ = os.Lstat(full)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	current := &j.entries[id]
	if current.Outcome == "failed" {
		j.state.Failed--
	}
	current.Outcome = outcome
	current.Error = ""
	if err != nil {
		current.Error = fmt.Sprint(err)
	}
	if partial != nil && partial.Mode().IsRegular() {
		current.info = partial
	} // allow resuming our own interrupted write
	j.setSelected(current, false)
	switch outcome {
	case "received":
		j.state.Received++
	case "skipped":
		j.state.Skipped++
	default:
		j.state.Failed++
	}
}
