package httpdownload

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/threatexpert/gonc/v2/httpfileshare"
)

var errLocalConflict = errors.New("local type conflict")

func (f FileInfo) PreserveLink() bool { return f.action == httpfileshare.ReceiveLink }
func (f FileInfo) Unavailable() bool  { return f.LinkTarget != "" && f.Size < 0 }

// Probe the actual destination. In particular Windows directory links need a
// type at creation even if their targets have not been received.
func supportsLinks(root string) bool {
	dir, err := os.MkdirTemp(root, ".gonc-link-probe-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	return createReceiverSymlink("missing", filepath.Join(dir, "file"), false) == nil &&
		createReceiverSymlink("missing-dir", filepath.Join(dir, "directory"), true) == nil
}

type fetchTree func(context.Context, string) ([]FileInfo, error)

func planLinks(ctx context.Context, root string, files []FileInfo, include []string, fetch fetchTree, skip func(string, error)) ([]FileInfo, error) {
	needsPlan := false
	for _, f := range files {
		if f.LinkTarget != "" && f.action == "" {
			needsPlan = true
			break
		}
	}
	if !needsPlan {
		return files, nil
	}
	canLink := supportsLinks(root)
	// Ignore cached descendants under directory links. The expansion request
	// below provides their authoritative listing when materializing a directory.
	dirLinks := map[string]bool{}
	for _, f := range files {
		if f.IsDir && f.LinkTarget != "" {
			dirLinks[f.Path] = true
		}
	}
	if len(dirLinks) > 0 {
		kept := files[:0]
		for _, f := range files {
			hidden := false
			for parent := path.Dir(f.Path); parent != "/" && parent != "."; parent = path.Dir(parent) {
				if dirLinks[parent] {
					hidden = true
					break
				}
			}
			if !hidden {
				kept = append(kept, f)
			}
		}
		files = kept
	}
	skipped := map[string]bool{}
	for first, depth := 0, 0; first < len(files); depth++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		input := make([]httpfileshare.FileInfo, len(files))
		for i, f := range files {
			input[i] = httpfileshare.FileInfo{Path: f.Path, IsDir: f.IsDir, LinkTarget: f.LinkTarget}
		}
		plan, err := httpfileshare.PlanReceive(input)
		if err != nil {
			return nil, err
		}
		end := len(files)
		for i := first; i < end; i++ {
			f := &files[i]
			f.action = plan[i].Action
			if f.Unavailable() {
				continue
			}
			// A user can browse into a directory link and select only one of its children.
			// Expand that directory so the selected child has a real destination.
			selectedChild := false
			if f.IsDir && f.PreserveLink() && !shouldIncludePath(f.Path, include, nil) {
				for _, name := range include {
					if strings.HasPrefix(name, f.Path+"/") {
						selectedChild = true
						break
					}
				}
			}
			if f.PreserveLink() && (!canLink || selectedChild) {
				if f.IsDir {
					f.action = httpfileshare.ReceiveDirectory
				} else {
					f.action = httpfileshare.ReceiveContent
				}
			}
			if f.LinkTarget == "" || !f.IsDir || f.PreserveLink() {
				continue
			}
			var children []FileInfo
			if depth >= 64 {
				err = fmt.Errorf("%w: directory link expansion too deep", errListStatus)
			} else {
				children, err = fetch(ctx, f.Path)
			}
			if err == nil {
				found := false
				for _, child := range children {
					if child.Path == f.Path && child.IsDir && child.LinkTarget == "" && !found {
						found = true
						continue
					}
					if !strings.HasPrefix(child.Path, f.Path+"/") || path.Clean(child.Path) != child.Path {
						err = fmt.Errorf("invalid expanded path %q", child.Path)
						break
					}
				}
				if !found && err == nil {
					err = errors.New("directory listing missing its root")
				}
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if skip == nil {
					if errors.Is(err, errListStatus) {
						f.Size = -1
						continue
					}
					return nil, fmt.Errorf("expand %s: %w", f.Path, err)
				}
				skip(f.Path, err)
				skipped[f.Path] = true
				continue
			}
			parent := f.Path
			for _, child := range children {
				if child.Path != parent {
					files = append(files, child)
				}
			}
		}
		first = end
	}
	if len(skipped) > 0 {
		kept := files[:0]
		for _, f := range files {
			if !skipped[f.Path] {
				kept = append(kept, f)
			}
		}
		files = kept
	}
	return files, nil
}

// Retain only the comparison scope. Any listing error must keep deletion and
// repair unavailable; an unreadable subtree is not evidence of a local extra.
func ListComparisonScope(ctx context.Context, serverURL, scope, root string, visit func(FileInfo) error) error {
	var budget int
	read := func(ctx context.Context, name string) ([]FileInfo, error) {
		var entries []FileInfo
		err := readComparisonList(ctx, serverURL, name, func(f FileInfo) error {
			budget += len(f.Path) + len(f.LinkTarget) + len(f.Name) + 320
			if budget > 256<<20 {
				return errors.New("comparison index too large; compare a smaller subfolder")
			}
			entries = append(entries, f)
			return nil
		})
		return entries, err
	}
	files, err := read(ctx, scope)
	if errors.Is(err, errListNotFound) && scope != "/" {
		// Some senders only list from the root. A missing scope can also
		// legitimately contain local-only files.
		err = ListForComparison(ctx, serverURL, func(f FileInfo) error {
			if f.Path == scope || strings.HasPrefix(f.Path, scope+"/") {
				budget += len(f.Path) + len(f.LinkTarget) + len(f.Name) + 320
				if budget > 256<<20 {
					return errors.New("comparison index too large; compare a smaller subfolder")
				}
				files = append(files, f)
			}
			return nil
		})
	}
	if err != nil {
		return err
	}
	files, err = planLinks(ctx, root, files, nil, read, nil)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := visit(f); err != nil {
			return err
		}
	}
	return nil
}

func checkParents(root, full string) error {
	for p := filepath.Dir(full); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if info != nil && (!info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0) {
			return fmt.Errorf("%w: parent %s", errLocalConflict, p)
		}
		if p == root {
			return nil
		}
		if p == filepath.Dir(p) {
			return fmt.Errorf("%w: outside save directory", errLocalConflict)
		}
	}
}

func (d *Downloader) contentOverLink(ctx context.Context, client *http.Client, f FileInfo, full string, sink Sink) error {
	dir, err := os.MkdirTemp(filepath.Dir(full), ".gonc-content-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	u, err := resolveURL(d.cfg.ServerURL, f.Path)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "file")
	if err := d.downloadFullFile(ctx, client, u, tmp, f, false, sink); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

func (d *Downloader) receiveLink(ctx context.Context, client *http.Client, f FileInfo, full string, info os.FileInfo, sink Sink) error {
	if info != nil {
		if info.Mode()&os.ModeSymlink != 0 {
			old, err := os.Readlink(full)
			if err != nil {
				return err
			}
			if filepath.ToSlash(old) == f.LinkTarget {
				d.progress.skippedFiles.Add(1)
				return nil
			}
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %s", errLocalConflict, full)
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(full), ".gonc-link-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	tmp := filepath.Join(dir, "link")
	if err := createReceiverSymlink(filepath.FromSlash(f.LinkTarget), tmp, f.IsDir); err != nil {
		if f.IsDir {
			return err
		}
		f.action = httpfileshare.ReceiveContent
		d.progress.totalBytes.Add(f.Size)
		return d.downloadOne(ctx, client, f, sink)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}
