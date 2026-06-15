package httpdownload

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zstd"
)

type FileInfo struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
	Size    int64     `json:"size"`
	Path    string    `json:"path"`
}

type Config struct {
	ServerURL    string
	SubPath      string
	SaveDir      string
	IncludePaths []string
	Concurrency  int
	Resume       bool
}

type Event struct {
	Type             string `json:"type"`
	Level            string `json:"level"`
	Message          string `json:"message"`
	Time             string `json:"time"`
	TotalFiles       int64  `json:"totalFiles,omitempty"`
	DoneFiles        int64  `json:"doneFiles,omitempty"`
	TotalBytes       int64  `json:"totalBytes,omitempty"`
	DoneBytes        int64  `json:"doneBytes,omitempty"`
	BytesPerSecond   int64  `json:"bytesPerSecond,omitempty"`
	CurrentFile      string `json:"currentFile,omitempty"`
	RemoteServerURL  string `json:"remoteServerUrl,omitempty"`
	RemoteServerPath string `json:"remoteServerPath,omitempty"`
}

type Sink func(Event)

type progress struct {
	totalFiles     atomic.Int64
	doneFiles      atomic.Int64
	totalBytes     atomic.Int64
	doneBytes      atomic.Int64
	intervalBytes  atomic.Int64
	lastSpeedNanos atomic.Int64
	lastEmitNanos  atomic.Int64
}

type Downloader struct {
	cfg       Config
	root      string
	files     []FileInfo
	progress  *progress
	countedMu sync.Mutex
	counted   map[string]int64
}

type httpStatusError struct {
	status string
	path   string
}

func (e httpStatusError) Error() string {
	return fmt.Sprintf("server returned %s for %s", e.status, e.path)
}

func New(cfg Config) (*Downloader, error) {
	if cfg.ServerURL == "" {
		return nil, errors.New("local HTTP URL is not ready")
	}
	if cfg.SaveDir == "" {
		return nil, errors.New("select a save directory")
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	root, err := filepath.Abs(cfg.SaveDir)
	if err != nil {
		return nil, err
	}
	cfg.SaveDir = root
	return &Downloader{
		cfg:  cfg,
		root: root,
		progress: &progress{
			lastSpeedNanos: atomic.Int64{},
		},
		counted: make(map[string]int64),
	}, nil
}

func List(ctx context.Context, serverURL, subPath string) ([]FileInfo, error) {
	reqURL, err := resolveURL(serverURL, subPath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "zstd, gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s for file list", resp.Status)
	}

	reader, closeReader, err := responseReader(resp)
	if err != nil {
		return nil, err
	}
	if closeReader != nil {
		defer closeReader()
	}

	var files []FileInfo
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var item FileInfo
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			continue
		}
		files = append(files, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		if file, ok := singleFileFromHeaders(resp); ok {
			return []FileInfo{file}, nil
		}
	}
	return files, nil
}

func (d *Downloader) Start(ctx context.Context, sink Sink) error {
	emit(sink, "status", "info", "fetching remote file list")
	files, err := d.fetchListWithRetry(ctx, sink)
	if err != nil {
		return err
	}
	files = filterFiles(files, d.cfg.IncludePaths)
	d.files = files
	for _, file := range files {
		d.progress.totalFiles.Add(1)
		if !file.IsDir {
			d.progress.totalBytes.Add(file.Size)
		}
	}
	d.progress.lastSpeedNanos.Store(time.Now().UnixNano())
	emitProgress(sink, d.progress, "", true)

	if err := os.MkdirAll(d.root, 0755); err != nil {
		return err
	}

	queue := make(chan FileInfo, d.cfg.Concurrency*2)
	var wg sync.WaitGroup
	var workerErrMu sync.Mutex
	var workerErr error
	setWorkerErr := func(err error) {
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		workerErrMu.Lock()
		defer workerErrMu.Unlock()
		if workerErr == nil {
			workerErr = err
		}
	}

	for i := 0; i < d.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{}
			for file := range queue {
				if ctx.Err() != nil {
					return
				}
				if err := d.downloadWithRetry(ctx, client, file, sink); err != nil {
					setWorkerErr(err)
					continue
				}
				d.progress.doneFiles.Add(1)
				emitProgress(sink, d.progress, file.Path, false)
			}
		}()
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			close(queue)
			wg.Wait()
			return ctx.Err()
		case queue <- file:
		}
	}
	close(queue)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	workerErrMu.Lock()
	err = workerErr
	workerErrMu.Unlock()
	if err != nil {
		return err
	}
	emitProgress(sink, d.progress, "", true)
	emit(sink, "status", "info", "download complete")
	return nil
}

