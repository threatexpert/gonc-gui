// Package nearbyapp connects the gonc nearby protocol to the desktop
// application: it owns the saved identity and pairings, runs Nearby sends and
// receives as their own sessions, and reports state to the UI through events.
package nearbyapp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gonc-gui/internal/httpdownload"
	"gonc-gui/internal/nearby"
	"gonc-gui/internal/nearbystore"

	"github.com/threatexpert/gonc/v2/goncembed"
	"github.com/threatexpert/gonc/v2/httpfileshare"
)

const (
	EventState    = "nearby:state"
	EventPairing  = "nearby:pairing"
	EventTransfer = "nearby:transfer"
	EventLog      = "nearby:log"

	preferredPort   = 19732
	offerTimeout    = 2 * time.Minute
	readyTimeout    = 20 * time.Second
	openTimeout     = 15 * time.Second
	finishedTimeout = 24 * time.Hour
	// directReceiveWait is how long a receive tries the sender's direct
	// listener before switching to the LAN path.
	directReceiveWait = 3 * time.Second
	lanReadyTimeout   = 45 * time.Second
	maxLogLines       = 400
)

// Emitter delivers an event to the UI.
type Emitter func(name string, data any)

type PeerInfo struct {
	PeerID     string    `json:"peerId"`
	Nickname   string    `json:"nickname"`
	DeviceType string    `json:"deviceType"`
	PairedAt   time.Time `json:"pairedAt"`
	// LastSeen is when the device was last nearby; zero if never since
	// pairing.
	LastSeen time.Time `json:"lastSeen"`
}

// peerSeenSaveEvery limits how often a paired device's last sighting is
// written to the store while it stays nearby. The time it leaves is always
// written.
const peerSeenSaveEvery = time.Hour

type State struct {
	Running bool `json:"running"`
	// Discovering is true while the device list is being kept current,
	// that is while the Nearby page is open and discovery works.
	Discovering bool `json:"discovering"`
	// DeviceFadeMs and DeviceExpiryMs are nearby.DeviceFadeAfter and
	// nearby.DeviceExpiry, so apps can show how close a device is to
	// leaving the list.
	DeviceFadeMs     int64           `json:"deviceFadeMs"`
	DeviceExpiryMs   int64           `json:"deviceExpiryMs"`
	Error            string          `json:"error,omitempty"`
	DiscoveryError   string          `json:"discoveryError,omitempty"`
	Nickname         string          `json:"nickname"`
	Visible          bool            `json:"visible"`
	QuickReceive     bool            `json:"quickReceive"`
	SaveDir          string          `json:"saveDir"`
	StorageProtected bool            `json:"storageProtected"`
	Port             int             `json:"port"`
	Addresses        []string        `json:"addresses"`
	Devices          []nearby.Device `json:"devices"`
	Peers            []PeerInfo      `json:"peers"`
}

type PairingInfo struct {
	ID       string `json:"id"`
	Incoming bool   `json:"incoming"`
	PeerName string `json:"peerName"`
	PeerType string `json:"peerType"`
	Code     string `json:"code"`
	DeviceID string `json:"deviceId,omitempty"`
	// Status is waiting, paired, rejected or failed.
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	PeerID string `json:"peerId,omitempty"`
}

type FileSummary struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

type TransferInfo struct {
	ID        string `json:"id"`
	Direction string `json:"direction"` // send or receive
	DeviceID  string `json:"deviceId,omitempty"`
	PeerName  string `json:"peerName"`
	// Status: connecting, waiting, offered, transferring, completed,
	// declined, cancelled or failed.
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	// ErrorCode names a failure apps word themselves: not-paired,
	// quick-refused, busy or timeout. Message is the English text.
	ErrorCode      string        `json:"errorCode,omitempty"`
	Items          []FileSummary `json:"items,omitempty"`
	FileCount      int64         `json:"fileCount"`
	TotalBytes     int64         `json:"totalBytes"`
	DoneBytes      int64         `json:"doneBytes"`
	DoneFiles      int64         `json:"doneFiles"`
	BytesPerSecond int64         `json:"bytesPerSecond"`
	SaveDir        string        `json:"saveDir,omitempty"`
	SavedPaths     []string      `json:"savedPaths,omitempty"`
	// Quick is true for transfers between unpaired devices. Nothing proves
	// who the other device is.
	Quick bool `json:"quick,omitempty"`
	// Endpoint is set while an ExternalDownload transfer is "ready".
	Endpoint  string    `json:"endpoint,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (t TransferInfo) finished() bool {
	switch t.Status {
	case "completed", "declined", "cancelled", "failed":
		return true
	}
	return false
}

type Manager struct {
	emit           Emitter
	defaultSaveDir func() string
	// StorePath overrides the settings file location, for tests.
	StorePath string
	// StoreKey, DefaultName and DeviceType configure the store for apps
	// that keep the key themselves, such as the Android app. Set them
	// before Start.
	StoreKey    []byte
	DefaultName string
	DeviceType  string
	// ExternalDownload hands an accepted transfer's download to the app:
	// the transfer reports status "ready" with Endpoint, the local HTTP
	// address of the sender's share, and waits for FinishReceive. Without
	// it the manager saves the files itself.
	ExternalDownload bool

	mu        sync.Mutex
	store     nearbystore.Store
	svc       *nearby.Service
	startErr  error
	pairings  map[string]*nearby.Pairing
	transfers map[string]*TransferInfo
	cancels   map[string]context.CancelFunc
	incoming  map[string]*incomingTransfer

	// pairingWith holds devices with a pairing being set up, so repeated
	// clicks do not start parallel attempts.
	pairingWith map[string]bool

	// discovering is whether the Nearby page is open. Discovery traffic is
	// sent only then; see SetDiscovering.
	discovering bool

	// nearbyPeers is when each paired device now in the list was last
	// heard from, by installation ID; see notePeersSeen.
	nearbyPeers map[string]time.Time

	// finishers wait for FinishReceive in ExternalDownload mode.
	finishers map[string]chan receiveResult

	logMu sync.Mutex
	logs  []LogLine
}

// LogLine is one Nearby entry for the app's activity log.
type LogLine struct {
	Type    string `json:"type"`
	Mode    string `json:"mode"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Time    string `json:"time"`
}

