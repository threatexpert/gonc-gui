package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"gonc-gui/internal/appupdate"
	"gonc-gui/internal/goncrunner"
	"gonc-gui/internal/httpdownload"
	"gonc-gui/internal/receivedfile"
	"gonc-gui/internal/sharecontent"
	"gonc-gui/internal/taskbar"
	"gonc-gui/internal/vpnprofile"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const updateManifestURL = "https://www.gonc.cc/gui/manifest.json"

type App struct {
	ctx context.Context

	mu                    sync.Mutex
	sendRunner            *goncrunner.Runner
	receiveRunner         *goncrunner.Runner
	vpnServerRunner       *goncrunner.Runner
	vpnClientRunner       *goncrunner.Runner
	receiveLocalHTTPURL   string
	downloadCancel        context.CancelFunc
	downloadDone          chan struct{}
	downloadID            int64
	startupSharePaths     []string
	shareContent          *sharecontent.Manager
	importNativeClipboard func() (sharecontent.ClipboardResult, error)
	clipboardGetText      func(context.Context) (string, error)
	createShareText       func(string, string) (string, error)
}

type TransferRequest struct {
	Mode            string   `json:"mode"`
	ClientRunID     int64    `json:"clientRunId,omitempty"`
	Password        string   `json:"password"`
	SharePaths      []string `json:"sharePaths"`
	SaveDir         string   `json:"saveDir"`
	DownloadSubPath string   `json:"downloadSubPath"`
	UseUDP          bool     `json:"useUDP"`
	Upstream        string   `json:"upstream"`
	DNSForward      string   `json:"dnsForward"`
	DNSServers      string   `json:"dnsServers"`
	RouteCIDRs      string   `json:"routeCidrs"`
	LinkConfig      string   `json:"linkConfig"`
	MTU             int      `json:"mtu"`
	RouteMetric     int      `json:"routeMetric"`
	BlockDNSLeak    bool     `json:"blockDnsLeak"`
	EnableIPv6      bool     `json:"enableIpv6"`
	TunnelOnly      bool     `json:"tunnelOnly"`
	ExtraArgs       string   `json:"extraArgs"`
}

type ClientP2PStatusReport struct {
	goncrunner.P2PStatusReport
	ClientRunID int64 `json:"clientRunId"`
}

func tagClientP2PReport(clientRunID int64, report goncrunner.P2PStatusReport) ClientP2PStatusReport {
	return ClientP2PStatusReport{P2PStatusReport: report, ClientRunID: clientRunID}
}

func validateTransferClientRunID(mode goncrunner.Mode, clientRunID int64) error {
	if (mode == goncrunner.ModeSend || mode == goncrunner.ModeReceive) && clientRunID <= 0 {
		return errors.New("client run ID is required for file transfer")
	}
	return nil
}

type AppStatus struct {
	Running          bool   `json:"running"`
	SendRunning      bool   `json:"sendRunning"`
	ReceiveRunning   bool   `json:"receiveRunning"`
	VPNServerRunning bool   `json:"vpnServerRunning"`
	VPNClientRunning bool   `json:"vpnClientRunning"`
	LocalHTTPURL     string `json:"localHTTPUrl"`
	Downloading      bool   `json:"downloading"`
	DefaultSaveDir   string `json:"defaultSaveDir"`
}

type RemoteListResponse struct {
	ServerURL string                  `json:"serverUrl"`
	Files     []httpdownload.FileInfo `json:"files"`
	FileCount int                     `json:"fileCount"`
	DirCount  int                     `json:"dirCount"`
	TotalSize int64                   `json:"totalSize"`
}

type ReceivedFileState struct {
	RemotePath string `json:"remotePath"`
	Available  bool   `json:"available"`
}

type ReceivedFileActionResult struct {
	Unavailable bool   `json:"unavailable"`
	Error       string `json:"error"`
}

type VPNProfile = vpnprofile.Profile
type VPNProfileStore = vpnprofile.Store

