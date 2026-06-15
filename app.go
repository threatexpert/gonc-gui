package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"gonc-gui/internal/goncrunner"
	"gonc-gui/internal/httpdownload"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context

	mu             sync.Mutex
	runner         *goncrunner.Runner
	reportServer   *http.Server
	localHTTPURL   string
	downloadCancel context.CancelFunc
	downloadID     int64
}

type TransferRequest struct {
	Mode            string   `json:"mode"`
	Password        string   `json:"password"`
	SharePaths      []string `json:"sharePaths"`
	SaveDir         string   `json:"saveDir"`
	GoncPath        string   `json:"goncPath"`
	DownloadSubPath string   `json:"downloadSubPath"`
	UseUDP          bool     `json:"useUDP"`
}

type AppStatus struct {
	Running      bool   `json:"running"`
	GoncPath     string `json:"goncPath"`
	LocalHTTPURL string `json:"localHTTPUrl"`
	Downloading  bool   `json:"downloading"`
}

type P2PStatusReport struct {
	Topic     string `json:"topic"`
	Status    string `json:"status"`
	Network   string `json:"network"`
	Mode      string `json:"mode"`
	Peer      string `json:"peer"`
	Timestamp int64  `json:"timestamp"`
	PID       int    `json:"pid"`
}

type RemoteListResponse struct {
	ServerURL string                  `json:"serverUrl"`
	Files     []httpdownload.FileInfo `json:"files"`
	FileCount int                     `json:"fileCount"`
	DirCount  int                     `json:"dirCount"`
	TotalSize int64                   `json:"totalSize"`
}

func NewApp() *App {
	return &App{
		runner: goncrunner.New(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	_ = a.cleanup(ctx)
}

func (a *App) SelectFiles() ([]string, error) {
	return wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select files to send",
	})
}

func (a *App) SelectFolder(title string) (string, error) {
	if title == "" {
		title = "Select folder"
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: title,
	})
}

func (a *App) GeneratePassword() (string, error) {
	return generateSecureRandomString(24)
}

func (a *App) Status() AppStatus {
	path, _ := a.LocateGonc("")
	a.mu.Lock()
	localURL := a.localHTTPURL
	downloading := a.downloadCancel != nil
	a.mu.Unlock()
	return AppStatus{
		Running:      a.runner.IsRunning(),
		GoncPath:     path,
		LocalHTTPURL: localURL,
		Downloading:  downloading,
	}
}

func (a *App) LocateGonc(preferred string) (string, error) {
	return findGonc(preferred)
}

func (a *App) StartTransfer(req TransferRequest) error {
	if a.ctx == nil {
		return errors.New("application is not ready")
	}

	goncPath, err := findGonc(req.GoncPath)
	if err != nil {
		return err
	}

	reportURL, reportServer, err := a.startReportServer()
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.localHTTPURL = ""
	if a.reportServer != nil {
		_ = a.reportServer.Close()
	}
	a.reportServer = reportServer
	a.mu.Unlock()

	err = a.runner.Start(a.ctx, goncPath, goncrunner.Request{
		Mode:            goncrunner.Mode(req.Mode),
		Password:        req.Password,
		SharePaths:      req.SharePaths,
		SaveDir:         req.SaveDir,
		DownloadSubPath: req.DownloadSubPath,
		UseUDP:          req.UseUDP,
		ReportURL:       reportURL,
	}, func(event goncrunner.Event) {
		if event.Type == "local_http" && event.LocalURL != "" {
			a.mu.Lock()
			a.localHTTPURL = event.LocalURL
			a.mu.Unlock()
		}
		wailsruntime.EventsEmit(a.ctx, "gonc:event", event)
	})
	if err != nil {
		_ = reportServer.Close()
		a.mu.Lock()
		if a.reportServer == reportServer {
			a.reportServer = nil
		}
		a.mu.Unlock()
	}
	return err
}

func (a *App) StopTransfer() error {
	return a.stopTransfer(true)
}

func (a *App) stopTransfer(requireRunning bool) error {
	a.StopHTTPDownload()
	a.mu.Lock()
	reportServer := a.reportServer
	a.reportServer = nil
	a.localHTTPURL = ""
	a.mu.Unlock()
	if reportServer != nil {
		_ = reportServer.Close()
	}
	stopped, err := a.runner.StopWait(5 * time.Second)
	if err != nil {
		if requireRunning {
			return err
		}
		return nil
	}
	if !stopped {
		return errors.New("gonc process did not exit within 5 seconds")
	}
	return nil
}

func (a *App) cleanup(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- a.stopTransfer(false)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(6 * time.Second):
		return errors.New("timed out waiting for gonc process cleanup")
	}
}