// logf records a line for the activity log.
func (m *Manager) logf(format string, args ...any) {
	msg := strings.TrimPrefix(strings.TrimSpace(fmt.Sprintf(format, args...)), "nearby: ")
	line := LogLine{Type: "nearby", Mode: "nearby", Level: logLevel(msg), Message: "Nearby: " + msg, Time: time.Now().Format(time.RFC3339Nano)}
	m.logMu.Lock()
	m.logs = append(m.logs, line)
	if len(m.logs) > maxLogLines {
		m.logs = append([]LogLine(nil), m.logs[len(m.logs)-maxLogLines:]...)
	}
	m.logMu.Unlock()
	m.emit(EventLog, line)
}

func logLevel(msg string) string {
	lower := strings.ToLower(msg)
	for _, word := range []string{"failed", "error", "timeout", "timed out", "could not", "did not", "ignored", "unavailable"} {
		if strings.Contains(lower, word) {
			return "warn"
		}
	}
	return "info"
}

// Logs returns the recent Nearby log lines.
func (m *Manager) Logs() []LogLine {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	return append([]LogLine(nil), m.logs...)
}

// logWriter feeds the nearby service's logger into the connection log.
type logWriter struct{ m *Manager }

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			w.m.logf("%s", line)
		}
	}
	return len(p), nil
}

type incomingTransfer struct {
	t       *nearby.IncomingTransfer
	decided chan bool
}

// receiveResult is the app's report on an externally downloaded transfer.
type receiveResult struct {
	ok      bool
	message string
}

func New(emit Emitter, defaultSaveDir func() string) *Manager {
	if emit == nil {
		emit = func(string, any) {}
	}
	return &Manager{
		emit:           emit,
		defaultSaveDir: defaultSaveDir,
		pairings:       map[string]*nearby.Pairing{},
		transfers:      map[string]*TransferInfo{},
		cancels:        map[string]context.CancelFunc{},
		incoming:       map[string]*incomingTransfer{},
		pairingWith:    map[string]bool{},
		finishers:      map[string]chan receiveResult{},
	}
}

func (m *Manager) storeOptions() nearbystore.Options {
	return nearbystore.Options{Key: m.StoreKey, DefaultName: m.DefaultName}
}

// Start loads the identity and starts the service. Errors are kept in State
// so the UI can explain them; the rest of the application keeps working.
func (m *Manager) Start() error {
	if m.StorePath == "" {
		path, err := nearbystore.FilePath()
		if err != nil {
			m.setStartErr(err)
			return err
		}
		m.StorePath = path
	}
	store, err := nearbystore.LoadWith(m.StorePath, m.storeOptions())
	if err != nil {
		m.setStartErr(fmt.Errorf("could not load Nearby settings: %w", err))
		return err
	}
	identity := store.Identity()
	if m.DeviceType != "" {
		identity.DeviceType = m.DeviceType
	}
	cfg := nearby.Config{
		Identity: identity,
		Peers:    store.Peers,
		Visible:  store.Visible,
		// Unpaired devices may offer files unless the user turned it off.
		QuickReceive: !store.RefuseQuick,
		Paused:       true, // until the Nearby page is open
		Handler:      handler{m},
		Logger:       log.New(logWriter{m}, "", 0),
		ListenAddr:   ":" + strconv.Itoa(preferredPort),
	}
	svc, err := nearby.Start(cfg)
	if err != nil {
		// Another copy of the app may hold the preferred port.
		cfg.ListenAddr = ":0"
		svc, err = nearby.Start(cfg)
	}
	if err != nil {
		m.setStartErr(fmt.Errorf("could not start Nearby: %w", err))
		return err
	}
	m.mu.Lock()
	m.store, m.svc, m.startErr = store, svc, nil
	if m.discovering {
		svc.SetDiscovering(true)
	}
	m.mu.Unlock()
	m.emitState()
	// Reconnect to devices the user added by address in earlier sessions.
	for _, address := range store.Addresses {
		go func(address string) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_, _ = svc.AddAddress(ctx, address)
		}(address)
	}
	return nil
}

func (m *Manager) setStartErr(err error) {
	m.mu.Lock()
	m.startErr = err
	m.mu.Unlock()
	m.emitState()
}

func (m *Manager) Close() {
	m.mu.Lock()
	running := m.svc
	m.mu.Unlock()
	if running != nil {
		m.notePeersSeen(running.Devices(), true)
	}
	m.mu.Lock()
	svc := m.svc
	m.svc = nil
	cancels := make([]context.CancelFunc, 0, len(m.cancels))
	for _, c := range m.cancels {
		cancels = append(cancels, c)
	}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	if svc != nil {
		_ = svc.Close()
	}
}

func (m *Manager) service() (*nearby.Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.svc == nil {
		if m.startErr != nil {
			return nil, m.startErr
		}
		return nil, errors.New("Nearby is not running")
	}
	return m.svc, nil
}

func (m *Manager) saveDirLocked() string {
	if m.store.SaveDir != "" {
		return m.store.SaveDir
	}
	if m.defaultSaveDir != nil {
		return m.defaultSaveDir()
	}
	return "."
}

// State returns everything the Nearby page shows.
func (m *Manager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := State{
		Running:          m.svc != nil,
		Nickname:         m.store.Nickname,
		Visible:          m.store.Visible,
		QuickReceive:     !m.store.RefuseQuick,
		SaveDir:          m.saveDirLocked(),
		StorageProtected: nearbystore.Protected,
		DeviceFadeMs:     nearby.DeviceFadeAfter.Milliseconds(),
		DeviceExpiryMs:   nearby.DeviceExpiry.Milliseconds(),
		Devices:          []nearby.Device{},
		Peers:            []PeerInfo{},
	}
	if m.startErr != nil {
		st.Error = m.startErr.Error()
	}
	if m.svc != nil {
		st.Devices = m.svc.Devices()
		st.Port = m.svc.Port()
		st.Addresses = localAddresses(st.Port)
		if err := m.svc.DiscoveryError(); err != nil {
			st.DiscoveryError = err.Error()
		}
		st.Discovering = m.discovering && st.DiscoveryError == ""
	}
	for _, p := range m.store.Peers {
		st.Peers = append(st.Peers, PeerInfo{p.InstallationID, p.Nickname, p.DeviceType, p.PairedAt, m.store.PeerLastSeen[p.InstallationID]})
	}
	return st
}

