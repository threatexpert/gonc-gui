package httpdownload

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	ServerURL   string
	SubPath     string
	SaveDir     string
	Concurrency int
	Resume      bool
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
}

type Downloader struct {
	cfg      Config
	root     string
	files    []FileInfo
	progress *progress
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
	return files, nil
}

func (d *Downloader) Start(ctx context.Context, sink Sink) error {
	emit(sink, "status", "info", "fetching remote file list")
	files, err := List(ctx, d.cfg.ServerURL, d.cfg.SubPath)
	if err != nil {
		return err
	}
	d.files = files
	for _, file := range files {
		d.progress.totalFiles.Add(1)
		if !file.IsDir {
			d.progress.totalBytes.Add(file.Size)
		}
	}
	d.progress.lastSpeedNanos.Store(time.Now().UnixNano())
	emitProgress(sink, d.progress, "")

	if err := os.MkdirAll(d.root, 0755); err != nil {
		return err
	}

	queue := make(chan FileInfo, d.cfg.Concurrency*2)
	var wg sync.WaitGroup
	for i := 0; i < d.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{}
			for file := range queue {
				if ctx.Err() != nil {
					return
				}
				if err := d.downloadOne(ctx, client, file, sink); err != nil {
					emit(sink, "log", "error", err.Error())
					continue
				}
				d.progress.doneFiles.Add(1)
				emitProgress(sink, d.progress, file.Path)
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
	emitProgress(sink, d.progress, "")
	emit(sink, "status", "info", "download complete")
	return nil
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
		d.addExistingBytes(localSize)
		return nil
	}
	if d.cfg.Resume && localExists && localSize > 0 && localSize < file.Size {
		fileMode |= os.O_APPEND
		d.addExistingBytes(localSize)
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
	} else if resp.StatusCode == http.StatusOK {
		fileMode = os.O_CREATE | os.O_WRONLY
	} else {
		return fmt.Errorf("server returned %s for %s", resp.Status, file.Path)
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
		writer:   out,
		progress: d.progress,
		sink:     sink,
		file:     file.Path,
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
	return nil
}

func (d *Downloader) localPath(file FileInfo) (string, error) {
	remotePath := strings.TrimPrefix(path.Clean(file.Path), "/")
	if remotePath == "." || remotePath == "" {
		remotePath = file.Name
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

func (d *Downloader) addExistingBytes(n int64) {
	if n <= 0 {
		return
	}
	d.progress.doneBytes.Add(n)
}

type progressWriter struct {
	writer   io.Writer
	progress *progress
	sink     Sink
	file     string
	lastEmit time.Time
}

func (w *progressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n > 0 {
		w.progress.doneBytes.Add(int64(n))
		w.progress.intervalBytes.Add(int64(n))
		if time.Since(w.lastEmit) > 500*time.Millisecond {
			w.lastEmit = time.Now()
			emitProgress(w.sink, w.progress, w.file)
		}
	}
	return n, err
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

func emitProgress(sink Sink, p *progress, file string) {
	if sink == nil {
		return
	}
	now := time.Now()
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
