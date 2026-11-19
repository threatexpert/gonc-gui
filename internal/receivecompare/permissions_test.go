package receivecompare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/threatexpert/gonc/v2/httpfileshare"
	"gonc-gui/internal/httpdownload"
)

func TestComparisonReceivePermissionOnlyAndMissingDirectory(t *testing.T) {
	root := t.TempDir()
	stamp := time.Unix(1700000000, 0)
	name := put(t, root, "build.sh", "abc")
	if err := os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	mode := uint32(0755)
	if runtime.GOOS == "windows" {
		mode = 0444
	}
	dirMode := uint32(0500)
	dir := filepath.Join(root, "scripts")
	t.Cleanup(func() { os.Chmod(dir, 0755); os.Chmod(name, 0600) })
	var gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
			if r.URL.Path != "/scripts/new" {
				t.Errorf("unexpected GET %s", r.URL)
			}
		}
		w.Header().Set("Last-Modified", stamp.UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Length", "3")
		if r.Method != http.MethodHead {
			fmt.Fprint(w, "abc")
		}
	}))
	defer srv.Close()
	files := []httpdownload.FileInfo{
		{Path: "/build.sh", Size: 3, ModTime: stamp, Mode: &mode},
		{Path: "/scripts/new", Size: 3, ModTime: stamp},
		{Path: "/scripts", IsDir: true, Mode: &dirMode},
	}
	job := scanPurpose(t, root, "/", Differences, files...)
	s := job.Page("", 0)
	if s.Candidates != 3 || s.Bytes != 3 {
		t.Fatalf("scan: %+v", s)
	}
	for _, item := range s.Items {
		if item.PermissionsDiffer != (item.Path == "/build.sh") {
			t.Errorf("%s: unexpected permissionsDiffer=%t", item.Path, item.PermissionsDiffer)
		}
	}
	if err := job.Select("", nil, true, true); err != nil {
		t.Fatal(err)
	}
	ctx, err := job.BeginReceive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	job.Receive(ctx, func(ctx context.Context, f httpdownload.FileInfo) (httpdownload.FileInfo, error) {
		return httpdownload.RefreshComparisonFile(ctx, srv.URL, f)
	}, func(ctx context.Context, root string, f httpdownload.FileInfo) error {
		d, err := httpdownload.New(httpdownload.Config{ServerURL: srv.URL, SaveDir: root, Resume: true, CachedFiles: []httpdownload.FileInfo{f}})
		if err != nil {
			return err
		}
		return d.Start(ctx, nil)
	})
	if s := job.Page("", 0); s.Received != 3 || s.Failed != 0 || gets.Load() != 1 {
		t.Fatalf("result %+v GETs=%d", s, gets.Load())
	}
	for _, item := range []struct {
		path string
		mode uint32
	}{{name, mode}, {dir, dirMode}} {
		info, err := os.Stat(item.path)
		if err != nil || httpfileshare.PermissionsDiffer(&item.mode, info.Mode()) {
			t.Fatalf("%s permissions: %v %v", item.path, info, err)
		}
	}
	if s := scanPurpose(t, root, "/", Differences, files...).Page("", 0); s.Candidates != 0 {
		t.Fatalf("repeat scan: %+v", s)
	}
	// A legacy list omits mode and cannot change the local permissions.
	files[0].Mode = nil
	if err := os.Chmod(name, 0600); err != nil {
		t.Fatal(err)
	}
	if s := scanPurpose(t, root, "/", Differences, files...).Page("", 0); s.Candidates != 0 {
		t.Fatalf("legacy list: %+v", s)
	}
}
