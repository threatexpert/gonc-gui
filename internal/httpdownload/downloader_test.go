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

func TestDownloaderDoesNotRetryHTTPStatusError(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			_, _ = fmt.Fprintf(w, `{"name":"bad.bin","is_dir":false,"mod_time":"%s","size":10,"path":"/bad.bin"}`+"\n", time.Now().Format(time.RFC3339))
			return
		}

		hits.Add(1)
		http.Error(w, "cannot read file", http.StatusInternalServerError)
	}))
	defer server.Close()

	downloader, err := New(Config{
		ServerURL:   server.URL,
		SaveDir:     t.TempDir(),
		Concurrency: 1,
		Resume:      true,
	})
	if err != nil {
		t.Fatal(err)
	}

	var retryLogs atomic.Int32
	err = downloader.Start(context.Background(), func(event Event) {
		if event.Type == "log" && strings.Contains(event.Message, "retrying") {
			retryLogs.Add(1)
		}
	})
	if err == nil {
		t.Fatal("Start returned nil, want HTTP status error")
	}
	if !strings.Contains(err.Error(), "server returned 500 Internal Server Error for /bad.bin") {
		t.Fatalf("error = %q, want HTTP 500 status error", err.Error())
	}
	if hits.Load() != 1 {
		t.Fatalf("download attempts = %d, want 1", hits.Load())
	}
	if retryLogs.Load() != 0 {
		t.Fatalf("retry logs = %d, want 0", retryLogs.Load())
	}
}

func TestListSynthesizesSingleFileFromAttachment(t *testing.T) {
	content := []byte("single file content")
	modTime := time.Date(2026, 6, 14, 14, 17, 48, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="OpenAI.Codex.7z"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(content)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Last-Modified", modTime.Format(http.TimeFormat))
		_, _ = w.Write(content)
	}))
	defer server.Close()

	files, err := List(context.Background(), server.URL, "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files length = %d, want 1", len(files))
	}
	if files[0].Name != "OpenAI.Codex.7z" || files[0].Path != "/" || files[0].Size != int64(len(content)) {
		t.Fatalf("single file = %+v", files[0])
	}

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
	if err := downloader.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "OpenAI.Codex.7z"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Fatalf("downloaded content = %q, want %q", data, content)
	}
}

func TestSingleFileNameCannotEscapeSaveDirectory(t *testing.T) {
	root := t.TempDir()
	downloader, err := New(Config{ServerURL: "http://127.0.0.1", SaveDir: root})
	if err != nil {
		t.Fatal(err)
	}

	localPath, err := downloader.localPath(FileInfo{
		Name: "OpenAI/../../../../../../Windows/notepad.exe",
		Path: "/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if localPath != filepath.Join(root, "notepad.exe") {
		t.Fatalf("local path = %q, want %q", localPath, filepath.Join(root, "notepad.exe"))
	}
	if rel, err := filepath.Rel(root, localPath); err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("local path escapes root: %q", localPath)
	}
}
