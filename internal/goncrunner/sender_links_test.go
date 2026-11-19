package goncrunner

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/threatexpert/gonc/v2/httpfileshare"
	"gonc-gui/internal/httpdownload"
)

func TestDesktopSenderListAdvertisesLinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "folder", "file"), []byte("file contents"), 0644); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"tool": "folder/file", "tools": "folder", "broken": "missing"} {
		if err := os.Symlink(filepath.FromSlash(target), filepath.Join(dir, name)); err != nil {
			t.Skipf("cannot create symlink: %v", err)
		}
	}
	src, err := newDynamicFileSource(nil)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := httpfileshare.NewServer(httpfileshare.ServerConfig{FileSource: src, Listener: ln, EnableZstd: true})
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Start() }()
	defer func() { ln.Close(); <-done }()
	if err := src.UpdatePaths([]string{dir}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "http://" + ln.Addr().String()
	files, err := httpdownload.List(ctx, url, "/")
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]httpdownload.FileInfo{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	for p, target := range map[string]string{"/tool": "folder/file", "/tools": "folder", "/broken": "missing"} {
		if f := byPath[p]; f.LinkTarget != target {
			t.Errorf("%s: link_target=%q, want %q", p, f.LinkTarget, target)
		}
	}
	if !byPath["/tools"].IsDir || byPath["/broken"].Size != -1 || byPath["/tool"].Size != int64(len("file contents")) {
		t.Errorf("wrong link attributes: %+v", files)
	}
	info, err := os.Stat(filepath.Join(dir, "folder", "file"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/folder/file", "/tool"} {
		mode := byPath[p].Mode
		if mode == nil || *mode != uint32(info.Mode().Perm()) {
			t.Errorf("%s: mode = %v, want %o", p, mode, info.Mode().Perm())
		}
	}
	if byPath["/broken"].Mode != nil {
		t.Error("broken link has a target mode")
	}
	single, err := httpdownload.List(ctx, url, "/tool")
	if err != nil || len(single) != 1 || single[0].LinkTarget != "folder/file" {
		t.Fatalf("file listing: %+v, %v", single, err)
	}
}