func (m *Manager) emitState() { m.emit(EventState, m.State()) }

// Transfers returns active and recent transfers, newest first.
func (m *Manager) Transfers() []TransferInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TransferInfo, 0, len(m.transfers))
	for _, t := range m.transfers {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func localAddresses(port int) []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, net.JoinHostPort(ipn.IP.String(), strconv.Itoa(port)))
		}
	}
	sort.Strings(out)
	return out
}

func (m *Manager) SetNickname(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("enter a device name")
	}
	return m.updateStore(func(s *nearbystore.Store) { s.Nickname = name }, func(svc *nearby.Service) { svc.SetNickname(name) })
}

func (m *Manager) SetVisible(visible bool) error {
	return m.updateStore(func(s *nearbystore.Store) { s.Visible = visible }, func(svc *nearby.Service) { svc.SetVisible(visible) })
}

// SetQuickReceive sets whether unpaired devices may offer files.
func (m *Manager) SetQuickReceive(on bool) error {
	return m.updateStore(func(s *nearbystore.Store) { s.RefuseQuick = !on }, func(svc *nearby.Service) { svc.SetQuickReceive(on) })
}

func (m *Manager) SetSaveDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		dir = abs
	}
	return m.updateStore(func(s *nearbystore.Store) { s.SaveDir = dir }, nil)
}

func (m *Manager) updateStore(change func(*nearbystore.Store), apply func(*nearby.Service)) error {
	m.mu.Lock()
	if m.svc == nil {
		m.mu.Unlock()
		_, err := m.service()
		return err
	}
	next := m.store
	next.Peers = append([]nearby.PeerRecord(nil), m.store.Peers...)
	next.Addresses = append([]string(nil), m.store.Addresses...)
	change(&next)
	if err := nearbystore.SaveWith(m.StorePath, m.storeOptions(), next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.store = next
	svc := m.svc
	m.mu.Unlock()
	if apply != nil {
		apply(svc)
	}
	m.emitState()
	return nil
}

// SetDiscovering sends discovery traffic only while the Nearby page is open.
// While it is off, Nearby stays quiet on the network but still accepts files
// from devices that reach it and finishes transfers already running.
func (m *Manager) SetDiscovering(on bool) {
	m.mu.Lock()
	changed := m.discovering != on
	m.discovering = on
	if m.svc != nil {
		m.svc.SetDiscovering(on)
	}
	m.mu.Unlock()
	if changed {
		m.emitState()
	}
}

func (m *Manager) Refresh() error {
	svc, err := m.service()
	if err != nil {
		return err
	}
	svc.Refresh()
	return nil
}

// AddAddress adds a device by host:port for networks without multicast.
func (m *Manager) AddAddress(address string) (nearby.Device, error) {
	svc, err := m.service()
	if err != nil {
		return nearby.Device{}, err
	}
	address = strings.TrimSpace(address)
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, strconv.Itoa(preferredPort))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	dev, err := svc.AddAddress(ctx, address)
	if err != nil {
		return nearby.Device{}, fmt.Errorf("no Gonc device answered at %s: %w", address, err)
	}
	_ = m.updateStore(func(s *nearbystore.Store) {
		for _, existing := range s.Addresses {
			if existing == address {
				return
			}
		}
		s.Addresses = append(s.Addresses, address)
	}, nil)
	return dev, nil
}

