package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
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

	mu                  sync.Mutex
	sendRunner          *goncrunner.Runner
	receiveRunner       *goncrunner.Runner
	vpnServerRunner     *goncrunner.Runner
	reportServer        *http.Server
	reportURL           string
	receiveLocalHTTPURL string
	downloadCancel      context.CancelFunc
	downloadID          int64
}

type TransferRequest struct {
	Mode            string   `json:"mode"`
	Password        string   `json:"password"`
	SharePaths      []string `json:"sharePaths"`
	SaveDir         string   `json:"saveDir"`
	GoncPath        string   `json:"goncPath"`
	DownloadSubPath string   `json:"downloadSubPath"`
	UseUDP          bool     `json:"useUDP"`
	Upstream        string   `json:"upstream"`
	DNSForward      string   `json:"dnsForward"`
	ExtraArgs       string   `json:"extraArgs"`
}

type AppStatus struct {
	Running          bool   `json:"running"`
	SendRunning      bool   `json:"sendRunning"`
	ReceiveRunning   bool   `json:"receiveRunning"`
	VPNServerRunning bool   `json:"vpnServerRunning"`
	GoncPath         string `json:"goncPath"`
	LocalHTTPURL     string `json:"localHTTPUrl"`
	Downloading      bool   `json:"downloading"`
	DefaultSaveDir   string `json:"defaultSaveDir"`
}

type P2PStatusReport struct {
	Topic     string `json:"topic"`
	Status    string `json:"status"`
	Network   string `json:"network"`
	Mode      string `json:"mode"`
	Side      string `json:"side"`
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
		sendRunner:      goncrunner.New(),
		receiveRunner:   goncrunner.New(),
		vpnServerRunner: goncrunner.New(),
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
	sendRunning := a.sendRunner.IsRunning()
	receiveRunning := a.receiveRunner.IsRunning()
	vpnServerRunning := a.vpnServerRunner.IsRunning()
	a.mu.Lock()
	localURL := a.receiveLocalHTTPURL
	downloading := a.downloadCancel != nil
	a.mu.Unlock()
	return AppStatus{
		Running:          sendRunning || receiveRunning || vpnServerRunning,
		SendRunning:      sendRunning,
		ReceiveRunning:   receiveRunning,
		VPNServerRunning: vpnServerRunning,
		GoncPath:         path,
		LocalHTTPURL:     localURL,
		Downloading:      downloading,
		DefaultSaveDir:   defaultSaveDir(),
	}
}

func (a *App) LocateGonc(preferred string) (string, error) {
	return findGonc(preferred)
}

func (a *App) StartTransfer(req TransferRequest) error {
	if a.ctx == nil {
		return errors.New("application is not ready")
	}
	req.Password = strings.TrimSpace(req.Password)
	if isWeakPassword(req.Password) {
		return errors.New("password is too weak; use at least 8 characters with letters and digits")
	}

	goncPath, err := findGonc(req.GoncPath)
	if err != nil {
		return err
	}

	reportURL, err := a.ensureReportServer(goncrunner.Mode(req.Mode))
	if err != nil {
		return err
	}

	mode := goncrunner.Mode(req.Mode)
	if mode == goncrunner.ModeReceive {
		a.mu.Lock()
		a.receiveLocalHTTPURL = ""
		a.mu.Unlock()
	}

	runner, err := a.runnerForMode(mode)
	if err != nil {
		return err
	}

	err = runner.Start(a.ctx, goncPath, goncrunner.Request{
		Mode:            mode,
		Password:        req.Password,
		SharePaths:      req.SharePaths,
		SaveDir:         req.SaveDir,
		DownloadSubPath: req.DownloadSubPath,
		UseUDP:          req.UseUDP,
		ReportURL:       reportURL,
		Upstream:        req.Upstream,
		DNSForward:      req.DNSForward,
		ExtraArgs:       req.ExtraArgs,
	}, func(event goncrunner.Event) {
		event.Mode = string(mode)
		if mode == goncrunner.ModeReceive && event.Type == "local_http" && event.LocalURL != "" {
			a.mu.Lock()
			a.receiveLocalHTTPURL = event.LocalURL
			a.mu.Unlock()
		}
		wailsruntime.EventsEmit(a.ctx, "gonc:event", event)
	})
	return err
}