func (d *Downloader) fetchListWithRetry(ctx context.Context, sink Sink) ([]FileInfo, error) {
	attempt := 0
	for {
		files, err := List(ctx, d.cfg.ServerURL, d.cfg.SubPath)
		if err == nil {
			if attempt > 0 {
				emit(sink, "status", "info", "remote file list is available again")
			}
			return files, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		attempt++
		emit(sink, "log", "warn", fmt.Sprintf("remote file list is unavailable, retrying: %v", err))
		emitProgress(sink, d.progress, "", true)
		if err := waitRetry(ctx, attempt); err != nil {
			return nil, err
		}
	}
}

func (d *Downloader) downloadWithRetry(ctx context.Context, client *http.Client, file FileInfo, sink Sink) error {
	if file.IsDir {
		return d.downloadOne(ctx, client, file, sink)
	}
	attempt := 0
	for {
		err := d.downloadOne(ctx, client, file, sink)
		if err == nil {
			if attempt > 0 {
				emit(sink, "status", "info", fmt.Sprintf("resumed %s", file.Path))
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var statusErr httpStatusError
		if errors.As(err, &statusErr) {
			emit(sink, "log", "error", fmt.Sprintf("download failed for %s: %v", file.Path, err))
			return err
		}
		attempt++
		emit(sink, "log", "warn", fmt.Sprintf("download interrupted for %s, retrying: %v", file.Path, err))
		emitProgress(sink, d.progress, file.Path, true)
		if err := waitRetry(ctx, attempt); err != nil {
			return err
		}
	}
}

func waitRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt) * time.Second
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Downloader) downloadOne(ctx context.Context, client *http.Client, file FileInfo, sink Sink) error {
	localPath, err := d.localPath(file)
	if err != nil {
		return err
	}
	if file.IsDir {
		return os.MkdirAll(localPath, 0755)
	}

	localExists := false
	localSize := int64(0)
	if stat, err := os.Stat(localPath); err == nil {
		localExists = true
		localSize = stat.Size()
	} else if !os.IsNotExist(err) {
		return err
	}

	fileMode := os.O_CREATE | os.O_WRONLY
	if d.cfg.Resume && localExists && localSize >= file.Size {
		d.setFileProgress(file.Path, file.Size)
		return nil
	}
	if d.cfg.Resume && localExists && localSize > 0 && localSize < file.Size {
		fileMode |= os.O_APPEND
	}

	downloadURL, err := resolveURL(d.cfg.ServerURL, file.Path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	if d.cfg.Resume && localExists && localSize > 0 && localSize < file.Size {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", localSize))
	}
	req.Header.Set("Accept-Encoding", "zstd, gzip")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	responseOffset := int64(0)
	if resp.StatusCode == http.StatusPartialContent {
		responseOffset = localSize
		d.setFileProgress(file.Path, localSize)
	} else if resp.StatusCode == http.StatusOK {
		fileMode = os.O_CREATE | os.O_WRONLY
		d.setFileProgress(file.Path, 0)
	} else {
		return httpStatusError{status: resp.Status, path: file.Path}
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(localPath, fileMode, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	if responseOffset > 0 {
		if _, err := out.Seek(responseOffset, io.SeekStart); err != nil {
			return err
		}
	}

	reader, closeReader, err := responseReader(resp)
	if err != nil {
		return err
	}
	if closeReader != nil {
		defer closeReader()
	}

	copied, err := io.Copy(&progressWriter{
		writer:     out,
		downloader: d,
		sink:       sink,
		file:       file.Path,
		offset:     responseOffset,
	}, reader)
	if err != nil {
		return err
	}

	if copied+responseOffset != file.Size {
		return fmt.Errorf("downloaded size mismatch for %s: got %d, expected %d", file.Path, copied+responseOffset, file.Size)
	}
	if !file.ModTime.IsZero() {
		_ = os.Chtimes(localPath, time.Now(), file.ModTime)
	}
	d.setFileProgress(file.Path, file.Size)
	return nil
}

func (d *Downloader) localPath(file FileInfo) (string, error) {
	remotePath := strings.TrimPrefix(path.Clean(file.Path), "/")
	if remotePath == "." || remotePath == "" {
		remotePath = safeLocalFilename(file.Name)
	}
	localPath := filepath.Clean(filepath.Join(d.root, filepath.FromSlash(remotePath)))
	rel, err := filepath.Rel(d.root, localPath)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("remote path escapes save directory: %s", file.Path)
	}
	return localPath, nil
}

func singleFileFromHeaders(resp *http.Response) (FileInfo, bool) {
	disposition := resp.Header.Get("Content-Disposition")
	name := ""
	if disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			name = params["filename"]
		}
	}
	name = safeLocalFilename(name)
	if name == "" {
		return FileInfo{}, false
	}

	size := resp.ContentLength
	if size < 0 {
		size = 0
	}
	return FileInfo{
		Name:    name,
		IsDir:   false,
		ModTime: headerModTime(resp.Header.Get("Last-Modified")),
		Size:    size,
		Path:    "/",
	}, true
}

func safeLocalFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(path.Clean("/" + name))
	name = strings.TrimSpace(name)
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return name
}

func headerModTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if t, err := http.ParseTime(value); err == nil {
		return t
	}
	return time.Time{}
}

func (d *Downloader) setFileProgress(file string, absoluteBytes int64) int64 {
	if absoluteBytes < 0 {
		absoluteBytes = 0
	}
	d.countedMu.Lock()
	previous := d.counted[file]
	if previous == absoluteBytes {
		d.countedMu.Unlock()
		return 0
	}
	d.counted[file] = absoluteBytes
	d.countedMu.Unlock()
	delta := absoluteBytes - previous
	d.progress.doneBytes.Add(delta)
	return delta
}

type progressWriter struct {
	writer     io.Writer
	downloader *Downloader
	sink       Sink
	file       string
	offset     int64
	written    int64
	lastEmit   time.Time
}

func (w *progressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n > 0 {
		w.written += int64(n)
		delta := w.downloader.setFileProgress(w.file, w.offset+w.written)
		if delta > 0 {
			w.downloader.progress.intervalBytes.Add(delta)
		}
		if time.Since(w.lastEmit) > time.Second {
			w.lastEmit = time.Now()
			emitProgress(w.sink, w.downloader.progress, w.file, false)
		}
	}
	return n, err
}

func filterFiles(files []FileInfo, includePaths []string) []FileInfo {
	if len(includePaths) == 0 {
		return files
	}
	include := make([]string, 0, len(includePaths))
	for _, item := range includePaths {
		normalized := normalizeRemotePath(item)
		if normalized != "" {
			include = append(include, normalized)
		}
	}
	if len(include) == 0 {
		return files
	}

	var filtered []FileInfo
	for _, file := range files {
		filePath := normalizeRemotePath(file.Path)
		for _, selected := range include {
			if filePath == selected || strings.HasPrefix(filePath, strings.TrimRight(selected, "/")+"/") {
				filtered = append(filtered, file)
				break
			}
		}
	}
	return filtered
}

func normalizeRemotePath(remotePath string) string {
	cleaned := path.Clean(remotePath)
	if cleaned == "." {
		return "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func responseReader(resp *http.Response) (io.Reader, func(), error) {
	switch resp.Header.Get("Content-Encoding") {
	case "zstd":
		reader, err := zstd.NewReader(resp.Body)
		if err != nil {
			return nil, nil, err
		}
		return reader, reader.Close, nil
	case "gzip":
		reader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, nil, err
		}
		return reader, func() { _ = reader.Close() }, nil
	default:
		return resp.Body, nil, nil
	}
}

func resolveURL(serverURL, remotePath string) (string, error) {
	base, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	cleanPath := path.Clean(remotePath)
	if cleanPath == "." {
		cleanPath = "/"
	}
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}
	base.Path = cleanPath
	base.RawPath = encodePath(cleanPath)
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func encodePath(remotePath string) string {
	parts := strings.Split(remotePath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	encoded := strings.Join(parts, "/")
	if !strings.HasPrefix(encoded, "/") {
		encoded = "/" + encoded
	}
	return encoded
}

func emit(sink Sink, typ, level, message string) {
	if sink == nil {
		return
	}
	sink(Event{
		Type:    typ,
		Level:   level,
		Message: message,
		Time:    time.Now().Format(time.RFC3339),
	})
}

func emitProgress(sink Sink, p *progress, file string, force bool) {
	if sink == nil {
		return
	}
	now := time.Now()
	nowNano := now.UnixNano()
	if !force {
		lastEmit := p.lastEmitNanos.Load()
		if lastEmit > 0 && nowNano-lastEmit < int64(time.Second) {
			return
		}
		if !p.lastEmitNanos.CompareAndSwap(lastEmit, nowNano) {
			return
		}
	} else {
		p.lastEmitNanos.Store(nowNano)
	}
	last := time.Unix(0, p.lastSpeedNanos.Swap(now.UnixNano()))
	seconds := now.Sub(last).Seconds()
	speed := int64(0)
	if seconds > 0 {
		speed = int64(float64(p.intervalBytes.Swap(0)) / seconds)
	}
	sink(Event{
		Type:           "progress",
		Level:          "info",
		Message:        "download progress",
		Time:           now.Format(time.RFC3339),
		TotalFiles:     p.totalFiles.Load(),
		DoneFiles:      p.doneFiles.Load(),
		TotalBytes:     p.totalBytes.Load(),
		DoneBytes:      p.doneBytes.Load(),
		BytesPerSecond: speed,
		CurrentFile:    file,
	})
}
