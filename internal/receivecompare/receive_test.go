package receivecompare

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gonc-gui/internal/httpdownload"
)

func scanPurpose(t *testing.T, root, scope, kind string, files ...httpdownload.FileInfo) *Job {
	t.Helper()
	j, err := New("1", root, scope, "trash", kind)
	if err != nil {
		t.Fatal(err)
	}
	j.Scan(context.Background(), func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
		for _, file := range files {
			if err := visit(file); err != nil {
				return err
			}
		}
		return nil
	})
	if s := j.Page("", 0); s.Phase != "ready" {
		t.Fatalf("scan: %+v", s)
	}
	return j
}

func sameMetadata(_ context.Context, file httpdownload.FileInfo) (httpdownload.FileInfo, error) {
	return file, nil
}

func TestPurposeSpecificResults(t *testing.T) {
	root := t.TempDir()
	put(t, root, "only-local.txt", "local")
	put(t, root, "same-size.txt", "abc")
	put(t, root, "different.txt", "old")
	put(t, root, "empty.txt", "")
	put(t, root, "file-conflict/inside.txt", "child")
	put(t, root, "parent-conflict", "file")
	remote := []httpdownload.FileInfo{
		{Path: "/same-size.txt", Size: 3}, {Path: "/different.txt", Size: 8},
		{Path: "/empty.txt", Size: 0}, {Path: "/new/deep/missing.txt", Size: 4}, {Path: "/missing-empty.txt", Size: 0},
		{Path: "/file-conflict", Size: 1}, {Path: "/parent-conflict/child.txt", Size: 2},
	}
	missing := scanPurpose(t, root, "/", LocalMissing, remote...)
	s := missing.Page("", 0)
	if s.Candidates != 2 || s.Unchecked != 2 || s.Selected != 0 || s.Bytes != 4 {
		t.Fatalf("missing: %+v", s)
	}
	for _, item := range s.Items {
		if item.LocalSize != nil || item.RemoteSize == nil {
			t.Fatalf("missing sides: %+v", item)
		}
	}
	different := scanPurpose(t, root, "/", SizeDifferent, remote...)
	s = different.Page("", 0)
	if s.Candidates != 1 || s.Items[0].Path != "/different.txt" || *s.Items[0].LocalSize != 3 || *s.Items[0].RemoteSize != 8 {
		t.Fatalf("different: %+v", s)
	}
	scoped := scanPurpose(t, root, "/new", LocalMissing, remote...)
	if s := scoped.Page("", 0); s.Candidates != 1 || s.Scanned != 1 {
		t.Fatalf("scope: %+v", s)
	}
	for _, j := range []*Job{missing, different} {
		j.Select("", nil, true, true)
		if _, err := j.BeginDelete(context.Background(), "permanent"); err == nil {
			t.Fatal("non-local-only result can be deleted")
		}
	}
	local := scanPurpose(t, root, "/", LocalOnly, remote...)
	local.Select("", nil, true, true)
	if _, err := local.BeginReceive(context.Background()); err == nil {
		t.Fatal("local-only result can be received")
	}
}

func TestRemoteSizeAndAmbiguousPathsFailClosed(t *testing.T) {
	for _, files := range [][]httpdownload.FileInfo{
		{{Path: "/unknown", Size: -1}},
		{{Path: "/duplicate", Size: 1}, {Path: "/duplicate", Size: 2}},
	} {
		j, _ := New("1", t.TempDir(), "/", "trash", LocalMissing)
		j.Scan(context.Background(), func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
			for _, file := range files {
				if err := visit(file); err != nil {
					return err
				}
			}
			return nil
		})
		if j.Page("", 0).Phase != "error" {
			t.Fatal("invalid list accepted")
		}
		if _, err := j.BeginReceive(context.Background()); err == nil {
			t.Fatal("incomplete result used")
		}
	}
	if _, err := New("1", t.TempDir(), "/", "trash", "all"); err == nil {
		t.Fatal("unsupported purpose accepted")
	}
}

