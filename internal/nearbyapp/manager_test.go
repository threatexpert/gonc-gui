package nearbyapp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/threatexpert/gonc/v2/nearby"
)

type recorder struct {
	mu        sync.Mutex
	pairings  []PairingInfo
	transfers []TransferInfo
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch v := data.(type) {
	case PairingInfo:
		r.pairings = append(r.pairings, v)
	case TransferInfo:
		r.transfers = append(r.transfers, v)
	}
}

func (r *recorder) waitPairing(t *testing.T, match func(PairingInfo) bool) PairingInfo {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, p := range r.pairings {
			if match(p) {
				r.mu.Unlock()
				return p
			}
		}
		r.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pairing event not seen; got %+v", r.pairings)
	return PairingInfo{}
}

func (r *recorder) waitTransfer(t *testing.T, match func(TransferInfo) bool) TransferInfo {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for i := len(r.transfers) - 1; i >= 0; i-- {
			if match(r.transfers[i]) {
				tr := r.transfers[i]
				r.mu.Unlock()
				return tr
			}
		}
		r.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("transfer event not seen; got %+v", r.transfers)
	return TransferInfo{}
}

func startManager(t *testing.T, name string) (*Manager, *recorder, string) {
	t.Helper()
	dir := t.TempDir()
	saveDir := filepath.Join(dir, "Downloads")
	rec := &recorder{}
	m := New(rec.emit, func() string { return saveDir })
	m.StorePath = filepath.Join(dir, "nearby.json.enc")
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	if err := m.SetNickname(name); err != nil {
		t.Fatal(err)
	}
	return m, rec, saveDir
}

func loopback(m *Manager) string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(m.State().Port))
}

func TestPairSendAcceptAndSaveWithoutOverwriting(t *testing.T) {
	alice, aliceEvents, _ := startManager(t, "Alice")
	bob, bobEvents, bobSaveDir := startManager(t, "Bob")

	devBob, err := alice.AddAddress(loopback(bob))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bob.AddAddress(loopback(alice)); err != nil {
		t.Fatal(err)
	}

	// Pair: both sides see the same code and confirm.
	outgoing, err := alice.Pair(devBob.ID)
	if err != nil {
		t.Fatal(err)
	}
	incoming := bobEvents.waitPairing(t, func(p PairingInfo) bool { return p.Incoming && p.Status == "waiting" })
	if incoming.Code != outgoing.Code || incoming.PeerName != "Alice" {
		t.Fatalf("pairing mismatch: %+v vs %+v", outgoing, incoming)
	}
	if err := alice.ConfirmPairing(outgoing.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := bob.ConfirmPairing(incoming.ID, true); err != nil {
		t.Fatal(err)
	}
	aliceEvents.waitPairing(t, func(p PairingInfo) bool { return p.Status == "paired" })
	bobEvents.waitPairing(t, func(p PairingInfo) bool { return p.Status == "paired" })
	if peers := alice.State().Peers; len(peers) != 1 || peers[0].Nickname != "Bob" {
		t.Fatalf("Alice's saved peers: %+v", peers)
	}

	// Files to send: one file and one folder.
	src := t.TempDir()
	report := filepath.Join(src, "report.txt")
	if err := os.WriteFile(report, []byte("quarterly numbers"), 0o600); err != nil {
		t.Fatal(err)
	}
	photos := filepath.Join(src, "photos")
	if err := os.MkdirAll(photos, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photos, "a.jpg"), make([]byte, 200_000), 0o600); err != nil {
		t.Fatal(err)
	}

	paired := "p-" + func() string {
		for _, d := range alice.State().Devices {
			if d.Paired {
				return d.PeerID
			}
		}
		t.Fatal("no paired device in Alice's list")
		return ""
	}()

	send := func() {
		t.Helper()
		id, err := alice.Send(paired, []string{report, photos})
		if err != nil {
			t.Fatal(err)
		}
		offer := bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.Direction == "receive" && x.Status == "offered" })
		if offer.FileCount != 2 || offer.TotalBytes != int64(len("quarterly numbers"))+200_000 || len(offer.Items) != 2 {
			t.Fatalf("offer summary: %+v", offer)
		}
		if err := bob.AcceptIncoming(offer.ID); err != nil {
			t.Fatal(err)
		}
		bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == offer.ID && x.Status == "completed" })
		aliceEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == id && x.Status == "completed" })
		bobEvents.mu.Lock()
		bobEvents.transfers = nil
		bobEvents.mu.Unlock()
	}

	send()
	got, err := os.ReadFile(filepath.Join(bobSaveDir, "report.txt"))
	if err != nil || string(got) != "quarterly numbers" {
		t.Fatalf("received report: %q, %v", got, err)
	}
	if st, err := os.Stat(filepath.Join(bobSaveDir, "photos", "a.jpg")); err != nil || st.Size() != 200_000 {
		t.Fatalf("received photo: %v", err)
	}

	// A second transfer of the same names must not overwrite the first.
	send()
	if _, err := os.Stat(filepath.Join(bobSaveDir, "report (1).txt")); err != nil {
		t.Fatalf("second copy not saved under a new name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bobSaveDir, "photos (1)", "a.jpg")); err != nil {
		t.Fatalf("second folder not saved under a new name: %v", err)
	}
	entries, _ := os.ReadDir(bobSaveDir)
	for _, e := range entries {
		if len(e.Name()) > 13 && e.Name()[:13] == ".gonc-nearby-" {
			t.Fatalf("staging folder left behind: %s", e.Name())
		}
	}
}