func NewApp(startupSharePaths []string) *App {
	shareContent := sharecontent.NewManager()
	return &App{
		sendRunner:            goncrunner.New(),
		receiveRunner:         goncrunner.New(),
		vpnServerRunner:       goncrunner.New(),
		vpnClientRunner:       goncrunner.New(),
		startupSharePaths:     append([]string(nil), startupSharePaths...),
		shareContent:          shareContent,
		importNativeClipboard: shareContent.ImportNativeClipboard,
		clipboardGetText:      wailsruntime.ClipboardGetText,
		createShareText:       shareContent.CreateText,
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

func (a *App) StartupSharePaths() []string {
	return append([]string(nil), a.startupSharePaths...)
}

func (a *App) CreateTextShare(text string) (string, error) {
	if text == "" {
		return "", errors.New("text content is empty")
	}
	return a.createShareText("text", text)
}

func (a *App) ImportClipboard() (sharecontent.ClipboardResult, error) {
	result, err := a.importNativeClipboard()
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, sharecontent.ErrClipboardUnsupported) {
		return sharecontent.ClipboardResult{}, err
	}
	text, err := a.clipboardGetText(a.ctx)
	if err != nil {
		return sharecontent.ClipboardResult{}, fmt.Errorf("%w: read clipboard text: %w", sharecontent.ErrClipboardAccess, err)
	}
	if text == "" {
		return sharecontent.ClipboardResult{}, sharecontent.ErrClipboardUnsupported
	}
	path, err := a.createShareText("clipboard-text", text)
	if err != nil {
		return sharecontent.ClipboardResult{}, fmt.Errorf("%w: create clipboard text: %w", sharecontent.ErrClipboardTemporaryFile, err)
	}
	return sharecontent.ClipboardResult{Paths: []string{path}, Kind: sharecontent.ClipboardText}, nil
}

func (a *App) ReleaseGeneratedSharePaths(paths []string) error {
	return a.shareContent.Release(paths)
}

func (a *App) CheckForUpdate(currentVersion string) (appupdate.Result, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return appupdate.Check(ctx, client, updateManifestURL, currentVersion, runtime.GOOS, runtime.GOARCH)
}

func (a *App) OpenSaveDir(saveDir string) (string, error) {
	if strings.TrimSpace(saveDir) == "" {
		saveDir = defaultSaveDir()
	}
	dir, err := filepath.Abs(saveDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	return dir, nil
}

func (a *App) CheckReceivedFiles(saveDir string, files []httpdownload.FileInfo) []ReceivedFileState {
	checked := receivedfile.Check(saveDir, files)
	out := make([]ReceivedFileState, len(checked))
	for i, state := range checked {
		out[i] = ReceivedFileState(state)
	}
	return out
}

func (a *App) RevealReceivedFile(saveDir string, file httpdownload.FileInfo) ReceivedFileActionResult {
	return classifyRevealError(receivedfile.Reveal(saveDir, file))
}

func classifyRevealError(err error) ReceivedFileActionResult {
	if err == nil {
		return ReceivedFileActionResult{}
	}
	return ReceivedFileActionResult{
		Unavailable: errors.Is(err, receivedfile.ErrUnavailable),
		Error:       err.Error(),
	}
}

func (a *App) GeneratePassword() (string, error) {
	return generateSecureRandomString(24)
}

func (a *App) IsAdministrator() bool {
	return isAdministrator()
}

func (a *App) LoadVPNProfiles() (VPNProfileStore, error) {
	return vpnprofile.Load()
}

func (a *App) SaveVPNProfiles(store VPNProfileStore) error {
	return vpnprofile.Save(store)
}

func (a *App) SetTaskbarProgress(doneBytes, totalBytes int64) {
	if doneBytes <= 0 || totalBytes <= 0 || doneBytes >= totalBytes {
		taskbar.Clear()
		return
	}
	taskbar.SetProgress(uint64(doneBytes), uint64(totalBytes))
}

func (a *App) ClearTaskbarProgress() {
	taskbar.Clear()
}

func (a *App) Status() AppStatus {
	sendRunning := a.sendRunner.IsRunning()
	receiveRunning := a.receiveRunner.IsRunning()
	vpnServerRunning := a.vpnServerRunner.IsRunning()
	vpnClientRunning := a.vpnClientRunner.IsRunning()
	a.mu.Lock()
	localURL := a.receiveLocalHTTPURL
	downloading := a.downloadCancel != nil
	a.mu.Unlock()
	return AppStatus{
		Running:          sendRunning || receiveRunning || vpnServerRunning || vpnClientRunning,
		SendRunning:      sendRunning,
		ReceiveRunning:   receiveRunning,
		VPNServerRunning: vpnServerRunning,
		VPNClientRunning: vpnClientRunning,
		LocalHTTPURL:     localURL,
		Downloading:      downloading,
		DefaultSaveDir:   defaultSaveDir(),
	}
}

func (a *App) StartTransfer(req TransferRequest) error {
	if a.ctx == nil {
		return errors.New("application is not ready")
	}
	req.Password = strings.TrimSpace(req.Password)
	if isWeakPassword(req.Password) {
		return errors.New("password is too weak; use at least 8 characters with letters and digits")
	}

	mode := goncrunner.Mode(req.Mode)
	if err := validateTransferClientRunID(mode, req.ClientRunID); err != nil {
		return err
	}
	if mode == goncrunner.ModeReceive {
		a.mu.Lock()
		a.receiveLocalHTTPURL = ""
		a.mu.Unlock()
	}

	runner, err := a.runnerForMode(mode)
	if err != nil {
		return err
	}

	err = runner.Start(a.ctx, goncrunner.Request{
		Mode:            mode,
		Password:        req.Password,
		SharePaths:      req.SharePaths,
		SaveDir:         req.SaveDir,
		DownloadSubPath: req.DownloadSubPath,
		UseUDP:          req.UseUDP,
		Upstream:        req.Upstream,
		DNSForward:      req.DNSForward,
		DNSServers:      req.DNSServers,
		RouteCIDRs:      req.RouteCIDRs,
		LinkConfig:      req.LinkConfig,
		MTU:             req.MTU,
		RouteMetric:     req.RouteMetric,
		BlockDNSLeak:    req.BlockDNSLeak,
		EnableIPv6:      req.EnableIPv6,
		TunnelOnly:      req.TunnelOnly,
		ExtraArgs:       req.ExtraArgs,
	}, func(event goncrunner.Event) {
		event.Mode = string(mode)
		if mode == goncrunner.ModeReceive && event.Type == "local_http" && event.LocalURL != "" {
			a.mu.Lock()
			a.receiveLocalHTTPURL = event.LocalURL
			a.mu.Unlock()
		}
		wailsruntime.EventsEmit(a.ctx, "gonc:event", event)
	}, func(report goncrunner.P2PStatusReport) {
		wailsruntime.EventsEmit(a.ctx, "p2p:report", tagClientP2PReport(req.ClientRunID, report))
	})
	return err
}

func (a *App) StopTransfer(mode string) error {
	return a.stopTransfer(goncrunner.Mode(mode), true)
}

func (a *App) UpdateSharePaths(paths []string) error {
	return a.sendRunner.UpdateSharePaths(paths)
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
		return errors.New("gonc embedded session did not exit within 5 seconds")
	}
	return nil
}

func (a *App) stopAllTransfers(requireRunning bool) error {
	var firstErr error
	for _, mode := range []goncrunner.Mode{goncrunner.ModeSend, goncrunner.ModeReceive, goncrunner.ModeVPNServer, goncrunner.ModeVPNClient} {
		if err := a.stopTransfer(mode, requireRunning); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *App) cleanup(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- cleanupTransfersThenContent(
			func() error { return a.stopAllTransfers(false) },
			a.shareContent.Cleanup,
		)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(6 * time.Second):
		return errors.New("timed out waiting for gonc embedded session cleanup")
	}
}

func cleanupTransfersThenContent(stopTransfers, cleanupContent func() error) error {
	transferErr := stopTransfers()
	contentErr := cleanupContent()
	return errors.Join(transferErr, contentErr)
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

func clientTaskDownloadSink(clientTaskID int64, sink httpdownload.Sink) httpdownload.Sink {
	return func(event httpdownload.Event) {
		event.ClientTaskID = clientTaskID
		sink(event)
	}
}

func (a *App) StartHTTPDownload(saveDir, subPath string, includePaths []string, excludePaths []string, cachedFiles []httpdownload.FileInfo, resume bool, clientTaskID int64) error {
	if clientTaskID <= 0 {
		return errors.New("client task ID is required")
	}
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
	done := make(chan struct{})
	a.downloadCancel = cancel
	a.downloadDone = done
	a.downloadID++
	downloadID := a.downloadID
	a.mu.Unlock()

	d, err := httpdownload.New(httpdownload.Config{
		ServerURL:    localURL,
		SubPath:      subPath,
		SaveDir:      saveDir,
		IncludePaths: includePaths,
		ExcludePaths: excludePaths,
		CachedFiles:  cachedFiles,
		Concurrency:  4,
		Resume:       resume,
	})
	if err != nil {
		a.clearDownload(downloadID, done)
		return err
	}

	go func() {
		emit := clientTaskDownloadSink(clientTaskID, func(event httpdownload.Event) {
			wailsruntime.EventsEmit(a.ctx, "download:event", event)
		})
		err := d.Start(ctx, emit)
		if err != nil && !errors.Is(err, context.Canceled) {
			emit(httpdownload.Event{
				Type:    "status",
				Level:   "error",
				Message: err.Error(),
				Time:    time.Now().Format(time.RFC3339),
			})
		}
		a.clearDownload(downloadID, done)
	}()
	return nil
}

func (a *App) StopHTTPDownload() error {
	a.mu.Lock()
	cancel := a.downloadCancel
	done := a.downloadDone
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

func (a *App) getLocalHTTPURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.receiveLocalHTTPURL
}

func (a *App) clearDownload(downloadID int64, done chan struct{}) {
	a.mu.Lock()
	if a.downloadID == downloadID && a.downloadDone == done {
		a.downloadCancel = nil
		a.downloadDone = nil
	}
	a.mu.Unlock()
	close(done)
}

func (a *App) runnerForMode(mode goncrunner.Mode) (*goncrunner.Runner, error) {
	switch mode {
	case goncrunner.ModeSend:
		return a.sendRunner, nil
	case goncrunner.ModeReceive:
		return a.receiveRunner, nil
	case goncrunner.ModeVPNServer:
		return a.vpnServerRunner, nil
	case goncrunner.ModeVPNClient:
		return a.vpnClientRunner, nil
	default:
		return nil, errors.New("unknown mode: " + string(mode))
	}
}

func defaultSaveDir() string {
	return defaultSaveDirFrom(platformDownloadsDir, os.UserHomeDir, os.Getwd)
}

func defaultSaveDirFrom(
	downloadsDir func() (string, error),
	homeDir func() (string, error),
	workingDir func() (string, error),
) string {
	downloads, err := downloadsDir()
	if err == nil && strings.TrimSpace(downloads) != "" {
		return filepath.Join(downloads, "GoncTransfer")
	}
	home, err := homeDir()
	if err == nil && home != "" {
		return filepath.Join(home, "Downloads", "GoncTransfer")
	}
	wd, err := workingDir()
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
