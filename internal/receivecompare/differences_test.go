package receivecompare

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gonc-gui/internal/httpdownload"
)

func TestDifferencesIncludesPresenceSizeAndTime(t *testing.T) {
	root := t.TempDir()
	stamp := time.Unix(1700000000, 0)
	for _, name := range []string{"same", "subsecond", "time", "size", "both", "unknown", "extra"} {
		p := put(t, root, name, "abc")
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	remote := []httpdownload.FileInfo{
		{Path: "/same", Size: 3, ModTime: stamp.In(time.FixedZone("other", 8*3600))},
		{Path: "/subsecond", Size: 3, ModTime: stamp.Add(time.Nanosecond)},
		{Path: "/time", Size: 3, ModTime: stamp.Add(time.Second)},
		{Path: "/size", Size: 4, ModTime: stamp},
		{Path: "/both", Size: 5, ModTime: stamp.Add(-time.Second)},
		{Path: "/unknown", Size: 3},
		{Path: "/new/missing", Size: 0, ModTime: stamp},
	}
	j := scanPurpose(t, root, "/", Differences, remote...)
	s := j.Page("", 0)
	if s.Candidates != 6 || s.Scanned != 8 || s.Unchecked != 1 {
		t.Fatalf("result: %+v", s)
	}
	want := map[string]bool{"/subsecond": true, "/time": true, "/size": true, "/both": true, "/extra": true, "/new/missing": true}
	for _, item := range s.Items {
		if !want[item.Path] {
			t.Fatalf("unexpected difference: %+v", item)
		}
		delete(want, item.Path)
		if item.LocalSize != nil && item.LocalModified == "" || item.RemoteSize != nil && item.RemoteModified == "" {
			t.Fatalf("missing time: %+v", item)
		}
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
	missingScope := scanPurpose(t, root, "/new", Differences, remote...).Page("", 0)
	if missingScope.Candidates != 1 || missingScope.Items[0].LocalSize != nil {
		t.Fatalf("missing scope: %+v", missingScope)
	}
}

func TestMixedSelectionActionsStaySeparateAcrossPages(t *testing.T) {
	for _, deleteFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(deleteFirst), func(t *testing.T) {
			root := t.TempDir()
			put(t, root, "extra", "extra")
			remote := make([]httpdownload.FileInfo, PageSize+1)
			for i := range remote {
				remote[i] = httpdownload.FileInfo{Path: fmt.Sprintf("/missing-%03d", i), Size: 1}
			}
			j := scanPurpose(t, root, "/", Differences, remote...)
			if err := j.Select("", nil, true, true); err != nil {
				t.Fatal(err)
			}
			if s := j.Page("", 1); s.SelectedLocalOnly != 1 || s.SelectedReceive != len(remote) || s.SelectedBytes != int64(len(remote)+5) || len(s.Items) != 2 {
				t.Fatalf("selection: %+v", s)
			}
			receive := func() {
				ctx, err := j.BeginReceive(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				j.Receive(ctx, sameMetadata, func(_ context.Context, root string, f httpdownload.FileInfo) error {
					if f.Path == "/extra" || f.Path == "" {
						t.Errorf("extra file received: %+v", f)
					}
					return os.WriteFile(filepath.Join(root, filepath.Base(f.Path)), []byte("x"), 0600)
				})
				if _, err := j.BeginReceive(context.Background()); err == nil {
					t.Fatal("receive accepted without eligible selection")
				}
			}
			remove := func() {
				ctx, err := j.BeginDelete(context.Background(), "permanent")
				if err != nil {
					t.Fatal(err)
				}
				j.Delete(ctx, func(_ context.Context, p string) (bool, error) {
					if p != "/extra" {
						t.Errorf("non-extra file offered for deletion: %s", p)
					}
					return true, nil
				}, os.Remove)
				if _, err := j.BeginDelete(context.Background(), "permanent"); err == nil {
					t.Fatal("delete accepted without eligible selection")
				}
			}
			if deleteFirst {
				remove()
				if s := j.Page("", 0); s.SelectedReceive != len(remote) || s.SelectedLocalOnly != 0 {
					t.Fatalf("after delete: %+v", s)
				}
				receive()
			} else {
				receive()
				if s := j.Page("", 0); s.SelectedLocalOnly != 1 || s.SelectedBytes != 5 {
					t.Fatalf("after receive: %+v", s)
				}
				remove()
			}
			if s := j.Page("", 0); s.Received != len(remote) || s.Deleted != 1 || s.Selected != 0 || s.SelectedBytes != 0 || s.SelectedReceiveBytes != 0 || s.SelectedLocalOnlyBytes != 0 || s.Failed != 0 {
				t.Fatalf("final: %+v", s)
			}
		})
	}
}