// Forget removes a saved pairing. The other device keeps its record until
// its user forgets this device too.
func (m *Manager) Forget(peerID string) error {
	m.mu.Lock()
	if m.svc == nil {
		m.mu.Unlock()
		_, err := m.service()
		return err
	}
	next := m.store
	next.Peers = append([]nearby.PeerRecord(nil), m.store.Peers...)
	if !next.RemovePeer(peerID) {
		m.mu.Unlock()
		return errors.New("that device is not paired")
	}
	if err := nearbystore.SaveWith(m.StorePath, m.storeOptions(), next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.store = next
	svc, peers := m.svc, next.Peers
	m.mu.Unlock()
	svc.SetPeers(peers)
	m.emitState()
	return nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- pairing ----

// Pair starts pairing with an unpaired device and returns the code to show.
func (m *Manager) Pair(deviceID string) (PairingInfo, error) {
	svc, err := m.service()
	if err != nil {
		return PairingInfo{}, err
	}
	m.mu.Lock()
	busy := m.pairingWith[deviceID]
	m.pairingWith[deviceID] = true
	m.mu.Unlock()
	if busy {
		return PairingInfo{}, errors.New("already connecting to this device")
	}
	defer func() {
		m.mu.Lock()
		delete(m.pairingWith, deviceID)
		m.mu.Unlock()
	}()
	p, err := svc.Pair(deviceID)
	if err != nil {
		return PairingInfo{}, friendlyError(err)
	}
	info := m.trackPairing(p, deviceID)
	return info, nil
}

func (m *Manager) trackPairing(p *nearby.Pairing, deviceID string) PairingInfo {
	info := PairingInfo{ID: p.ID, Incoming: p.Incoming, PeerName: p.PeerName, PeerType: p.PeerType, Code: p.Code, DeviceID: deviceID, Status: "waiting"}
	m.mu.Lock()
	m.pairings[p.ID] = p
	m.mu.Unlock()
	m.emit(EventPairing, info)
	go func() {
		<-p.Done()
		rec, err := p.Result()
		m.mu.Lock()
		delete(m.pairings, p.ID)
		m.mu.Unlock()
		switch {
		case err == nil:
			if serr := m.savePeer(*rec); serr != nil {
				info.Status, info.Error = "failed", "Paired, but saving failed: "+serr.Error()
			} else {
				info.Status, info.PeerID = "paired", rec.InstallationID
			}
		case errors.Is(err, nearby.ErrRejected):
			info.Status = "rejected"
		default:
			info.Status, info.Error = "failed", friendlyError(err).Error()
		}
		m.emit(EventPairing, info)
		m.emitState()
	}()
	return info
}

func (m *Manager) savePeer(rec nearby.PeerRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.store
	next.Peers = append([]nearby.PeerRecord(nil), m.store.Peers...)
	next.UpsertPeer(rec)
	if err := nearbystore.SaveWith(m.StorePath, m.storeOptions(), next); err != nil {
		return err
	}
	m.store = next
	return nil
}

// ConfirmPairing passes the user's decision on whether the codes match.
func (m *Manager) ConfirmPairing(id string, accept bool) error {
	m.mu.Lock()
	p, ok := m.pairings[id]
	m.mu.Unlock()
	if !ok {
		return errors.New("that pairing request has ended")
	}
	p.Confirm(accept)
	return nil
}

// errorCode is TransferInfo.ErrorCode for err, or "".
func errorCode(err error) string {
	switch {
	case errors.Is(err, nearby.ErrNotPaired):
		return "not-paired"
	case errors.Is(err, nearby.ErrQuickRefused):
		return "quick-refused"
	case errors.Is(err, nearby.ErrBusy):
		return "busy"
	case errors.Is(err, nearby.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	return ""
}

func friendlyError(err error) error {
	switch {
	case errors.Is(err, nearby.ErrQuickRefused):
		return errors.New("that device only accepts files from paired devices; pair with it first")
	case errors.Is(err, nearby.ErrBusy):
		return errors.New("the other device is busy; try again in a moment")
	case errors.Is(err, nearby.ErrNotPaired):
		return errors.New("the other device no longer recognizes this pairing; forget it and pair again")
	case errors.Is(err, nearby.ErrVerification):
		return errors.New("the device could not be verified")
	case errors.Is(err, nearby.ErrTimeout):
		return errors.New("timed out waiting for the other device")
	case errors.Is(err, nearby.ErrUnknown):
		return errors.New("that device is no longer nearby")
	case errors.Is(err, context.DeadlineExceeded):
		return errors.New("timed out waiting for the other device")
	}
	return err
}

// ---- transfer bookkeeping ----

func (m *Manager) putTransfer(t TransferInfo) {
	t.UpdatedAt = time.Now()
	m.mu.Lock()
	// A transfer that ended stays ended: a late progress report must not
	// bring a cancelled transfer back.
	if cur, ok := m.transfers[t.ID]; ok && cur.finished() && !t.finished() {
		m.mu.Unlock()
		return
	}
	cp := t
	m.transfers[t.ID] = &cp
	// Keep the list short: drop the oldest finished entries.
	if len(m.transfers) > 30 {
		var oldest *TransferInfo
		for _, x := range m.transfers {
			if x.finished() && (oldest == nil || x.UpdatedAt.Before(oldest.UpdatedAt)) {
				oldest = x
			}
		}
		if oldest != nil {
			delete(m.transfers, oldest.ID)
		}
	}
	m.mu.Unlock()
	m.emit(EventTransfer, t)
}

func (m *Manager) setCancel(id string, cancel context.CancelFunc) {
	m.mu.Lock()
	if cancel == nil {
		delete(m.cancels, id)
	} else {
		m.cancels[id] = cancel
	}
	m.mu.Unlock()
}

// CancelTransfer stops a running send or receive.
func (m *Manager) CancelTransfer(id string) error {
	m.mu.Lock()
	cancel := m.cancels[id]
	in := m.incoming[id]
	m.mu.Unlock()
	if in != nil {
		select {
		case in.decided <- false:
			return nil
		default:
		}
	}
	if cancel == nil {
		return errors.New("that transfer is not running")
	}
	cancel()
	// Show the result at once; closing connections and removing a partly
	// received file finish in the background.
	m.mu.Lock()
	cur := m.transfers[id]
	var snap TransferInfo
	show := cur != nil && !cur.finished()
	if show {
		snap = *cur
		snap.Status, snap.Message, snap.BytesPerSecond = "cancelled", "", 0
	}
	m.mu.Unlock()
	if show {
		m.putTransfer(snap)
	}
	return nil
}

// ClearFinished removes finished transfers from the list.
func (m *Manager) ClearFinished() {
	m.mu.Lock()
	for id, t := range m.transfers {
		if t.finished() {
			delete(m.transfers, id)
		}
	}
	m.mu.Unlock()
}

// ---- sending ----

func summarizeLocal(paths []string) ([]FileSummary, int64, int64) {
	var items []FileSummary
	var count, total int64
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		item := FileSummary{Name: filepath.Base(p), IsDir: info.IsDir()}
		if !info.IsDir() {
			item.Size = info.Size()
			count++
			total += info.Size()
		} else {
			_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if fi, err := d.Info(); err == nil && fi.Mode().IsRegular() {
					count++
					item.Size += fi.Size()
					total += fi.Size()
				}
				return nil
			})
		}
		items = append(items, item)
	}
	return items, count, total
}

// Send offers files to a paired device and runs the transfer in the
// background. Progress arrives as EventTransfer events.
func (m *Manager) Send(deviceID string, paths []string) (string, error) {
	return m.send(deviceID, paths, false)
}

// QuickSend offers files to a device without pairing. The recipient still
// accepts or declines, and is told the sender is unverified.
func (m *Manager) QuickSend(deviceID string, paths []string) (string, error) {
	return m.send(deviceID, paths, true)
}

func (m *Manager) send(deviceID string, paths []string, quick bool) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("add files or folders to send")
	}
	source, err := httpfileshare.NewOSFileSource(paths)
	if err != nil {
		return "", err
	}
	items, count, total := summarizeLocal(paths)
	return m.sendSource(deviceID, source, items, count, total, quick)
}

