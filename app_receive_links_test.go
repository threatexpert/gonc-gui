package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/threatexpert/gonc/v2/httpfileshare"
	"gonc-gui/internal/httpdownload"
	"gonc-gui/internal/receivecompare"
)

func TestComparisonReceivesLinkAccordingToScope(t *testing.T) {
	remote := t.TempDir()
	outside := t.TempDir()
	for _, dir := range []string{"A", "B"} {
		if err := os.Mkdir(filepath.Join(remote, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(remote, "A", "a"), filepath.Join(outside, "leaf")} {
		if err := os.WriteFile(p, []byte("contents"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.FromSlash("../A/a"), filepath.Join(remote, "B", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for target, name := range map[string]string{outside: "external", "missing": "broken", "A": "alias"} {
		if err := os.Symlink(target, filepath.Join(remote, name)); err != nil {
			t.Fatal(err)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := httpfileshare.NewServer(httpfileshare.ServerConfig{RootPaths: []string{remote}, Listener: ln})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Start() }()
	defer func() { ln.Close(); <-done }()
	url := "http://" + ln.Addr().String()
	for _, tc := range []struct {
		name, scope string
		preserve    bool
	}{{"root", "/", true}, {"subfolder", "/B", false}} {
		t.Run(tc.name, func(t *testing.T) {
			dest := t.TempDir()
			scan := func() *receivecompare.Job {
				j, err := receivecompare.New("test", dest, tc.scope, "trash", receivecompare.Differences)
				if err != nil {
					t.Fatal(err)
				}
				j.Scan(context.Background(), func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
					return httpdownload.ListComparisonScope(ctx, url, tc.scope, dest, visit)
				})
				if s := j.Page("", 0); s.Phase != "ready" {
					t.Fatalf("scan: %+v", s)
				}
				return j
			}
			j := scan()
			s := j.Page("", 0)
			var ids []int
			for _, it := range s.Items {
				if it.Path == "/B/link" {
					if it.PreserveLink != tc.preserve {
						t.Fatalf("link plan: %+v", it)
					}
					ids = append(ids, it.ID)
				}
			}
			if len(ids) != 1 {
				t.Fatalf("missing link candidate: %+v", s)
			}
			j.Select("", ids, false, true)
			ctx, err := j.BeginReceive(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			j.Receive(ctx, func(ctx context.Context, f httpdownload.FileInfo) (httpdownload.FileInfo, error) {
				return httpdownload.RefreshComparisonFile(ctx, url, f)
			}, func(ctx context.Context, root string, f httpdownload.FileInfo) error {
				d, err := httpdownload.New(httpdownload.Config{ServerURL: url, SaveDir: root, CachedFiles: []httpdownload.FileInfo{f}, Resume: true, MaxAttempts: 1})
				if err != nil {
					return err
				}
				return d.Start(ctx, nil)
			})
			if s := j.Page("", 0); s.Received != 1 {
				t.Fatalf("receive: %+v", s)
			}
			p := filepath.Join(dest, "B", "link")
			if tc.preserve {
				if v, err := os.Readlink(p); err != nil || filepath.ToSlash(v) != "../A/a" {
					t.Fatalf("target=%q err=%v", v, err)
				}
			} else if v, err := os.ReadFile(p); err != nil || string(v) != "contents" {
				t.Fatalf("contents=%q err=%v", v, err)
			}
			if _, err := os.Lstat(filepath.Join(dest, "A", "a")); !os.IsNotExist(err) {
				t.Fatal("selected the target implicitly")
			}
			for _, it := range scan().Page("", 0).Items {
				if it.Path == "/B/link" {
					t.Fatalf("repeated difference: %+v", it)
				}
			}
		})
	}
	t.Run("ordinary receive expands external directory and preserves directory link", func(t *testing.T) {
		dest := t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := httpdownload.New(httpdownload.Config{ServerURL: url, SaveDir: dest, IncludePaths: []string{"/alias", "/external"}, Resume: true, MaxAttempts: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Start(ctx, nil); err != nil {
			t.Fatal(err)
		}
		if v, err := os.Readlink(filepath.Join(dest, "alias")); err != nil || v != "A" {
			t.Fatalf("directory link=%q err=%v", v, err)
		}
		if v, err := os.ReadFile(filepath.Join(dest, "external", "leaf")); err != nil || string(v) != "contents" {
			t.Fatalf("external file=%q err=%v", v, err)
		}
	})
}
