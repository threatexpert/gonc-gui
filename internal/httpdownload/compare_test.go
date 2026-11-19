package httpdownload

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestComparisonListRejectsPartialOrMalformedRecords(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		valid                   bool
	}{
		{"valid", `{"path":"/ok","is_dir":false}` + "\n", "application/x-ndjson", true},
		{"empty", "", "application/x-ndjson", true},
		{"bad record", `{"path":"/ok","is_dir":false}` + "\nBROKEN\n", "application/x-ndjson", false},
		{"missing path", `{"is_dir":false}` + "\n", "application/x-ndjson", false},
		{"missing kind", `{"path":"/ok"}` + "\n", "application/x-ndjson", false},
		{"html", "<html></html>", "text/html", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				fmt.Fprint(w, tc.body)
			}))
			defer s.Close()
			err := ListForComparison(context.Background(), s.URL, func(FileInfo) error { return nil })
			if (err == nil) != tc.valid {
				t.Fatalf("error=%v, valid=%v", err, tc.valid)
			}
		})
	}
}

func TestComparisonListRejectsTruncatedHTTPBody(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Content-Length", "999")
		fmt.Fprintln(w, `{"path":"/ok","is_dir":false}`)
	}))
	defer s.Close()
	if err := ListForComparison(context.Background(), s.URL, func(FileInfo) error { return nil }); err == nil {
		t.Fatal("truncated list accepted")
	}
}

func TestRemoteMissingRequiresExplicit404AndEscapesPath(t *testing.T) {
	for _, status := range []int{200, 301, 404, 403, 500, 405} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "HEAD" || r.URL.Path != "/dir/a #?%.txt" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
				}
				w.Header().Set("Location", "/do-not-follow")
				w.WriteHeader(status)
			}))
			defer s.Close()
			missing, err := RemotePathMissing(context.Background(), s.URL, "/dir/a #?%.txt")
			if missing != (status == 404) {
				t.Fatalf("missing=%v status=%d", missing, status)
			}
			if (err != nil) != (status >= 400 && status != 404) {
				t.Fatalf("error=%v status=%d", err, status)
			}
		})
	}
}

func TestComparisonMetadataPreservesZeroAndUnknownSize(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"path":"/zero","name":"zero","is_dir":false,"size":0,"mod_time":"2026-11-19T01:02:03Z"}`)
		fmt.Fprintln(w, `{"path":"/unknown","is_dir":false}`)
	}))
	defer s.Close()
	var files []FileInfo
	if err := ListForComparison(context.Background(), s.URL, func(file FileInfo) error { files = append(files, file); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Size != 0 || files[1].Size != -1 || files[0].ModTime.IsZero() || files[0].Name != "zero" {
		t.Fatalf("metadata: %+v", files)
	}
}

func TestComparisonDownloadRetryBudget(t *testing.T) {
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", "10")
		fmt.Fprint(w, "short")
	}))
	defer s.Close()
	d, err := New(Config{ServerURL: s.URL, SaveDir: t.TempDir(), CachedFiles: []FileInfo{{Path: "/file", Size: 10}}, MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Start(context.Background(), nil); err == nil {
		t.Fatal("truncated content succeeded")
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestComparisonRefreshKeepsListPrecisionWhenHeadAgrees(t *testing.T) {
	listed := time.Unix(1700000000, 123456789)
	for _, header := range []string{listed.UTC().Format(http.TimeFormat), listed.Add(time.Second).UTC().Format(http.TimeFormat), ""} {
		t.Run(header, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "3")
				w.Header().Set("Last-Modified", header)
			}))
			defer s.Close()
			file, err := RefreshComparisonFile(context.Background(), s.URL, FileInfo{Path: "/file", Size: 3, ModTime: listed})
			if err != nil {
				t.Fatal(err)
			}
			want, _ := http.ParseTime(header)
			if !want.IsZero() && want.Unix() == listed.Unix() {
				want = listed
			}
			if !file.ModTime.Equal(want) {
				t.Fatalf("time = %s, want %s", file.ModTime, want)
			}
		})
	}
}
