package httpdownload

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloaderRetriesAndResumesInterruptedFile(t *testing.T) {
	content := []byte("0123456789")
	var hits atomic.Int32
	var resumeRange atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			_, _ = fmt.Fprintf(w, `{"name":"file.bin","is_dir":false,"mod_time":"%s","size":%d,"path":"/file.bin"}`+"\n", time.Now().Format(time.RFC3339), len(content))
			return
		}

		hit := hits.Add(1)
		if hit == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content[:5])
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			return
		}

		resumeRange.Store(r.Header.Get("Range"))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 5-%d/%d", len(content)-1, len(content)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[5:])
	}))
	defer server.Close()

	root := t.TempDir()
	downloader, err := New(Config{
		ServerURL:   server.URL,
		SaveDir:     root,
		Concurrency: 1,
		Resume:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var maxDone atomic.Int64
	err = downloader.Start(context.Background(), func(event Event) {
		if event.Type == "progress" {
			for {
				current := maxDone.Load()
				if event.DoneBytes <= current || maxDone.CompareAndSwap(current, event.DoneBytes) {
					break
				}
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "file.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Fatalf("downloaded content = %q, want %q", data, content)
	}
	if maxDone.Load() > int64(len(content)) {
		t.Fatalf("progress counted %d bytes, want at most %d", maxDone.Load(), len(content))
	}
	if hits.Load() != 2 {
		t.Fatalf("download attempts = %d, want 2", hits.Load())
	}
	if got, _ := resumeRange.Load().(string); got != "bytes=5-" {
		t.Fatalf("resume range = %q, want %q", got, "bytes=5-")
	}
}