func (a *App) RemoteFiles(subPath string) (RemoteListResponse, error) {
	localURL := a.getLocalHTTPURL()
	if localURL == "" {
		return RemoteListResponse{}, errors.New("local HTTP endpoint is not ready")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	files, err := httpdownload.List(ctx, localURL, subPath)
	if err != nil {
		return RemoteListResponse{}, err
	}
	resp := RemoteListResponse{
		ServerURL: localURL,
		Files:     files,
	}
	for _, file := range files {
		if file.IsDir {
			resp.DirCount++
		} else {
			resp.FileCount++
			resp.TotalSize += file.Size
		}
	}
	return resp, nil
}

func (a *App) StartHTTPDownload(saveDir, subPath string) error {
	if saveDir == "" {
		return errors.New("select a save directory")
	}
	localURL := a.getLocalHTTPURL()
	if localURL == "" {
		return errors.New("local HTTP endpoint is not ready")
	}

	a.mu.Lock()
	if a.downloadCancel != nil {
		a.mu.Unlock()
		return errors.New("download is already running")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.downloadCancel = cancel
	a.downloadID++
	downloadID := a.downloadID
	a.mu.Unlock()

	d, err := httpdownload.New(httpdownload.Config{
		ServerURL:   localURL,
		SubPath:     subPath,
		SaveDir:     saveDir,
		Concurrency: 4,
		Resume:      true,
	})
	if err != nil {
		a.clearDownload(downloadID)
		return err
	}

	go func() {
		err := d.Start(ctx, func(event httpdownload.Event) {
			wailsruntime.EventsEmit(a.ctx, "download:event", event)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			wailsruntime.EventsEmit(a.ctx, "download:event", httpdownload.Event{
				Type:    "status",
				Level:   "error",
				Message: err.Error(),
				Time:    time.Now().Format(time.RFC3339),
			})
		}
		a.clearDownload(downloadID)
	}()
	return nil
}

func (a *App) StopHTTPDownload() error {
	a.mu.Lock()
	cancel := a.downloadCancel
	a.downloadCancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (a *App) getLocalHTTPURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.localHTTPURL
}

func (a *App) clearDownload(downloadID int64) {
	a.mu.Lock()
	if a.downloadID == downloadID {
		a.downloadCancel = nil
	}
	a.mu.Unlock()
}

func (a *App) startReportServer() (string, *http.Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}

	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	mux.HandleFunc("/p2p-report", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var report P2PStatusReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wailsruntime.EventsEmit(a.ctx, "p2p:report", report)
		w.WriteHeader(http.StatusNoContent)
	})

	go func() {
		_ = server.Serve(ln)
	}()
	return "http://" + ln.Addr().String() + "/p2p-report", server, nil
}

func findGonc(preferred string) (string, error) {
	if preferred != "" {
		if fileExists(preferred) {
			return preferred, nil
		}
		return "", errors.New("selected gonc executable does not exist")
	}

	name := "gonc"
	if runtime.GOOS == "windows" {
		name = "gonc.exe"
	}

	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "gonc", runtime.GOOS+"-"+runtime.GOARCH, name),
			filepath.Join(base, "bundled", "gonc", runtime.GOOS+"-"+runtime.GOARCH, name),
			filepath.Join(base, name),
		)
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "bundled", "gonc", runtime.GOOS+"-"+runtime.GOARCH, name),
			filepath.Join(wd, "..", "gonetcat", "bin", name),
			filepath.Join(wd, "..", "gonetcat", name),
		)
	}

	if path, err := exec.LookPath(name); err == nil {
		candidates = append(candidates, path)
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			abs, err := filepath.Abs(candidate)
			if err == nil {
				return abs, nil
			}
			return candidate, nil
		}
	}
	return "", errors.New("gonc executable was not found; make sure bundled/gonc/current-platform/gonc(.exe) exists or put gonc in PATH")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

const passwordCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateSecureRandomString(length int) (string, error) {
	for {
		result, err := generateRandomString(length)
		if err != nil {
			return "", err
		}
		if !isWeakPassword(result) {
			return result, nil
		}
	}
}

func generateRandomString(length int) (string, error) {
	result := make([]byte, length)
	max := big.NewInt(int64(len(passwordCharset)))
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		result[i] = passwordCharset[n.Int64()]
	}
	return string(result), nil
}

func isWeakPassword(password string) bool {
	if len(password) < 8 {
		return true
	}

	lowerPassword := strings.ToLower(password)
	weakList := []string{
		"123456", "password", "12345678", "qwerty", "abc123", "111111", "123123",
	}
	for _, weak := range weakList {
		if lowerPassword == weak {
			return true
		}
	}

	var hasLetter, hasDigit bool
	for _, c := range password {
		if unicode.IsLetter(c) {
			hasLetter = true
		}
		if unicode.IsDigit(c) {
			hasDigit = true
		}
	}
	return !hasLetter || !hasDigit
}
