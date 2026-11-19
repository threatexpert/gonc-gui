package receivecompare

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gonc-gui/internal/httpdownload"
)

func put(t *testing.T, root, p, content string) string {
	t.Helper()
	full := filepath.Join(root, p)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return full
}

func scan(t *testing.T, root, scope string, remote ...httpdownload.FileInfo) *Job {
	t.Helper()
	j, err := New("1", root, scope, "trash")
	if err != nil {
		t.Fatal(err)
	}
	j.Scan(context.Background(), func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
		for _, f := range remote {
			if err := visit(f); err != nil {
				return err
			}
		}
		return nil
	})
	if s := j.Page("", 0); s.Phase != "ready" {
		t.Fatalf("scan=%+v", s)
	}
	return j
}

func TestScanFindsRemovedSubtreesAndKeepsExistingPathsRegardlessOfSize(t *testing.T) {
	root := t.TempDir()
	put(t, root, "keep.txt", "local edits")
	put(t, root, "removed/deep/old.txt", "old")
	put(t, root, "own.txt", "mine")
	put(t, root, "conflict/inside.txt", "no")
	put(t, root, "implicit", "local file where remote has a directory")
	j := scan(t, root, "/", httpdownload.FileInfo{Path: "/keep.txt"}, httpdownload.FileInfo{Path: "/conflict"}, httpdownload.FileInfo{Path: "/implicit/file.txt"})
	s := j.Page("", 0)
	if s.Candidates != 2 || s.Selected != 0 || s.Unchecked != 1 {
		t.Fatalf("snapshot=%+v", s)
	}
	if got := j.Page("removed/", 0); got.Matched != 1 || got.Items[0].Path != "/removed/deep/old.txt" {
		t.Fatalf("removed subtree=%+v", got)
	}
	if err := j.Select("removed/", nil, true, true); err != nil {
		t.Fatal(err)
	}
	if got := j.Page("own", 0); got.Selected != 1 || got.Items[0].Selected {
		t.Fatalf("filter changed selection=%+v", got)
	}
}

func TestScopedComparisonAndPagination(t *testing.T) {
	root := t.TempDir()
	put(t, root, "outside.txt", "outside")
	for i := 0; i < 405; i++ {
		put(t, root, fmt.Sprintf("old/%04d.txt", i), "x")
	}
	j := scan(t, root, "/old")
	if s := j.Page("", 0); s.Candidates != 405 || len(s.Items) != PageSize {
		t.Fatalf("page=%+v", s)
	}
	if s := j.Page("", 999); s.Page != 2 || len(s.Items) != 5 {
		t.Fatalf("last page=%+v", s)
	}
	if err := j.Select(".txt", nil, true, true); err != nil {
		t.Fatal(err)
	}
	if s := j.Page("", 1); s.Selected != 405 || s.SelectedBytes != 405 {
		t.Fatalf("cross-page selection=%+v", s)
	}
	if err := j.Select("", []int{0}, false, false); err != nil {
		t.Fatal(err)
	}
	if s := j.Page("", 0); s.Selected != 404 {
		t.Fatalf("exclusion=%+v", s)
	}
	if s := j.Page("no matches", 0); s.Matched != 0 || len(s.Items) != 0 || s.Page != 0 {
		t.Fatalf("empty page=%+v", s)
	}
}

func TestBadListCancellationAndOversizeDisableDeletion(t *testing.T) {
	for _, kind := range []string{"bad", "cancel", "large"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "local.txt", "x")
			j, _ := New("1", root, "/", "trash")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancel" {
				cancel()
			}
			if kind == "large" {
				j.indexBytes = maxIndexBytes
			}
			j.Scan(ctx, func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
				if err := visit(httpdownload.FileInfo{Path: "/remote"}); err != nil {
					return err
				}
				if kind == "bad" {
					return errors.New("truncated list")
				}
				return ctx.Err()
			})
			if err := j.Select("", nil, true, true); err == nil {
				t.Fatal("incomplete result selected")
			}
			if _, err := j.BeginDelete(context.Background(), "permanent"); err == nil {
				t.Fatal("incomplete result deleted")
			}
		})
	}
}

func TestDeleteRechecksRemoteLocalIdentityAndNeverFallsBack(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"gone", "remote-present", "remote-error", "changed", "replaced", "trash-failed"} {
		put(t, root, name, "old")
	}
	j := scan(t, root, "/")
	put(t, root, "changed", "new longer content")
	// Same path, length and modification time, but a different file identity.
	var oldTime time.Time
	for _, e := range j.entries {
		if e.Path == "/replaced" {
			oldTime = e.info.ModTime()
		}
	}
	if err := os.Rename(filepath.Join(root, "replaced"), filepath.Join(root, "backup")); err != nil {
		t.Fatal(err)
	}
	newFile := put(t, root, "replaced", "new")
	os.Chtimes(newFile, oldTime, oldTime)
	if err := j.Select("", nil, true, true); err != nil {
		t.Fatal(err)
	}
	ctx, err := j.BeginDelete(context.Background(), "trash")
	if err != nil {
		t.Fatal(err)
	}
	j.Delete(ctx, func(ctx context.Context, p string) (bool, error) {
		switch p {
		case "/remote-present":
			return false, nil
		case "/remote-error":
			return false, errors.New("offline")
		}
		return true, nil
	}, func(p string) error {
		if filepath.Base(p) == "trash-failed" {
			return errors.New("trash unavailable")
		}
		return os.Remove(p)
	})
	s := j.Page("", 0)
	if s.Deleted != 1 || s.Skipped != 3 || s.Failed != 2 || s.Selected != 0 {
		t.Fatalf("delete result=%+v", s)
	}
	for _, name := range []string{"remote-present", "remote-error", "changed", "replaced", "trash-failed"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("unexpected deletion %s: %v", name, err)
		}
	}
	if err := j.Select("trash-failed", nil, true, true); err != nil {
		t.Fatal(err)
	}
	ctx, err = j.BeginDelete(context.Background(), "permanent")
	if err != nil {
		t.Fatal(err)
	}
	j.Delete(ctx, func(context.Context, string) (bool, error) { return true, nil }, os.Remove)
	if s := j.Page("", 0); s.Deleted != 2 || s.Failed != 1 {
		t.Fatalf("retry result=%+v", s)
	}
}