func (a *App) StopTransfer(mode string) error {
	return a.stopTransfer(goncrunner.Mode(mode), true)
}

func (a *App) stopTransfer(mode goncrunner.Mode, requireRunning bool) error {
	runner, err := a.runnerForMode(mode)
	if err != nil {
		return err
	}
	if mode == goncrunner.ModeReceive {
		a.StopHTTPDownload()
		a.mu.Lock()
		a.receiveLocalHTTPURL = ""
		a.mu.Unlock()
	}
	stopped, err := runner.StopWait(5 * time.Second)
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

func (a *App) stopAllTransfers(requireRunning bool) error {
	var firstErr error
	for _, mode := range []goncrunner.Mode{goncrunner.ModeSend, goncrunner.ModeReceive, goncrunner.ModeVPNServer} {
		if err := a.stopTransfer(mode, requireRunning); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	a.mu.Lock()
	reportServer := a.reportServer
	a.reportServer = nil
	a.reportURL = ""
	a.mu.Unlock()
	if reportServer != nil {
		_ = reportServer.Close()
	}
	return firstErr
}

func (a *App) cleanup(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- a.stopAllTransfers(false)
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

func (a *App) StartHTTPDownload(saveDir, subPath string, includePaths []string, resume bool) error {
	if saveDir == "" {
		saveDir = defaultSaveDir()
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
		ServerURL:    localURL,
		SubPath:      subPath,
		SaveDir:      saveDir,
		IncludePaths: includePaths,
		Concurrency:  4,
		Resume:       resume,
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
	return a.receiveLocalHTTPURL
}

func (a *App) clearDownload(downloadID int64) {
	a.mu.Lock()
	if a.downloadID == downloadID {
		a.downloadCancel = nil
	}
	a.mu.Unlock()
}

func (a *App) runnerForMode(mode goncrunner.Mode) (*goncrunner.Runner, error) {
	switch mode {
	case goncrunner.ModeSend:
		return a.sendRunner, nil
	case goncrunner.ModeReceive:
		return a.receiveRunner, nil
	case goncrunner.ModeVPNServer:
		return a.vpnServerRunner, nil
	default:
		return nil, errors.New("unknown mode: " + string(mode))
	}
}

func (a *App) ensureReportServer(mode goncrunner.Mode) (string, error) {
	a.mu.Lock()
	if a.reportServer != nil && a.reportURL != "" {
		reportURL := a.reportURL
		a.mu.Unlock()
		return reportURL + "?side=" + url.QueryEscape(string(mode)), nil
	}
	a.mu.Unlock()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
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
		if reportSide := r.URL.Query().Get("side"); reportSide != "" {
			report.Side = reportSide
		}
		wailsruntime.EventsEmit(a.ctx, "p2p:report", report)
		w.WriteHeader(http.StatusNoContent)
	})

	reportURL := "http://" + ln.Addr().String() + "/p2p-report"
	a.mu.Lock()
	if a.reportServer != nil && a.reportURL != "" {
		_ = server.Close()
		_ = ln.Close()
		reportURL = a.reportURL
		a.mu.Unlock()
		return reportURL + "?side=" + url.QueryEscape(string(mode)), nil
	}
	a.reportServer = server
	a.reportURL = reportURL
	a.mu.Unlock()

	go func() {
		_ = server.Serve(ln)
	}()
	return reportURL + "?side=" + url.QueryEscape(string(mode)), nil
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

func defaultSaveDir() string {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, "Downloads", "GoncTransfer")
	}
	wd, err := os.Getwd()
	if err == nil && wd != "" {
		return filepath.Join(wd, "GoncTransfer")
	}
	return "GoncTransfer"
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