func TestDeclinedTransferReportsToSender(t *testing.T) {
	alice, aliceEvents, _ := startManager(t, "Alice")
	bob, bobEvents, bobSaveDir := startManager(t, "Bob")
	devBob, err := alice.AddAddress(loopback(bob))
	if err != nil {
		t.Fatal(err)
	}
	out, err := alice.Pair(devBob.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := bobEvents.waitPairing(t, func(p PairingInfo) bool { return p.Incoming })
	_ = alice.ConfirmPairing(out.ID, true)
	_ = bob.ConfirmPairing(in.ID, true)
	aliceEvents.waitPairing(t, func(p PairingInfo) bool { return p.Status == "paired" })

	file := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	var deviceID string
	deadline := time.Now().Add(5 * time.Second)
	for deviceID == "" && time.Now().Before(deadline) {
		for _, d := range alice.State().Devices {
			if d.Paired {
				deviceID = d.ID
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	id, err := alice.Send(deviceID, []string{file})
	if err != nil {
		t.Fatal(err)
	}
	offer := bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.Status == "offered" })
	if err := bob.DeclineIncoming(offer.ID); err != nil {
		t.Fatal(err)
	}
	aliceEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == id && x.Status == "declined" })
	if _, err := os.Stat(filepath.Join(bobSaveDir, "secret.txt")); err == nil {
		t.Fatal("declined file was saved")
	}
}

