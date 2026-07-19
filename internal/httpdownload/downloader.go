package httpdownload

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
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
	"github.com/zeebo/blake3"
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
	ExcludePaths []string
	CachedFiles  []FileInfo
	Concurrency  int
	Resume       bool
}

type Event struct {
	Type             string `json:"type"`
	Level            string `json:"level"`
	Message          string `json:"message"`
	Time             string `json:"time"`
	ClientTaskID     int64  `json:"clientTaskId,omitempty"`
	TotalFiles       int64  `json:"totalFiles,omitempty"`
	DoneFiles        int64  `json:"doneFiles,omitempty"`
	TotalDirs        int64  `json:"totalDirs,omitempty"`
	DoneDirs         int64  `json:"doneDirs,omitempty"`
	SkippedFiles     int64  `json:"skippedFiles,omitempty"`
	ResumedFiles     int64  `json:"resumedFiles,omitempty"`
	FailedFiles      int64  `json:"failedFiles,omitempty"`
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
	totalDirs      atomic.Int64
	doneDirs       atomic.Int64
	skippedFiles   atomic.Int64
	resumedFiles   atomic.Int64
	failedFiles    atomic.Int64
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

var (
	errManifestUnsupported = errors.New("BLAKE3 manifest unsupported")
	errRangeUnsupported    = errors.New("range repair unsupported")
	errUnsupportedEncoding = errors.New("unsupported content encoding")
)

const (
	manifestAlgo             = "blake3"
	defaultManifestBlockSize = int64(8 * 1024 * 1024)
	minManifestBlockSize     = int64(64 * 1024)
	maxManifestBlockSize     = int64(64 * 1024 * 1024)
	maxRepairRangeCount      = 128
)

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
	var files []FileInfo
	if len(d.cfg.CachedFiles) > 0 {
		emit(sink, "status", "info", "using selected remote file list")
		files = append([]FileInfo(nil), d.cfg.CachedFiles...)
	} else {
		emit(sink, "status", "info", "fetching remote file list")
		var err error
		files, err = d.fetchListWithRetry(ctx, sink)
		if err != nil {
			return err
		}
	}
	files = filterFiles(files, d.cfg.IncludePaths, d.cfg.ExcludePaths)
	files = dropRootDirectoryEntries(files)
	d.files = files
	for _, file := range files {
		if file.IsDir {
			d.progress.totalDirs.Add(1)
		} else {
			d.progress.totalFiles.Add(1)
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
					if !file.IsDir {
						d.progress.failedFiles.Add(1)
					}
					setWorkerErr(err)
					emitProgress(sink, d.progress, file.Path, true)
					continue
				}
				if file.IsDir {
					d.progress.doneDirs.Add(1)
				} else {
					d.progress.doneFiles.Add(1)
				}
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
	finalErr := workerErr
	workerErrMu.Unlock()
	emitProgress(sink, d.progress, "", true)
	if finalErr != nil {
		emit(sink, "status", "error", fmt.Sprintf("download finished with %d failed files", d.progress.failedFiles.Load()))
		return finalErr
	}
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
	localModTime := time.Time{}
	if stat, err := os.Stat(localPath); err == nil {
		localExists = true
		localSize = stat.Size()
		localModTime = stat.ModTime()
	} else if !os.IsNotExist(err) {
		return err
	}

	downloadURL, err := resolveURL(d.cfg.ServerURL, file.Path)
	if err != nil {
		return err
	}

	if d.cfg.Resume && localExists && localSize == file.Size && !file.ModTime.IsZero() && localModTime.Equal(file.ModTime) {
		d.setFileProgress(file.Path, file.Size)
		d.progress.skippedFiles.Add(1)
		return nil
	}

	if d.cfg.Resume && localExists {
		repaired, downloadedBytes, err := d.repairFile(ctx, client, downloadURL, localPath, file, localSize, sink)
		if err != nil {
			return err
		}
		if repaired {
			if downloadedBytes > 0 {
				d.progress.resumedFiles.Add(1)
			} else {
				d.progress.skippedFiles.Add(1)
			}
			return nil
		}
		emit(sink, "log", "info", fmt.Sprintf("BLAKE3 repair unavailable or inefficient for %s; re-downloading full file", file.Path))
	}

	return d.downloadFullFile(ctx, client, downloadURL, localPath, file, sink)
}

func (d *Downloader) downloadFullFile(ctx context.Context, client *http.Client, downloadURL, localPath string, file FileInfo, sink Sink) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept-Encoding", "zstd, gzip")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return httpStatusError{status: resp.Status, path: file.Path}
	}
	d.setFileProgress(file.Path, 0)

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(localPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

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
	}, reader)
	if err != nil {
		return err
	}

	if copied != file.Size {
		return fmt.Errorf("downloaded size mismatch for %s: got %d, expected %d", file.Path, copied, file.Size)
	}
	if !file.ModTime.IsZero() {
		_ = os.Chtimes(localPath, time.Now(), file.ModTime)
	}
	d.setFileProgress(file.Path, file.Size)
	return nil
}