func TestLinksAndRootReplacementAreNotDeleted(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	put(t, root, "old.txt", "old")
	put(t, outside, "keep.txt", "keep")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err == nil {
		j := scan(t, root, "/")
		if s := j.Page("", 0); s.Candidates != 1 || s.Unchecked != 1 {
			t.Fatalf("followed link: %+v", s)
		}
	}
	j := scan(t, root, "/")
	if err := os.Rename(root, filepath.Join(base, "original")); err != nil {
		t.Fatal(err)
	}
	put(t, root, "old.txt", "replacement")
	j.Select("", nil, true, true)
	ctx, err := j.BeginDelete(context.Background(), "permanent")
	if err != nil {
		t.Fatal(err)
	}
	j.Delete(ctx, func(context.Context, string) (bool, error) { return true, nil }, func(string) error { t.Error("delete called on replacement root"); return nil })
	if s := j.Page("", 0); s.Deleted != 0 || s.Skipped != 1 {
		t.Fatalf("replacement=%+v", s)
	}
}

func TestCancellationAndConcurrentPolling(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		put(t, root, fmt.Sprint(i), "x")
	}
	j := scan(t, root, "/")
	j.Select("", nil, true, true)
	ctx, err := j.BeginDelete(context.Background(), "permanent")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		j.Delete(ctx, func(ctx context.Context, p string) (bool, error) {
			started <- struct{}{}
			<-ctx.Done()
			return false, ctx.Err()
		}, func(string) error { t.Error("delete after cancellation"); return nil })
	}()
	<-started
	for i := 0; i < 20; i++ {
		j.Page("", 0)
	}
	j.Cancel()
	<-done
	if s := j.Page("", 0); s.Deleted != 0 || s.Selected != 20 || s.Phase != "done" {
		t.Fatalf("cancel=%+v", s)
	}
}

func TestScopeValidation(t *testing.T) {
	for _, p := range []string{"../outside", "/a/../../b", "C:/other", "/a\\b"} {
		if _, err := New("1", t.TempDir(), p, "trash"); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
}

func TestLocalFilesystemAliasesAreNotOfferedForDeletion(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		t.Skip("case-insensitive destination rules")
	}
	root := t.TempDir()
	put(t, root, "Keep.txt", "local")
	remotePath := "/keep.txt"
	if runtime.GOOS == "windows" {
		remotePath += ". "
	}
	j := scan(t, root, "/", httpdownload.FileInfo{Path: remotePath})
	if s := j.Page("", 0); s.Candidates != 0 {
		t.Fatalf("filesystem alias offered: %+v", s)
	}
}

func BenchmarkRemoteIndex100K(b *testing.B) {
	root := b.TempDir()
	paths := make([]string, 100000)
	for i := range paths {
		paths[i] = fmt.Sprintf("/dir%04d/file%08d.txt", i/100, i)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		j, _ := New("1", root, "/", "trash")
		j.Scan(context.Background(), func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
			for _, p := range paths {
				if err := visit(httpdownload.FileInfo{Path: p}); err != nil {
					return err
				}
			}
			return nil
		})
		if j.state.Phase != "ready" {
			b.Fatal(j.state.Error)
		}
	}
}

func BenchmarkCachedPage100K(b *testing.B) {
	j := &Job{state: Snapshot{Phase: "ready"}}
	for i := 0; i < 100000; i++ {
		j.entries = append(j.entries, entry{Item: Item{ID: i, Path: fmt.Sprintf("/dir/file%08d.txt", i)}})
	}
	j.Page("file", 0)
	b.ResetTimer()
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		j.Page("file", n%500)
	}
}

func TestScopedIndexDoesNotRetainUnrelatedRemoteTree(t *testing.T) {
	root := t.TempDir()
	put(t, root, "old/x", "x")
	j, _ := New("1", root, "/old", "trash")
	j.Scan(context.Background(), func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
		for i := 0; i < 10000; i++ {
			if err := visit(httpdownload.FileInfo{Path: fmt.Sprintf("/elsewhere/%d", i)}); err != nil {
				return err
			}
		}
		return nil
	})
	if j.indexBytes > 1024 || j.Page("", 0).Candidates != 1 {
		t.Fatal("unrelated remote tree retained")
	}
}

// Keep these imported utilities exercised in race runs with simultaneous reads.
func TestConcurrentPages(t *testing.T) {
	root := t.TempDir()
	put(t, root, "a.txt", "a")
	j := scan(t, root, "/")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				s := j.Page(strings.Repeat("a", n%2), 0)
				if s.Candidates != 1 {
					t.Error("lost candidate")
				}
			}
		}()
	}
	wg.Wait()
}