// SendSource offers the files of any FileSource, such as an app's own
// document provider, to a device. Set quick for an unpaired device.
func (m *Manager) SendSource(deviceID string, source httpfileshare.FileSource, quick bool) (string, error) {
	if source == nil {
		return "", errors.New("add files to send")
	}
	items, count, total := summarizeSource(source)
	if len(items) == 0 {
		return "", errors.New("add files to send")
	}
	return m.sendSource(deviceID, source, items, count, total, quick)
}

// summarizeSource lists a source's top-level items with their total sizes.
func summarizeSource(source httpfileshare.FileSource) ([]FileSummary, int64, int64) {
	entries, err := source.ReadDir("/")
	if err != nil {
		return nil, 0, 0
	}
	var items []FileSummary
	var count, total int64
	for _, e := range entries {
		item := FileSummary{Name: e.Name(), IsDir: e.IsDir()}
		if !e.IsDir() {
			item.Size = e.Size()
			count++
		} else {
			_ = source.Walk("/"+e.Name(), func(_ string, info fs.FileInfo, err error) error {
				if err == nil && info != nil && !info.IsDir() {
					count++
					item.Size += info.Size()
				}
				return nil
			})
		}
		total += item.Size
		items = append(items, item)
	}
	return items, count, total
}

func (m *Manager) sendSource(deviceID string, source httpfileshare.FileSource, items []FileSummary, count, total int64, quick bool) (string, error) {
	svc, err := m.service()
	if err != nil {
		return "", err
	}
	peerName := deviceID
	for _, d := range svc.Devices() {
		if d.ID == deviceID {
			peerName = d.Nickname
			if !d.Paired && !quick {
				return "", errors.New("pair with this device first")
			}
		}
	}
	info := TransferInfo{ID: newID(), Direction: "send", DeviceID: deviceID, PeerName: peerName, Status: "connecting", Items: items, FileCount: count, TotalBytes: total, Quick: quick}
	ctx, cancel := context.WithCancel(context.Background())
	m.setCancel(info.ID, cancel)
	m.putTransfer(info)
	go m.runSend(ctx, cancel, svc, source, info)
	return info.ID, nil
}

func (m *Manager) runSend(ctx context.Context, cancel context.CancelFunc, svc *nearby.Service, source httpfileshare.FileSource, info TransferInfo) {
	defer cancel()
	defer m.setCancel(info.ID, nil)
	fail := func(status string, err error) {
		info.Status = status
		if err != nil {
			info.Message, info.ErrorCode = friendlyError(err).Error(), errorCode(err)
		}
		if ctx.Err() != nil && status == "failed" {
			info.Status, info.Message, info.ErrorCode = "cancelled", "", ""
		}
		m.putTransfer(info)
	}

	openCtx, openCancel := context.WithTimeout(ctx, openTimeout)
	var out *nearby.OutgoingTransfer
	var err error
	if info.Quick {
		out, err = svc.OpenQuickTransfer(openCtx, info.DeviceID)
	} else {
		out, err = svc.OpenTransfer(openCtx, info.DeviceID)
	}
	openCancel()
	if err != nil {
		fail("failed", err)
		return
	}
	defer out.Close()
	stopOut := context.AfterFunc(ctx, out.Cancel)
	defer stopOut()

	port, err := freePort(out.LocalIP)
	if err != nil {
		fail("failed", err)
		return
	}
	progress := &shareProgress{ready: make(chan struct{})}
	share, err := goncembed.StartDirectShareSource(source, net.JoinHostPort(out.LocalIP, strconv.Itoa(port)), out.Passphrase, progress)
	if err != nil {
		fail("failed", err)
		return
	}
	defer func() {
		share.Stop()
		select {
		case <-share.Done():
		case <-time.After(5 * time.Second):
		}
	}()
	// Offer only once the share accepts connections, so the recipient's
	// first connection attempt succeeds.
	select {
	case <-progress.ready:
	case <-share.Done():
		fail("failed", errors.New("could not start sharing the files"))
		return
	case <-time.After(10 * time.Second):
		fail("failed", errors.New("timed out starting the file share"))
		return
	case <-ctx.Done():
		fail("cancelled", nil)
		return
	}

	// Share the same files through gonc's LAN-only P2P mode too. If the
	// recipient cannot reach the direct listener (for example because this
	// computer's firewall blocks it), both sides find each other with
	// multicast and dial each other at the same time.
	// The LAN share reports a discovery error when it is stopped after the
	// transfer; that is expected, so stop logging it once the send ends.
	var sendEnded atomic.Bool
	lanProgress := &shareProgress{ready: make(chan struct{}), log: func(msg string) {
		if !sendEnded.Load() {
			m.logf("LAN share for %s: %s", info.PeerName, msg)
		}
	}}
	lanShare, lanErr := goncembed.StartP2PShareSourceWithOptions(source, out.Passphrase, goncembed.P2POptions{LANOnly: true}, lanProgress)
	if lanErr != nil {
		m.logf("LAN share for %s unavailable: %v", info.PeerName, lanErr)
		lanShare = nil
	} else {
		defer func() {
			sendEnded.Store(true)
			stopSession(lanShare)
		}()
	}
	paths := "direct only"
	if lanShare != nil {
		paths = "direct and LAN path"
	}
	m.logf("sharing with %s on %s:%d (%s)", info.PeerName, out.LocalIP, port, paths)

	m.mu.Lock()
	nickname := m.store.Nickname
	m.mu.Unlock()
	offerItems := make([]nearby.OfferItem, 0, len(info.Items))
	for _, item := range info.Items {
		offerItems = append(offerItems, nearby.OfferItem{Name: item.Name, Dir: item.IsDir, Size: item.Size})
	}
	if err := out.Offer(nearby.OfferInfo{
		Port: port, Name: nickname, LAN: lanShare != nil,
		Items: offerItems, Files: info.FileCount, Bytes: info.TotalBytes,
	}); err != nil {
		fail("failed", err)
		return
	}
	info.Status = "waiting"
	m.putTransfer(info)

	respCtx, respCancel := context.WithTimeout(ctx, offerTimeout+30*time.Second)
	accepted, err := out.WaitResponse(respCtx)
	respCancel()
	if err != nil {
		fail("failed", err)
		return
	}
	if !accepted {
		fail("declined", nil)
		return
	}
	info.Status = "transferring"
	m.putTransfer(info)

	// Report the share's outgoing byte count until the recipient finishes.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	done := make(chan struct{})
	var ok bool
	var msg string
	go func() {
		defer close(done)
		waitCtx, waitCancel := context.WithTimeout(ctx, finishedTimeout)
		defer waitCancel()
		ok, msg, err = out.WaitFinished(waitCtx)
	}()
report:
	for {
		select {
		case <-tick.C:
			info.DoneBytes, info.BytesPerSecond = progress.snapshot()
			lanBytes, lanBps := lanProgress.snapshot()
			info.DoneBytes += lanBytes
			info.BytesPerSecond += lanBps
			if info.TotalBytes > 0 && info.DoneBytes > info.TotalBytes {
				info.DoneBytes = info.TotalBytes
			}
			m.putTransfer(info)
		case <-done:
			break report
		}
	}
	if err != nil {
		fail("failed", err)
		return
	}
	if !ok {
		if msg == recipientCancelled {
			info.Status, info.Message, info.BytesPerSecond = "cancelled", "cancelled by the recipient", 0
			m.putTransfer(info)
			return
		}
		fail("failed", errors.New(firstNonEmpty(msg, "the recipient could not save the files")))
		return
	}
	info.Status, info.Message, info.DoneBytes, info.BytesPerSecond = "completed", msg, info.TotalBytes, 0
	info.DoneFiles = info.FileCount
	m.putTransfer(info)
}