type blake3Manifest struct {
	Path         string
	Size         int64
	ModTime      time.Time
	BlockSize    int64
	ManifestSize int64
	LimitSize    int64
	Blocks       []blake3Block
}

type blake3Block struct {
	Type   string `json:"type"`
	Index  int    `json:"index"`
	Offset int64  `json:"offset"`
	Size   int64  `json:"size"`
	Hash   string `json:"hash"`
}

type repairRange struct {
	Offset int64
	Size   int64
}

type repairPlanKind string

const (
	repairPlanSparse       repairPlanKind = "SparseRepair"
	repairPlanTailResume   repairPlanKind = "TailResume"
	repairPlanTruncateOnly repairPlanKind = "TruncateOnly"
)

type repairPlan struct {
	Ranges          []repairRange
	TransferBytes   int64
	DirtyBytes      int64
	DirtyRangeCount int
	Kind            repairPlanKind
}

func (r repairRange) endInclusive() int64 {
	return r.Offset + r.Size - 1
}

func (d *Downloader) repairFile(ctx context.Context, client *http.Client, downloadURL, localPath string, file FileInfo, localSize int64, sink Sink) (bool, int64, error) {
	manifest, plan, err := d.prepareRepairPlan(ctx, client, downloadURL, localPath, file, localSize, sink)
	if err != nil {
		if errors.Is(err, errManifestUnsupported) {
			emit(sink, "log", "info", fmt.Sprintf("Repair check unavailable for %s: server does not provide BLAKE3 manifest; falling back to full download", file.Path))
			return false, 0, nil
		}
		return false, 0, err
	}
	if plan.Kind == repairPlanSparse && shouldRedownload(manifest.Size, plan.DirtyBytes, plan.DirtyRangeCount) {
		emit(sink, "log", "info", fmt.Sprintf("Repair check for %s: local=%s remote=%s, %d dirty range(s) totaling %s; full download selected",
			file.Path, formatBytes(localSize), formatBytes(manifest.Size), plan.DirtyRangeCount, formatBytes(plan.DirtyBytes)))
		return false, 0, nil
	}

	keptBytes := manifest.Size - plan.TransferBytes
	if keptBytes < 0 {
		keptBytes = 0
	}
	truncateBytes := int64(0)
	if localSize > manifest.Size {
		truncateBytes = localSize - manifest.Size
	}
	emit(sink, "log", "info", fmt.Sprintf("Repair plan for %s: kind=%s, local=%s remote=%s, keep %s, download %s in %d range request(s), dirty %s in %d range(s), truncate %s",
		file.Path, plan.Kind, formatBytes(localSize), formatBytes(manifest.Size), formatBytes(keptBytes), formatBytes(plan.TransferBytes), len(plan.Ranges), formatBytes(plan.DirtyBytes), plan.DirtyRangeCount, formatBytes(truncateBytes)))
	d.setFileProgress(file.Path, keptBytes)

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return false, 0, err
	}
	out, err := os.OpenFile(localPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return false, 0, err
	}
	defer out.Close()

	for _, repairRange := range plan.Ranges {
		if err := d.downloadRange(ctx, client, downloadURL, out, file.Path, repairRange, sink); err != nil {
			if errors.Is(err, errRangeUnsupported) {
				return false, 0, nil
			}
			return false, 0, err
		}
	}

	if err := out.Truncate(manifest.Size); err != nil {
		return false, 0, err
	}
	if !manifest.ModTime.IsZero() {
		_ = os.Chtimes(localPath, time.Now(), manifest.ModTime)
	}
	d.setFileProgress(file.Path, manifest.Size)
	emit(sink, "log", "info", fmt.Sprintf("Repair completed for %s: %d range request(s), downloaded %s, final size %s",
		file.Path, len(plan.Ranges), formatBytes(plan.TransferBytes), formatBytes(manifest.Size)))
	return true, plan.TransferBytes, nil
}