func TestReceiveRevalidatesAfterRemoteRefresh(t *testing.T) {
	for _, kind := range []string{LocalMissing, SizeDifferent} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if kind == SizeDifferent {
				put(t, root, "file.txt", "old")
			}
			j := scanPurpose(t, root, "/", kind, httpdownload.FileInfo{Path: "/file.txt", Size: 9})
			j.Select("", nil, true, true)
			ctx, err := j.BeginReceive(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			j.Receive(ctx, func(ctx context.Context, file httpdownload.FileInfo) (httpdownload.FileInfo, error) {
				put(t, root, "file.txt", "user's new edits")
				return file, nil
			}, func(context.Context, string, httpdownload.FileInfo) error {
				t.Error("modified local file overwritten")
				return nil
			})
			if s := j.Page("", 0); s.Received != 0 || s.Skipped != 1 {
				t.Fatalf("revalidation: %+v", s)
			}
			content, _ := os.ReadFile(filepath.Join(root, "file.txt"))
			if string(content) != "user's new edits" {
				t.Fatal("edits lost")
			}
		})
	}
}

func TestReceiveSelectedOnlyAndRetryOwnPartialWrite(t *testing.T) {
	root := t.TempDir()
	j := scanPurpose(t, root, "/", LocalMissing, httpdownload.FileInfo{Path: "/partial.txt", Size: 10}, httpdownload.FileInfo{Path: "/unselected.txt", Size: 20})
	j.Select("partial", nil, true, true)
	ctx, err := j.BeginReceive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j.Receive(ctx, sameMetadata, func(ctx context.Context, root string, file httpdownload.FileInfo) error {
		if file.Path != "/partial.txt" {
			t.Error("unselected file received")
		}
		return errors.Join(os.WriteFile(filepath.Join(root, "partial.txt"), []byte("part"), 0600), errors.New("network interrupted"))
	})
	if s := j.Page("", 0); s.Failed != 1 || s.Received != 0 {
		t.Fatalf("partial result: %+v", s)
	}
	j.Select("partial", nil, true, true)
	ctx, err = j.BeginReceive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j.Receive(ctx, sameMetadata, func(ctx context.Context, root string, file httpdownload.FileInfo) error {
		return os.WriteFile(filepath.Join(root, "partial.txt"), []byte("0123456789"), 0600)
	})
	if s := j.Page("", 0); s.Failed != 0 || s.Received != 1 {
		t.Fatalf("resumed: %+v", s)
	}
	if _, err := os.Stat(filepath.Join(root, "unselected.txt")); !os.IsNotExist(err) {
		t.Fatal("unselected file created")
	}
}

func TestReceiveSkipsLinksAndCancellation(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err == nil {
		j := scanPurpose(t, root, "/", LocalMissing, httpdownload.FileInfo{Path: "/linked/new.txt", Size: 1})
		if s := j.Page("", 0); s.Candidates != 0 || s.Unchecked != 1 {
			t.Fatalf("linked parent: %+v", s)
		}
	}
	j := scanPurpose(t, root, "/", LocalMissing, httpdownload.FileInfo{Path: "/new.txt", Size: 1})
	j.Select("", nil, true, true)
	ctx, err := j.BeginReceive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j.Cancel()
	j.Receive(ctx, sameMetadata, func(context.Context, string, httpdownload.FileInfo) error {
		t.Error("received after cancellation")
		return nil
	})
	if s := j.Page("", 0); s.Received != 0 || s.Selected != 1 || s.Phase != "done" {
		t.Fatalf("cancel: %+v", s)
	}
}

func TestReceiveProgressVisibleOutsideCurrentFilter(t *testing.T) {
	file := httpdownload.FileInfo{Path: "/large.bin", Size: 10}
	j := scanPurpose(t, t.TempDir(), "/", LocalMissing, file)
	j.Select("", nil, true, true)
	ctx, err := j.BeginReceive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j.Receive(ctx, func(_ context.Context, file httpdownload.FileInfo) (httpdownload.FileInfo, error) {
		s := j.Page("other", 0)
		if len(s.Items) != 0 || s.BatchTotal != 1 || s.BatchFinished != 0 || len(s.ActiveReceives) != 1 || !s.ActiveReceives[0].Checking {
			t.Errorf("checking file: %+v", s)
		}
		return file, nil
	}, func(_ context.Context, _ string, file httpdownload.FileInfo) error {
		j.RecordReceiveProgress(file.Path, httpdownload.Event{Type: "progress", DoneBytes: 4, TotalBytes: 10})
		s := j.Page("other", 0)
		want := ActiveReceive{ID: 0, Path: file.Path, DoneBytes: 4, TotalBytes: 10}
		if len(s.ActiveReceives) != 1 || s.ActiveReceives[0] != want {
			t.Errorf("transfer: %+v", s)
		}
		return nil
	})
	if s := j.Page("", 0); s.BatchFinished != 1 || len(s.ActiveReceives) != 0 || s.Items[0].Outcome != "received" {
		t.Fatalf("completed: %+v", s)
	}
}