// recipientCancelled is the message a recipient sends when its user cancels.
const recipientCancelled = "the recipient cancelled"

func plural(n int64, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.FormatInt(n, 10) + " " + word + "s"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func freePort(ip string) (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// shareProgress records traffic reported by the sending session.
type shareProgress struct {
	mu         sync.Mutex
	outBytes   int64
	outBps     float64
	ready      chan struct{}
	readyOnce  sync.Once
	log        func(string)
	lastStatus string
}

func (p *shareProgress) Event(string, string) {}
func (p *shareProgress) P2PReport(topic, side, status, network, mode, peer string, timestamp int64, pid int) {
	p.mu.Lock()
	changed := status != p.lastStatus
	p.lastStatus = status
	p.mu.Unlock()
	if changed && p.log != nil {
		p.log(strings.TrimSpace(status + " " + mode + " " + peer))
	}
}
func (p *shareProgress) Stopped(int)  {}
func (p *shareProgress) Error(string) {}

// Ready reports that the share is listening.
func (p *shareProgress) Ready(string) {
	p.readyOnce.Do(func() { close(p.ready) })
}
func (p *shareProgress) Traffic(side string, inBytes, outBytes int64, inBps, outBps float64, elapsed int64, connCount int, final bool) {
	p.mu.Lock()
	p.outBytes, p.outBps = outBytes, outBps
	p.mu.Unlock()
}

func (p *shareProgress) snapshot() (int64, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.outBytes, int64(p.outBps)
}

// ---- receiving ----

type handler struct{ m *Manager }

func (h handler) DevicesChanged(devices []nearby.Device) {
	h.m.notePeersSeen(devices, false)
	h.m.emitState()
}

// notePeersSeen records when paired devices were last nearby: when one is
// first seen, at most every peerSeenSaveEvery while it stays, and when it
// leaves the list. With flush set, every listed device is recorded, as the
// app closes.
func (m *Manager) notePeersSeen(devices []nearby.Device, flush bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	present := map[string]time.Time{}
	for _, d := range devices {
		if d.Paired && d.PeerID != "" && !d.LastSeen.IsZero() {
			present[d.PeerID] = d.LastSeen
		}
	}
	updates := map[string]time.Time{}
	for id, seen := range present {
		if prev := m.store.PeerLastSeen[id]; flush || prev.IsZero() || seen.Sub(prev) >= peerSeenSaveEvery {
			updates[id] = seen
		}
	}
	for id, seen := range m.nearbyPeers {
		if _, still := present[id]; !still && seen.After(m.store.PeerLastSeen[id]) {
			updates[id] = seen
		}
	}
	m.nearbyPeers = present
	paired := map[string]bool{}
	for _, p := range m.store.Peers {
		paired[p.InstallationID] = true
	}
	next := m.store
	next.PeerLastSeen = map[string]time.Time{}
	for id, seen := range m.store.PeerLastSeen {
		next.PeerLastSeen[id] = seen
	}
	changed := false
	for id, seen := range updates {
		if paired[id] && !seen.Equal(next.PeerLastSeen[id]) {
			next.PeerLastSeen[id] = seen
			changed = true
		}
	}
	if !changed {
		return
	}
	next.Peers = append([]nearby.PeerRecord(nil), m.store.Peers...)
	next.Addresses = append([]string(nil), m.store.Addresses...)
	if err := nearbystore.SaveWith(m.StorePath, m.storeOptions(), next); err != nil {
		return // keep the old times; the next change tries again
	}
	m.store = next
}

func (h handler) IncomingPairing(p *nearby.Pairing) { h.m.trackPairing(p, "") }

func (h handler) IncomingTransfer(t *nearby.IncomingTransfer) { go h.m.runReceive(t) }

// readyCallback captures the receive session's local HTTP endpoint.
type readyCallback struct {
	ready chan string
	once  sync.Once
	// log, when set, receives the session's own log lines.
	log func(string)
}

func (c *readyCallback) Event(level, message string) {
	if c.log != nil && !strings.Contains(message, "IN: ") {
		c.log(message)
	}
}
func (c *readyCallback) P2PReport(string, string, string, string, string, string, int64, int) {}
func (c *readyCallback) Ready(endpoint string) {
	c.once.Do(func() { c.ready <- endpoint })
}
func (c *readyCallback) Stopped(int)  {}
func (c *readyCallback) Error(string) {}

func summarizeRemote(files []httpdownload.FileInfo) ([]FileSummary, int64, int64) {
	top := map[string]*FileSummary{}
	var order []string
	var count, total int64
	for _, f := range files {
		p := strings.Trim(f.Path, "/")
		if p == "" {
			continue
		}
		first, _, nested := strings.Cut(p, "/")
		item, ok := top[first]
		if !ok {
			item = &FileSummary{Name: first, IsDir: nested || f.IsDir}
			top[first] = item
			order = append(order, first)
		}
		if nested {
			item.IsDir = true
		}
		if !f.IsDir && f.Size > 0 {
			item.Size += f.Size
			total += f.Size
		}
		if !f.IsDir {
			count++
		}
	}
	items := make([]FileSummary, 0, len(order))
	for _, name := range order {
		items = append(items, *top[name])
	}
	return items, count, total
}

// receiveConn is the outcome of connecting to the sender's share.
type receiveConn struct {
	endpoint string
	session  *goncembed.Session
	err      error
}

func (m *Manager) runReceive(t *nearby.IncomingTransfer) {
	info := TransferInfo{ID: t.ID, Direction: "receive", PeerName: t.PeerName, Status: "connecting", Quick: !t.Verified}
	if t.PeerID != "" {
		info.DeviceID = "p-" + t.PeerID
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.setCancel(info.ID, cancel)
	defer m.setCancel(info.ID, nil)
	go func() {
		select {
		case <-t.Cancelled():
			cancel()
		case <-ctx.Done():
		}
	}()

	finish := func(status, message string) {
		info.Status, info.Message, info.BytesPerSecond, info.Endpoint = status, message, 0, ""
		m.putTransfer(info)
	}

	// Connect to the sender's share in the background, so the user is asked
	// at once and usually answers while this runs.
	connCh := make(chan receiveConn, 1)
	go func() {
		endpoint, session, err := m.openReceive(ctx, t)
		connCh <- receiveConn{endpoint, session, err}
	}()
	var conn receiveConn
	haveConn := false
	getConn := func() receiveConn {
		if !haveConn {
			conn, haveConn = <-connCh, true
		}
		return conn
	}
	// Runs before the deferred cancel, which ends a connection attempt that
	// is still running.
	defer func() {
		if haveConn {
			if conn.session != nil {
				stopSession(conn.session)
			}
			return
		}
		go func() {
			if r := <-connCh; r.session != nil {
				stopSession(r.session)
			}
		}()
	}()
	list := func(endpoint string) ([]httpdownload.FileInfo, error) {
		listCtx, listCancel := context.WithTimeout(ctx, 30*time.Second)
		defer listCancel()
		files, err := httpdownload.List(listCtx, endpoint, "")
		if err != nil {
			return nil, fmt.Errorf("could not read the file list: %w", err)
		}
		return files, nil
	}

	var files []httpdownload.FileInfo
	if t.Files > 0 || len(t.Items) > 0 {
		for _, item := range t.Items {
			info.Items = append(info.Items, FileSummary{Name: item.Name, IsDir: item.Dir, Size: item.Size})
		}
		info.FileCount, info.TotalBytes = t.Files, t.Bytes
	} else {
		// The sender sent no summary, so read the file list before asking.
		r := getConn()
		if r.err == nil {
			files, r.err = list(r.endpoint)
		}
		if r.err != nil {
			t.Close()
			if ctx.Err() != nil {
				finish("cancelled", "")
			} else {
				finish("failed", r.err.Error())
			}
			return
		}
		info.Items, info.FileCount, info.TotalBytes = summarizeRemote(files)
	}

	m.mu.Lock()
	info.SaveDir = m.saveDirLocked()
	in := &incomingTransfer{t: t, decided: make(chan bool, 1)}
	m.incoming[info.ID] = in
	m.mu.Unlock()
	info.Status = "offered"
	m.putTransfer(info)
	m.logf("%s wants to send %s; asking", t.PeerName, plural(info.FileCount, "file"))

	var accept bool
	select {
	case accept = <-in.decided:
	case <-time.After(offerTimeout):
		accept = false
	case <-ctx.Done():
	}
	// Once answered, Cancel stops the transfer rather than answering again.
	m.mu.Lock()
	delete(m.incoming, info.ID)
	// The user may have picked another folder while the request was shown.
	info.SaveDir = m.saveDirLocked()
	m.mu.Unlock()
	if ctx.Err() != nil {
		t.Close()
		finish("cancelled", "the sender cancelled")
		return
	}
	if !accept {
		_ = t.Decline()
		finish("declined", "")
		return
	}
	if err := t.Accept(); err != nil {
		t.Close()
		finish("failed", err.Error())
		return
	}
	failAccepted := func(err error) {
		if ctx.Err() != nil {
			_ = t.Finish(false, recipientCancelled)
			finish("cancelled", "")
			return
		}
		_ = t.Finish(false, err.Error())
		finish("failed", err.Error())
	}
	if !haveConn {
		select {
		case conn = <-connCh:
			haveConn = true
		default:
			info.Status = "connecting"
			m.putTransfer(info)
		}
	}
	r := getConn()
	if r.err != nil {
		failAccepted(r.err)
		return
	}
	if m.ExternalDownload {
		m.awaitExternalDownload(ctx, t, &info, r.endpoint, failAccepted, finish)
		return
	}
	if files == nil {
		var err error
		if files, err = list(r.endpoint); err != nil {
			failAccepted(err)
			return
		}
		info.Items, info.FileCount, info.TotalBytes = summarizeRemote(files)
	}
	info.Status = "transferring"
	m.putTransfer(info)

	saved, err := m.download(ctx, &info, r.endpoint, files)
	if err != nil {
		failAccepted(err)
		return
	}
	info.SavedPaths = saved
	info.DoneBytes, info.DoneFiles = info.TotalBytes, info.FileCount
	_ = t.Finish(true, "")
	finish("completed", "")
}

// awaitExternalDownload reports the endpoint and waits for the app to finish
// downloading from it.
func (m *Manager) awaitExternalDownload(ctx context.Context, t *nearby.IncomingTransfer, info *TransferInfo, endpoint string,
	failAccepted func(error), finish func(status, message string)) {
	done := make(chan receiveResult, 1)
	m.mu.Lock()
	m.finishers[info.ID] = done
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.finishers, info.ID)
		m.mu.Unlock()
	}()
	info.Status, info.Endpoint = "ready", endpoint
	m.putTransfer(*info)
	select {
	case r := <-done:
		if !r.ok {
			msg := strings.TrimSpace(r.message)
			if msg == "" {
				msg = "the files could not be saved"
			}
			failAccepted(errors.New(msg))
			return
		}
		info.DoneBytes, info.DoneFiles = info.TotalBytes, info.FileCount
		_ = t.Finish(true, "")
		finish("completed", "")
	case <-ctx.Done():
		failAccepted(ctx.Err())
	}
}

// FinishReceive reports that the app finished downloading a transfer it
// received in ExternalDownload mode. A failure message is shown to the
// sender.
func (m *Manager) FinishReceive(id string, ok bool, message string) {
	m.mu.Lock()
	done := m.finishers[id]
	m.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case done <- receiveResult{ok: ok, message: message}:
	default:
	}
}

