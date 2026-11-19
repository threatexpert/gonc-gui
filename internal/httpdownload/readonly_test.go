package httpdownload

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadOnlyReceiveBatch(t *testing.T) {
	const block = 64 * 1024
	stamp := time.Unix(1700000000, 123456700)
	content := bytes.Repeat([]byte("a"), 3*block)
	for _, manifest := range []bool{true, false} {
		name := "without-manifest"
		if manifest {
			name = "with-manifest"
		}
		t.Run(name, func(t *testing.T) {
			var unchangedGET, dirtyGET, manifests, retries atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Has("manifest") {
					manifests.Add(1)
					if manifest {
						writeBlake3Manifest(t, w, r.URL.Path, content, stamp, block)
					} else {
						http.NotFound(w, r)
					}
					return
				}
				switch r.URL.Path {
				case "/same":
					unchangedGET.Add(1)
				case "/dirty":
					dirtyGET.Add(1)
				}
				w.Write(content)
			}))
			defer server.Close()

			root := t.TempDir()
			dirty := bytes.Clone(content)
			dirty[block] = 'x'
			for name, data := range map[string][]byte{"same": content, "dirty": dirty} {
				p := filepath.Join(root, name)
				if err := os.WriteFile(p, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(p, stamp, stamp.Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(p, 0444); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(p, 0600) })
			}
			if f, err := os.OpenFile(filepath.Join(root, "dirty"), os.O_WRONLY, 0); err == nil {
				f.Close()
				t.Skip("current account can write read-only files")
			} else if !errors.Is(err, os.ErrPermission) {
				t.Fatal(err)
			}
			var files []FileInfo
			for _, path := range []string{"/same", "/dirty", "/next"} {
				files = append(files, FileInfo{Path: path, ModTime: stamp, Size: int64(len(content))})
			}
			d, err := New(Config{ServerURL: server.URL, SaveDir: root, Resume: true, Concurrency: 1, CachedFiles: files})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = d.Start(ctx, func(e Event) {
				if strings.Contains(e.Message, "retrying") {
					retries.Add(1)
				}
			})
			if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), filepath.Join(root, "dirty")) || retries.Load() != 0 {
				t.Fatalf("expected one local permission failure and no retries, got %v, retries=%d", err, retries.Load())
			}
			for name, expected := range map[string][]byte{"same": content, "dirty": dirty, "next": content} {
				p := filepath.Join(root, name)
				data, err := os.ReadFile(p)
				if err != nil || !bytes.Equal(data, expected) {
					t.Fatalf("%s content mismatch: %v", name, err)
				}
				info, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				wantTime := stamp
				if name == "dirty" {
					wantTime = stamp.Add(-time.Second)
				}
				if !info.ModTime().Equal(wantTime) || (name != "next" && info.Mode().Perm()&0200 != 0) {
					t.Fatalf("%s: mode=%s, mtime=%s, want %s", name, info.Mode(), info.ModTime(), wantTime)
				}
			}
			wantGET := int32(1)
			if manifest {
				wantGET = 0
			}
			if unchangedGET.Load() != wantGET || dirtyGET.Load() != wantGET || manifests.Load() != 2 ||
				d.progress.failedFiles.Load() != 1 || d.progress.doneFiles.Load() != 2 || d.progress.skippedFiles.Load() != 1 {
				t.Fatalf("manifest requests=%d, body requests same=%d dirty=%d, failed=%d done=%d skipped=%d", manifests.Load(), unchangedGET.Load(), dirtyGET.Load(), d.progress.failedFiles.Load(), d.progress.doneFiles.Load(), d.progress.skippedFiles.Load())
			}
		})
	}
}

func TestIncompleteManifestFallsBackAndReplacesDifferentSuffix(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 96*1024)
	stamp := time.Unix(1700000000, 123456700)
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("manifest") {
			// A successful response containing just the header proves no blocks.
			w.Write([]byte(`{"type":"file","algo":"blake3","size":98304,"block_size":65536,"mod_time":"2023-11-14T22:13:20.1234567Z"}`))
			return
		}
		gets.Add(1)
		w.Write(content)
	}))
	defer server.Close()
	root := t.TempDir()
	p := filepath.Join(root, "file")
	local := bytes.Clone(content)
	local[len(local)-1] = 'y'
	if err := os.WriteFile(p, local, 0600); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{ServerURL: server.URL, SaveDir: root, Resume: true, MaxAttempts: 1, CachedFiles: []FileInfo{{Path: "/file", Size: int64(len(content)), ModTime: stamp}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil || !bytes.Equal(got, content) || gets.Load() != 1 {
		t.Fatalf("suffix repair: err=%v, full GETs=%d, correct=%t", err, gets.Load(), bytes.Equal(got, content))
	}
}