func TestUniquePath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "a (1).txt", ".env", "folder"} {
		_ = os.WriteFile(filepath.Join(dir, name), nil, 0o600)
	}
	cases := map[string]string{
		"a.txt":  "a (2).txt",
		".env":   ".env (1)",
		"folder": "folder (1)",
		"new.md": "new.md",
	}
	for in, want := range cases {
		if got := filepath.Base(uniquePath(filepath.Join(dir, in))); got != want {
			t.Errorf("uniquePath(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestQuickSendWithoutPairing(t *testing.T) {
	alice, aliceEvents, _ := startManager(t, "Alice")
	bob, bobEvents, bobSaveDir := startManager(t, "Bob")
	devBob, err := alice.AddAddress(loopback(bob))
	if err != nil {
		t.Fatal(err)
	}
	if !devBob.QuickReceive || devBob.Paired {
		t.Fatalf("Bob before quick send: %+v", devBob)
	}
	src := filepath.Join(t.TempDir(), "flyer.txt")
	if err := os.WriteFile(src, []byte("public flyer"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, err := alice.QuickSend(devBob.ID, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	offer := bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.Direction == "receive" && x.Status == "offered" })
	if !offer.Quick || offer.DeviceID != "" || offer.PeerName != "Alice" || offer.FileCount != 1 {
		t.Fatalf("quick offer: %+v", offer)
	}
	if err := bob.AcceptIncoming(offer.ID); err != nil {
		t.Fatal(err)
	}
	bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == offer.ID && x.Status == "completed" })
	aliceEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == id && x.Status == "completed" })
	if got, err := os.ReadFile(filepath.Join(bobSaveDir, "flyer.txt")); err != nil || string(got) != "public flyer" {
		t.Fatalf("received flyer: %q, %v", got, err)
	}
	if len(alice.State().Peers) != 0 || len(bob.State().Peers) != 0 {
		t.Fatal("quick send saved a pairing")
	}

	// With quick receive off, the sender is told to pair first.
	if err := bob.SetQuickReceive(false); err != nil {
		t.Fatal(err)
	}
	if bob.State().QuickReceive {
		t.Fatal("quick receive still reported on")
	}
	id, err = alice.QuickSend(devBob.ID, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	failed := aliceEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == id && x.Status == "failed" })
	if failed.Message == "" {
		t.Fatal("refused quick send has no reason")
	}
}

// Cancelling shows on both sides at once, from either side, while a large
// file is still being received.
func TestCancelShowsImmediately(t *testing.T) {
	for _, side := range []string{"sender", "receiver"} {
		t.Run(side, func(t *testing.T) {
			alice, aliceEvents, _ := startManager(t, "Alice")
			bob, bobEvents, bobSaveDir := startManager(t, "Bob")
			devBob, err := alice.AddAddress(loopback(bob))
			if err != nil {
				t.Fatal(err)
			}
			big := filepath.Join(t.TempDir(), "big.bin")
			f, err := os.Create(big)
			if err != nil {
				t.Fatal(err)
			}
			_ = f.Truncate(8 << 30) // sparse; far more than loopback moves in the test
			f.Close()
			id, err := alice.QuickSend(devBob.ID, []string{big})
			if err != nil {
				t.Fatal(err)
			}
			offer := bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.Direction == "receive" && x.Status == "offered" })
			if err := bob.AcceptIncoming(offer.ID); err != nil {
				t.Fatal(err)
			}
			bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == offer.ID && x.Status == "transferring" && x.DoneBytes > 0 })
			start := time.Now()
			if side == "sender" {
				err = alice.CancelTransfer(id)
			} else {
				err = bob.CancelTransfer(offer.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			sent := aliceEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == id && x.finished() })
			got := bobEvents.waitTransfer(t, func(x TransferInfo) bool { return x.ID == offer.ID && x.finished() })
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("cancel took %v to show on both sides", elapsed)
			}
			if sent.Status != "cancelled" || got.Status != "cancelled" {
				t.Fatalf("after cancel: sender %s (%s), recipient %s (%s)", sent.Status, sent.Message, got.Status, got.Message)
			}
			// The partial file is removed in the background.
			deadline := time.Now().Add(10 * time.Second)
			for {
				entries, _ := os.ReadDir(bobSaveDir)
				if len(entries) == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("partial download left behind: %v", entries)
				}
				time.Sleep(100 * time.Millisecond)
			}
		})
	}
}

func TestErrorCodesNameFailuresAppsWordThemselves(t *testing.T) {
	for err, want := range map[error]string{
		nearby.ErrNotPaired: "not-paired",
		fmt.Errorf("LAN rendezvous: %w", nearby.ErrNotPaired): "not-paired",
		nearby.ErrQuickRefused:                                "quick-refused",
		nearby.ErrBusy:                                        "busy",
		context.DeadlineExceeded:                              "timeout",
		errors.New("disk full"):                               "",
	} {
		if got := errorCode(err); got != want {
			t.Errorf("errorCode(%v) = %q, want %q", err, got, want)
		}
	}
}