func stopSession(s *goncembed.Session) {
	s.Stop()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
	}
}

// openReceive connects to the sender's share: directly first, then, if the
// sender offered it, through gonc's LAN-only P2P mode, where both sides find
// each other with multicast and dial each other at the same time.
func (m *Manager) openReceive(ctx context.Context, t *nearby.IncomingTransfer) (string, *goncembed.Session, error) {
	wait := readyTimeout
	if t.LANFallback {
		wait = directReceiveWait
	}
	cb := &readyCallback{ready: make(chan string, 1)}
	direct, err := goncembed.StartDirectReceive(t.SenderAddress, t.Passphrase, cb)
	if err == nil {
		select {
		case endpoint := <-cb.ready:
			m.logf("receiving from %s directly (%s)", t.PeerName, t.SenderAddress)
			return endpoint, direct, nil
		case <-time.After(wait):
			m.logf("direct connection to %s at %s did not answer within %s", t.PeerName, t.SenderAddress, wait)
		case <-ctx.Done():
			go stopSession(direct)
			return "", nil, ctx.Err()
		}
		go stopSession(direct)
	} else {
		m.logf("direct receive from %s could not start: %v", t.PeerName, err)
	}
	if !t.LANFallback {
		return "", nil, errors.New("could not connect to the sender")
	}
	m.logf("trying the LAN path to %s", t.PeerName)
	lcb := &readyCallback{ready: make(chan string, 1), log: func(msg string) { m.logf("LAN receive: %s", msg) }}
	lan, err := goncembed.StartP2PReceiveWithOptions(t.Passphrase, goncembed.P2POptions{LANOnly: true}, lcb)
	if err != nil {
		return "", nil, err
	}
	select {
	case endpoint := <-lcb.ready:
		m.logf("receiving from %s over the LAN path", t.PeerName)
		return endpoint, lan, nil
	case <-time.After(lanReadyTimeout):
		go stopSession(lan)
		return "", nil, errors.New("could not connect to the sender, directly or on the LAN")
	case <-ctx.Done():
		go stopSession(lan)
		return "", nil, ctx.Err()
	}
}

