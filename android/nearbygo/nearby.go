// Package nearbygo is the Android app's Nearby engine. It wraps the desktop
// app's nearbyapp.Manager in a gomobile-friendly API and is bound into
// mobilegonc.aar together with the app's mobilegonc package (see
// android/update-mobilegonc-aar.bat).
//
// Everything reaches the app as events on one NearbyListener, with JSON
// payloads:
//
//	"state"    this device, nearby devices and saved pairings
//	"pairing"  a pairing attempt: show Code, then call ConfirmPairing
//	"transfer" progress of a send or receive
//	"log"      a plain-text line for the activity log
//
// Receiving is split with the app: after Accept the node connects to the
// sender and reports status "ready" with Endpoint, the local HTTP address of
// the sender's share. The app downloads every file from it and then calls
// FinishReceive.
package nearbygo

import (
	"encoding/json"
	"errors"
	"strings"

	"gonc-gui/android/internal/mobilefilesource"
	"gonc-gui/android/mobilegonc"
	"gonc-gui/internal/nearbyapp"
)

// NearbyListener receives Nearby events. Calls arrive on Go threads.
type NearbyListener interface {
	OnNearbyEvent(kind string, payload string)
}

// NearbyNode is a running Nearby service.
type NearbyNode struct {
	m *nearbyapp.Manager
}

// StartNearby starts the service. storePath is a file in app-private
// storage and storeKey a 32-byte key kept in the platform keystore;
// defaultName names a new device and deviceType is phone or tablet. The
// service does not announce itself until SetDiscovering(true).
func StartNearby(storePath string, storeKey []byte, defaultName string, deviceType string, listener NearbyListener) (*NearbyNode, error) {
	if listener == nil {
		return nil, errors.New("listener is required")
	}
	if strings.TrimSpace(storePath) == "" {
		return nil, errors.New("store path is required")
	}
	if len(storeKey) != 32 {
		return nil, errors.New("store key must be 32 bytes")
	}
	m := nearbyapp.New(func(name string, data any) { emit(listener, name, data) }, func() string { return "" })
	m.StorePath = storePath
	m.StoreKey = append([]byte(nil), storeKey...)
	m.DefaultName = defaultName
	m.DeviceType = deviceType
	m.ExternalDownload = true
	if err := m.Start(); err != nil {
		return nil, err
	}
	return &NearbyNode{m: m}, nil
}

// emit forwards a manager event under a short kind name.
func emit(listener NearbyListener, name string, data any) {
	kind := strings.TrimPrefix(name, "nearby:")
	if line, ok := data.(nearbyapp.LogLine); ok {
		listener.OnNearbyEvent(kind, line.Message)
		return
	}
	listener.OnNearbyEvent(kind, marshal(data))
}

func marshal(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// Close stops the service and every transfer.
func (n *NearbyNode) Close() { n.m.Close() }

// StateJSON returns the current "state" payload.
func (n *NearbyNode) StateJSON() string { return marshal(n.m.State()) }

// SetDiscovering announces this device and looks for others only while on.
func (n *NearbyNode) SetDiscovering(on bool) { n.m.SetDiscovering(on) }

// Refresh asks nearby devices to announce themselves now.
func (n *NearbyNode) Refresh() { _ = n.m.Refresh() }

// SetNickname changes the name other devices see.
func (n *NearbyNode) SetNickname(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("enter a device name")
	}
	return n.m.SetNickname(name)
}

// SetVisible sets whether other devices can see this one.
func (n *NearbyNode) SetVisible(on bool) error { return n.m.SetVisible(on) }

// SetQuickReceive sets whether unpaired devices may offer files.
func (n *NearbyNode) SetQuickReceive(on bool) error { return n.m.SetQuickReceive(on) }

// Forget removes a pairing.
func (n *NearbyNode) Forget(peerID string) error { return n.m.Forget(peerID) }

// Pair starts pairing with a nearby device; the code arrives as a "pairing"
// event.
func (n *NearbyNode) Pair(deviceID string) error {
	_, err := n.m.Pair(deviceID)
	return err
}

// ConfirmPairing passes the user's answer to whether both screens show the
// same number.
func (n *NearbyNode) ConfirmPairing(id string, accept bool) error {
	return n.m.ConfirmPairing(id, accept)
}

// Send offers the source's files to a device. Set quick to send to an
// unpaired device without comparing numbers. Progress arrives as "transfer"
// events with the returned ID.
func (n *NearbyNode) Send(deviceID string, source mobilegonc.AndroidFileSource, quick bool) (string, error) {
	if source == nil {
		return "", errors.New("add files to send")
	}
	return n.m.SendSource(deviceID, mobilefilesource.New(source), quick)
}

// Accept accepts an offered transfer.
func (n *NearbyNode) Accept(id string) error { return n.m.AcceptIncoming(id) }

// Decline declines an offered transfer.
func (n *NearbyNode) Decline(id string) error { return n.m.DeclineIncoming(id) }

// FinishReceive reports that the app finished downloading an accepted
// transfer. A failure message is shown to the sender.
func (n *NearbyNode) FinishReceive(id string, ok bool, message string) {
	n.m.FinishReceive(id, ok, message)
}

// Cancel stops a transfer, or declines an offer that is still waiting.
func (n *NearbyNode) Cancel(id string) error { return n.m.CancelTransfer(id) }
