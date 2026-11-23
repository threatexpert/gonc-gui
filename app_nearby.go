package main

import (
	"errors"

	"gonc-gui/internal/nearbyapp"

	"github.com/threatexpert/gonc/v2/nearby"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type NearbyState = nearbyapp.State
type NearbyPairing = nearbyapp.PairingInfo
type NearbyTransfer = nearbyapp.TransferInfo
type NearbyDevice = nearby.Device

// startNearby runs once the Wails context exists, so events can be emitted.
func (a *App) startNearby() {
	a.nearby = nearbyapp.New(func(name string, data any) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, name, data)
		}
	}, defaultSaveDir)
	go func() { _ = a.nearby.Start() }()
}

func (a *App) nearbyManager() (*nearbyapp.Manager, error) {
	if a.nearby == nil {
		return nil, errors.New("Nearby is not running")
	}
	return a.nearby, nil
}

func (a *App) NearbyState() NearbyState {
	m, err := a.nearbyManager()
	if err != nil {
		return NearbyState{Error: err.Error()}
	}
	return m.State()
}

func (a *App) NearbyTransfers() []NearbyTransfer {
	m, err := a.nearbyManager()
	if err != nil {
		return nil
	}
	return m.Transfers()
}

func (a *App) NearbySetNickname(name string) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.SetNickname(name)
}

// NearbySetDiscovering is called when the Nearby page opens or closes.
func (a *App) NearbySetDiscovering(on bool) {
	if m, err := a.nearbyManager(); err == nil {
		m.SetDiscovering(on)
	}
}

func (a *App) NearbySetVisible(visible bool) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.SetVisible(visible)
}

// NearbyChooseSaveDir lets the user pick where received files go. It returns
// the chosen folder, or an empty string when the dialog was cancelled.
func (a *App) NearbyChooseSaveDir(title string) (string, error) {
	m, err := a.nearbyManager()
	if err != nil {
		return "", err
	}
	dir, err := a.SelectFolder(title)
	if err != nil || dir == "" {
		return "", err
	}
	return dir, m.SetSaveDir(dir)
}

func (a *App) NearbyRefresh() error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.Refresh()
}

func (a *App) NearbyPair(deviceID string) (NearbyPairing, error) {
	m, err := a.nearbyManager()
	if err != nil {
		return NearbyPairing{}, err
	}
	return m.Pair(deviceID)
}

func (a *App) NearbyConfirmPairing(id string, accept bool) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.ConfirmPairing(id, accept)
}

func (a *App) NearbyForget(peerID string) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.Forget(peerID)
}

func (a *App) NearbySend(deviceID string, paths []string) (string, error) {
	m, err := a.nearbyManager()
	if err != nil {
		return "", err
	}
	return m.Send(deviceID, paths)
}

// NearbyQuickSend sends to a device without pairing.
func (a *App) NearbyQuickSend(deviceID string, paths []string) (string, error) {
	m, err := a.nearbyManager()
	if err != nil {
		return "", err
	}
	return m.QuickSend(deviceID, paths)
}

func (a *App) NearbySetQuickReceive(on bool) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.SetQuickReceive(on)
}

func (a *App) NearbyCancel(id string) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.CancelTransfer(id)
}

func (a *App) NearbyAccept(id string) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.AcceptIncoming(id)
}

func (a *App) NearbyDecline(id string) error {
	m, err := a.nearbyManager()
	if err != nil {
		return err
	}
	return m.DeclineIncoming(id)
}

func (a *App) NearbyClearFinished() {
	if m, err := a.nearbyManager(); err == nil {
		m.ClearFinished()
	}
}

// NearbyLogs returns recent Nearby lines for the activity log.
func (a *App) NearbyLogs() []nearbyapp.LogLine {
	m, err := a.nearbyManager()
	if err != nil {
		return nil
	}
	return m.Logs()
}