func (d *Downloader) prepareRepairPlan(ctx context.Context, client *http.Client, downloadURL, localPath string, file FileInfo, localSize int64, sink Sink) (*blake3Manifest, repairPlan, error) {
	manifestLimit := int64(0)
	if localSize < file.Size {
		manifestLimit = localSize
	}

	manifest, err := d.fetchManifest(ctx, client, downloadURL, manifestLimit)
	if err != nil && manifestLimit > 0 && errors.Is(err, errManifestUnsupported) {
		manifest, err = d.fetchManifest(ctx, client, downloadURL, 0)
	}
	if err != nil {
		return nil, repairPlan{}, err
	}
	if manifest.Size != file.Size || !manifest.ModTime.Equal(file.ModTime) {
		file.Size = manifest.Size
		file.ModTime = manifest.ModTime
	}

	if localSize < manifest.Size {
		plan, ok, err := planTailResume(localPath, localSize, manifest)
		if err != nil {
			return nil, repairPlan{}, err
		}
		if ok {
			return manifest, plan, nil
		}
		emit(sink, "log", "info", fmt.Sprintf("Tail resume check for %s did not prove a clean prefix; requesting full repair manifest", file.Path))
		if !manifest.isFull() {
			manifest, err = d.fetchManifest(ctx, client, downloadURL, 0)
			if err != nil {
				return nil, repairPlan{}, err
			}
			if manifest.Size != file.Size || !manifest.ModTime.Equal(file.ModTime) {
				file.Size = manifest.Size
				file.ModTime = manifest.ModTime
			}
		}
	}

	plan, err := planRepair(localPath, localSize, manifest)
	if err != nil {
		return nil, repairPlan{}, err
	}
	return manifest, plan, nil
}

func (m *blake3Manifest) isFull() bool {
	if m == nil {
		return false
	}
	return m.ManifestSize <= 0 || m.ManifestSize >= m.Size
}

