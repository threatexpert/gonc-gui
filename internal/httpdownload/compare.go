package httpdownload

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
)

// ListForComparison deliberately does not use List's lenient parser or the UI
// cache. Older senders can omit unreadable paths, so absence in this list alone
// is never sufficient to authorize deletion; RemotePathMissing checks it again.
func ListForComparison(ctx context.Context, serverURL string, visit func(FileInfo) error) error {
	u, err := resolveURL(serverURL, "/")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "zstd, gzip")
	resp, err := receiverHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK || (mediaType != "application/x-ndjson" && mediaType != "application/json") {
		return fmt.Errorf("invalid comparison list response: %s (%s)", resp.Status, mediaType)
	}
	reader, closeReader, err := responseReader(resp)
	if err != nil {
		return err
	}
	if closeReader != nil {
		defer closeReader()
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var record struct {
			Path    *string   `json:"path"`
			IsDir   *bool     `json:"is_dir"`
			Name    string    `json:"name"`
			Size    *int64    `json:"size"`
			ModTime time.Time `json:"mod_time"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.Path == nil || record.IsDir == nil || strings.TrimSpace(*record.Path) == "" {
			return fmt.Errorf("invalid record in comparison list")
		}
		size := int64(-1) // unknown is distinct from a real zero-byte file
		if record.Size != nil {
			size = *record.Size
		}
		if err := visit(FileInfo{Path: *record.Path, IsDir: *record.IsDir, Name: record.Name, Size: size, ModTime: record.ModTime}); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// A HEAD request checks the sender's actual Stat/Open result without reading
// file contents. Only an explicit 404 means missing. Redirects (directories),
// permission errors, unsupported HEAD, and transport errors never mean absent.
func RemotePathMissing(ctx context.Context, serverURL, remotePath string) (bool, error) {
	u, err := resolveURL(serverURL, remotePath)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return false, err
	}
	client := *receiverHTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return true, nil
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return false, nil
	}
	return false, fmt.Errorf("cannot verify remote path %s: %s", remotePath, resp.Status)
}

// Refresh size just before receiving a selected result. No contents are read;
// a missing file, directory redirect, or inaccessible target is a failed item.
func RefreshComparisonFile(ctx context.Context, serverURL string, file FileInfo) (FileInfo, error) {
	u, err := resolveURL(serverURL, file.Path)
	if err != nil {
		return file, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return file, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	client := *receiverHTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return file, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength < 0 {
		return file, fmt.Errorf("cannot refresh remote file %s: %s", file.Path, resp.Status)
	}
	file.Size = resp.ContentLength
	modified, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	// HEAD cannot supply fractions of a second. Retain the list's precise
	// timestamp when HEAD agrees with its second, so time-only repairs do not
	// incorrectly skip a local file whose timestamp was truncated to seconds.
	// A newer second (or unavailable metadata) must invalidate that timestamp.
	if modified.IsZero() || file.ModTime.IsZero() || modified.Unix() != file.ModTime.Unix() {
		file.ModTime = modified
	}
	return file, nil
}