// AcceptIncoming and DeclineIncoming answer an offered transfer.
func (m *Manager) AcceptIncoming(id string) error  { return m.decide(id, true) }
func (m *Manager) DeclineIncoming(id string) error { return m.decide(id, false) }

func (m *Manager) decide(id string, accept bool) error {
	m.mu.Lock()
	in := m.incoming[id]
	m.mu.Unlock()
	if in == nil {
		return errors.New("that request has ended")
	}
	select {
	case in.decided <- accept:
		return nil
	default:
		return errors.New("that request was already answered")
	}
}

// download saves the offered files into a staging folder next to the save
// folder, then moves each top-level item into place under a name that does
// not overwrite anything already there.
func (m *Manager) download(ctx context.Context, info *TransferInfo, endpoint string, files []httpdownload.FileInfo) ([]string, error) {
	saveDir := info.SaveDir
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return nil, err
	}
	staging := filepath.Join(saveDir, ".gonc-nearby-"+info.ID)
	// After a cancel or failure the staging folder may hold a large partial
	// file; remove it in the background so the result shows at once.
	cleanNow := false
	defer func() {
		if cleanNow {
			_ = os.RemoveAll(staging)
		} else {
			go os.RemoveAll(staging)
		}
	}()
	d, err := httpdownload.New(httpdownload.Config{
		ServerURL:   endpoint,
		SaveDir:     staging,
		CachedFiles: files,
		Concurrency: 4,
		MaxAttempts: 5,
	})
	if err != nil {
		return nil, err
	}
	// Download workers report progress concurrently.
	var progressMu sync.Mutex
	var failed int64
	err = d.Start(ctx, func(e httpdownload.Event) {
		if e.Type != "progress" {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		info.DoneBytes, info.DoneFiles, info.BytesPerSecond = e.DoneBytes, e.DoneFiles, e.BytesPerSecond
		failed = e.FailedFiles
		m.putTransfer(*info)
	})
	if err != nil {
		return nil, err
	}
	if failed > 0 {
		return nil, fmt.Errorf("%d files could not be received", failed)
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return nil, err
	}
	var saved []string
	for _, e := range entries {
		dst := uniquePath(filepath.Join(saveDir, e.Name()))
		if err := os.Rename(filepath.Join(staging, e.Name()), dst); err != nil {
			return saved, err
		}
		saved = append(saved, dst)
	}
	cleanNow = true
	return saved, nil
}

// uniquePath returns path, or "name (n).ext" when path already exists.
func uniquePath(path string) string {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	dir, base := filepath.Split(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // dotfile such as ".env"
		stem, ext = base, ""
	}
	for n := 1; ; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}