func (d *Downloader) fetchManifest(ctx context.Context, client *http.Client, downloadURL string, limitSize int64) (*blake3Manifest, error) {
	manifestURL, err := url.Parse(downloadURL)
	if err != nil {
		return nil, err
	}
	q := manifestURL.Query()
	q.Set("manifest", manifestAlgo)
	q.Set("block_size", fmt.Sprintf("%d", defaultManifestBlockSize))
	if limitSize > 0 {
		q.Set("limit_size", fmt.Sprintf("%d", limitSize))
	}
	manifestURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "zstd, gzip")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errManifestUnsupported
	}

	reader, closeReader, err := responseReaderStrict(resp)
	if err != nil {
		if errors.Is(err, errUnsupportedEncoding) {
			return nil, errManifestUnsupported
		}
		return nil, err
	}
	if closeReader != nil {
		defer closeReader()
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var manifest blake3Manifest
	for scanner.Scan() {
		line := scanner.Bytes()
		var typed struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &typed); err != nil {
			return nil, errManifestUnsupported
		}
		switch typed.Type {
		case "file":
			var header struct {
				Type         string `json:"type"`
				Path         string `json:"path"`
				Size         int64  `json:"size"`
				ModTime      string `json:"mod_time"`
				Algo         string `json:"algo"`
				BlockSize    int64  `json:"block_size"`
				ManifestSize int64  `json:"manifest_size"`
				LimitSize    int64  `json:"limit_size"`
			}
			if err := json.Unmarshal(line, &header); err != nil || header.Algo != manifestAlgo {
				return nil, errManifestUnsupported
			}
			modTime, err := time.Parse(time.RFC3339Nano, header.ModTime)
			if err != nil {
				return nil, errManifestUnsupported
			}
			manifest.Path = header.Path
			manifest.Size = header.Size
			manifest.ModTime = modTime
			manifest.BlockSize = normalizeManifestBlockSize(header.BlockSize)
			manifest.ManifestSize = header.ManifestSize
			manifest.LimitSize = header.LimitSize
		case "block":
			var block blake3Block
			if err := json.Unmarshal(line, &block); err != nil {
				return nil, errManifestUnsupported
			}
			manifest.Blocks = append(manifest.Blocks, block)
		default:
			return nil, errManifestUnsupported
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if manifest.BlockSize <= 0 && manifest.Size > 0 {
		return nil, errManifestUnsupported
	}
	return &manifest, nil
}

func planTailResume(localPath string, localSize int64, manifest *blake3Manifest) (repairPlan, bool, error) {
	if manifest == nil || localSize >= manifest.Size {
		return repairPlan{}, false, nil
	}
	blockSize := normalizeManifestBlockSize(manifest.BlockSize)
	prefixSize := localSize / blockSize * blockSize
	localBlocks, err := localBlockHashes(localPath, prefixSize, blockSize)
	if err != nil {
		return repairPlan{}, false, err
	}
	remoteBlocks := make(map[int]blake3Block, len(manifest.Blocks))
	for _, block := range manifest.Blocks {
		remoteBlocks[block.Index] = block
	}
	for _, localBlock := range localBlocks {
		remoteBlock, ok := remoteBlocks[localBlock.Index]
		if !ok || remoteBlock.Offset != localBlock.Offset || remoteBlock.Size != localBlock.Size || remoteBlock.Hash != localBlock.Hash {
			return repairPlan{}, false, nil
		}
	}
	transferBytes := manifest.Size - prefixSize
	if transferBytes <= 0 {
		return repairPlan{Kind: repairPlanTailResume}, true, nil
	}
	return repairPlan{
		Ranges:        []repairRange{{Offset: prefixSize, Size: transferBytes}},
		TransferBytes: transferBytes,
		Kind:          repairPlanTailResume,
	}, true, nil
}

func planRepair(localPath string, localSize int64, manifest *blake3Manifest) (repairPlan, error) {
	compareSize := localSize
	if compareSize > manifest.Size {
		compareSize = manifest.Size
	}
	localBlocks, err := localBlockHashes(localPath, compareSize, manifest.BlockSize)
	if err != nil {
		return repairPlan{}, err
	}

	var ranges []repairRange
	var dirtyRanges []repairRange
	for _, remoteBlock := range manifest.Blocks {
		needsDownload := remoteBlock.Offset+remoteBlock.Size > localSize
		dirty := false
		if !needsDownload {
			if remoteBlock.Index >= len(localBlocks) {
				needsDownload = true
				dirty = true
			} else {
				localBlock := localBlocks[remoteBlock.Index]
				needsDownload = localBlock.Size != remoteBlock.Size || localBlock.Hash != remoteBlock.Hash
				dirty = needsDownload
			}
		}
		if needsDownload {
			repairRange := repairRange{Offset: remoteBlock.Offset, Size: remoteBlock.Size}
			ranges = append(ranges, repairRange)
			if dirty {
				dirtyRanges = append(dirtyRanges, repairRange)
			}
		}
	}
	ranges = mergeRanges(ranges)
	dirtyRanges = mergeRanges(dirtyRanges)
	var transferBytes int64
	for _, repairRange := range ranges {
		transferBytes += repairRange.Size
	}
	var dirtyBytes int64
	for _, repairRange := range dirtyRanges {
		dirtyBytes += repairRange.Size
	}
	kind := repairPlanSparse
	if transferBytes == 0 && localSize > manifest.Size {
		kind = repairPlanTruncateOnly
	}
	return repairPlan{
		Ranges:          ranges,
		TransferBytes:   transferBytes,
		DirtyBytes:      dirtyBytes,
		DirtyRangeCount: len(dirtyRanges),
		Kind:            kind,
	}, nil
}

func localBlockHashes(localPath string, fileSize, blockSize int64) ([]blake3Block, error) {
	if fileSize < 0 {
		return nil, fmt.Errorf("negative file size")
	}
	blockSize = normalizeManifestBlockSize(blockSize)
	blockCount := int((fileSize + blockSize - 1) / blockSize)
	blocks := make([]blake3Block, 0, blockCount)
	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buffer := make([]byte, blockSize)
	for index := 0; index < blockCount; index++ {
		offset := int64(index) * blockSize
		size := blockSize
		if remaining := fileSize - offset; remaining < size {
			size = remaining
		}
		chunk := buffer[:size]
		if _, err := io.ReadFull(file, chunk); err != nil {
			return nil, err
		}
		sum := blake3.Sum256(chunk)
		blocks = append(blocks, blake3Block{
			Type:   "block",
			Index:  index,
			Offset: offset,
			Size:   size,
			Hash:   hex.EncodeToString(sum[:]),
		})
	}
	return blocks, nil
}

func normalizeManifestBlockSize(blockSize int64) int64 {
	if blockSize <= 0 {
		return defaultManifestBlockSize
	}
	if blockSize < minManifestBlockSize {
		return minManifestBlockSize
	}
	if blockSize > maxManifestBlockSize {
		return maxManifestBlockSize
	}
	return blockSize
}

func mergeRanges(ranges []repairRange) []repairRange {
	if len(ranges) < 2 {
		return ranges
	}
	merged := ranges[:0]
	for _, current := range ranges {
		if len(merged) == 0 {
			merged = append(merged, current)
			continue
		}
		last := &merged[len(merged)-1]
		if last.Offset+last.Size == current.Offset {
			last.Size += current.Size
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func shouldRedownload(remoteSize, transferBytes int64, rangeCount int) bool {
	if remoteSize == 0 {
		return false
	}
	return rangeCount > maxRepairRangeCount || transferBytes*2 > remoteSize
}

func (d *Downloader) downloadRange(ctx context.Context, client *http.Client, downloadURL string, out *os.File, filePath string, repairRange repairRange, sink Sink) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", repairRange.Offset, repairRange.endInclusive()))
	req.Header.Set("Accept-Encoding", "zstd, gzip")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return errRangeUnsupported
	}

	reader, closeReader, err := responseReaderStrict(resp)
	if err != nil {
		return errRangeUnsupported
	}
	if closeReader != nil {
		defer closeReader()
	}

	writer := &repairProgressWriter{
		writer:     &writeAtWriter{file: out, offset: repairRange.Offset},
		downloader: d,
		sink:       sink,
		file:       filePath,
	}
	written, err := io.Copy(writer, reader)
	if err != nil {
		return err
	}
	if written != repairRange.Size {
		return fmt.Errorf("range repair wrote %d bytes for %s range %d-%d, want %d", written, filePath, repairRange.Offset, repairRange.endInclusive(), repairRange.Size)
	}
	return nil
}

type writeAtWriter struct {
	file   *os.File
	offset int64
}

func (w *writeAtWriter) Write(data []byte) (int, error) {
	n, err := w.file.WriteAt(data, w.offset)
	w.offset += int64(n)
	return n, err
}

type repairProgressWriter struct {
	writer     io.Writer
	downloader *Downloader
	sink       Sink
	file       string
	lastEmit   time.Time
}

func (w *repairProgressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n > 0 {
		w.downloader.addFileProgress(w.file, int64(n))
		w.downloader.progress.intervalBytes.Add(int64(n))
		if time.Since(w.lastEmit) > time.Second {
			w.lastEmit = time.Now()
			emitProgress(w.sink, w.downloader.progress, w.file, false)
		}
	}
	return n, err
}

func (d *Downloader) localPath(file FileInfo) (string, error) {
	return ResolveLocalPath(d.root, file)
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

func (d *Downloader) addFileProgress(file string, delta int64) {
	if delta <= 0 {
		return
	}
	d.countedMu.Lock()
	d.counted[file] += delta
	d.countedMu.Unlock()
	d.progress.doneBytes.Add(delta)
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

func filterFiles(files []FileInfo, includePaths []string, excludePaths []string) []FileInfo {
	include := normalizePathSet(includePaths)
	exclude := normalizePathSet(excludePaths)
	if len(include) == 0 && len(exclude) == 0 {
		return files
	}

	var filtered []FileInfo
	for _, file := range files {
		if shouldIncludePath(normalizeRemotePath(file.Path), include, exclude) {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

func normalizePathSet(paths []string) []string {
	normalized := make([]string, 0, len(paths))
	for _, item := range paths {
		cleaned := normalizeRemotePath(item)
		if cleaned != "" {
			cleaned = strings.TrimRight(cleaned, "/")
			if cleaned == "" {
				cleaned = "/"
			}
			normalized = appendUniquePath(normalized, cleaned)
		}
	}
	return normalized
}

func appendUniquePath(paths []string, item string) []string {
	for _, existing := range paths {
		if existing == item {
			return paths
		}
	}
	return append(paths, item)
}

func shouldIncludePath(filePath string, include, exclude []string) bool {
	includeRank := longestPathMatch(filePath, include)
	if len(include) > 0 && includeRank < 0 {
		return false
	}
	if len(include) == 0 {
		includeRank = 0
	}
	excludeRank := longestPathMatch(filePath, exclude)
	return excludeRank < 0 || includeRank > excludeRank
}

func longestPathMatch(filePath string, paths []string) int {
	filePath = strings.TrimRight(normalizeRemotePath(filePath), "/")
	if filePath == "" {
		filePath = "/"
	}
	best := -1
	for _, item := range paths {
		if item == "/" {
			if best < 0 {
				best = 0
			}
			continue
		}
		if filePath == item || strings.HasPrefix(filePath, item+"/") {
			if rank := strings.Count(item, "/") + 1; rank > best {
				best = rank
			}
		}
	}
	return best
}

func dropRootDirectoryEntries(files []FileInfo) []FileInfo {
	filtered := files[:0]
	for _, file := range files {
		if file.IsDir && normalizeRemotePath(file.Path) == "/" {
			continue
		}
		filtered = append(filtered, file)
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

func responseReaderStrict(resp *http.Response) (io.Reader, func(), error) {
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
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
	case "", "identity":
		return resp.Body, nil, nil
	default:
		return nil, nil, errUnsupportedEncoding
	}
}

func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(value)/float64(div), "KMGTPE"[exp])
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
		TotalDirs:      p.totalDirs.Load(),
		DoneDirs:       p.doneDirs.Load(),
		SkippedFiles:   p.skippedFiles.Load(),
		ResumedFiles:   p.resumedFiles.Load(),
		FailedFiles:    p.failedFiles.Load(),
		TotalBytes:     p.totalBytes.Load(),
		DoneBytes:      p.doneBytes.Load(),
		BytesPerSecond: speed,
		CurrentFile:    file,
	})
}
