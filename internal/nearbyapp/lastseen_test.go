package nearbyapp

import (
	"testing"
	"time"

	"gonc-gui/internal/nearby"
	"gonc-gui/internal/nearbystore"
)

func TestPairedDevicesRememberWhenTheyWereLastNearby(t *testing.T) {
	m, _, _ := startManager(t, "Desk")
	if err := m.savePeer(nearby.PeerRecord{InstallationID: "phone", Nickname: "Phone", PairedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	lastSeen := func() time.Time {
		t.Helper()
		for _, p := range m.State().Peers {
			if p.PeerID == "phone" {
				return p.LastSeen
			}
		}
		t.Fatal("the pairing is not listed")
		return time.Time{}
	}
	if !lastSeen().IsZero() {
		t.Fatal("a device never seen since pairing has a last-seen time")
	}

	first := time.Now().Add(-time.Minute).Truncate(time.Second)
	phone := nearby.Device{ID: "p-phone", PeerID: "phone", Paired: true, LastSeen: first}
	m.notePeersSeen([]nearby.Device{phone}, false)
	if got := lastSeen(); !got.Equal(first) {
		t.Fatalf("after the first sighting: %v, want %v", got, first)
	}

	// While it stays nearby, the store is not rewritten on every announcement.
	phone.LastSeen = first.Add(10 * time.Second)
	m.notePeersSeen([]nearby.Device{phone}, false)
	if got := lastSeen(); !got.Equal(first) {
		t.Fatalf("rewritten within the hour: %v", got)
	}

	// When it leaves the list, the last time it was heard from is kept.
	m.notePeersSeen(nil, false)
	left := first.Add(10 * time.Second)
	if got := lastSeen(); !got.Equal(left) {
		t.Fatalf("after it left: %v, want %v", got, left)
	}
	saved, err := nearbystore.LoadWith(m.StorePath, m.storeOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.PeerLastSeen["phone"]; !got.Equal(left) {
		t.Fatalf("saved %v, want %v", got, left)
	}

	// Devices that are not paired are not recorded.
	m.notePeersSeen([]nearby.Device{{ID: "u-1", Nickname: "Guest", LastSeen: time.Now()}}, true)
	m.mu.Lock()
	entries := len(m.store.PeerLastSeen)
	m.mu.Unlock()
	if entries != 1 {
		t.Fatalf("%d last-seen entries, want 1", entries)
	}

	if err := m.Forget("phone"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	_, kept := m.store.PeerLastSeen["phone"]
	m.mu.Unlock()
	if kept {
		t.Fatal("forgetting a device kept its last-seen time")
	}
}
