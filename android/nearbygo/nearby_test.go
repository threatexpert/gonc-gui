package nearbygo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gonc-gui/internal/nearbystore"
)

// memorySource is a mobilegonc.AndroidFileSource with files in memory.
type memorySource struct {
	mu      sync.Mutex
	files   map[string][]byte // "/name" -> content
	handles map[int64]*memoryHandle
	next    int64
}

type memoryHandle struct {
	data []byte
	pos  int64
}

func newMemorySource(files map[string]string) *memorySource {
	s := &memorySource{files: map[string][]byte{}, handles: map[int64]*memoryHandle{}}
	for name, content := range files {
		s.files["/"+name] = []byte(content)
	}
	return s
}

func (s *memorySource) Description() string { return "memory" }

func (s *memorySource) Stat(name string) string {
	if name == "/" {
		return `{"name":"/","isDir":true}`
	}
	data, ok := s.files[name]
	if !ok {
		return ""
	}
	return fmt.Sprintf(`{"name":%q,"size":%d}`, strings.TrimPrefix(name, "/"), len(data))
}

func (s *memorySource) ReadDir(name string) string {
	if name != "/" {
		return ""
	}
	type fileInfo struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}
	var entries []fileInfo
	for path, data := range s.files {
		entries = append(entries, fileInfo{Name: strings.TrimPrefix(path, "/"), Size: len(data)})
	}
	out, _ := json.Marshal(entries)
	return string(out)
}

func (s *memorySource) Open(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[name]
	if !ok {
		return 0
	}
	s.next++
	s.handles[s.next] = &memoryHandle{data: data}
	return s.next
}

func (s *memorySource) Read(handle int64, maxBytes int) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.handles[handle]
	if h == nil || h.pos >= int64(len(h.data)) {
		return nil
	}
	end := min(h.pos+int64(maxBytes), int64(len(h.data)))
	out := h.data[h.pos:end]
	h.pos = end
	return out
}

func (s *memorySource) Seek(handle int64, offset int64, whence int) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.handles[handle]
	if h == nil {
		return -1
	}
	switch whence {
	case io.SeekStart:
		h.pos = offset
	case io.SeekCurrent:
		h.pos += offset
	case io.SeekEnd:
		h.pos = int64(len(h.data)) + offset
	}
	return h.pos
}

func (s *memorySource) Close(handle int64) {
	s.mu.Lock()
	delete(s.handles, handle)
	s.mu.Unlock()
}

type transferEvent struct {
	ID         string `json:"id"`
	Direction  string `json:"direction"`
	DeviceID   string `json:"deviceId"`
	PeerName   string `json:"peerName"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	FileCount  int64  `json:"fileCount"`
	TotalBytes int64  `json:"totalBytes"`
	Quick      bool   `json:"quick"`
	Endpoint   string `json:"endpoint"`
	Items      []struct {
		Name string `json:"name"`
	} `json:"items"`
}

func (t transferEvent) finished() bool {
	switch t.Status {
	case "completed", "declined", "cancelled", "failed":
		return true
	}
	return false
}

type pairingEvent struct {
	ID       string `json:"id"`
	Incoming bool   `json:"incoming"`
	PeerName string `json:"peerName"`
	Code     string `json:"code"`
	Status   string `json:"status"`
}

// recorder collects a node's events.
type recorder struct {
	mu        sync.Mutex
	kinds     map[string]int
	transfers []transferEvent
	pairings  []pairingEvent
	logs      []string
}

func newRecorder() *recorder { return &recorder{kinds: map[string]int{}} }

func (r *recorder) OnNearbyEvent(kind, payload string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds[kind]++
	switch kind {
	case "transfer":
		var t transferEvent
		_ = json.Unmarshal([]byte(payload), &t)
		r.transfers = append(r.transfers, t)
	case "pairing":
		var p pairingEvent
		_ = json.Unmarshal([]byte(payload), &p)
		r.pairings = append(r.pairings, p)
	case "log":
		r.logs = append(r.logs, payload)
	}
}

func waitFor[T any](t *testing.T, r *recorder, list func() []T, match func(T) bool) T {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		items := append([]T(nil), list()...)
		r.mu.Unlock()
		for _, x := range items {
			if match(x) {
				return x
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for an event")
	var zero T
	return zero
}

func (r *recorder) waitTransfer(t *testing.T, match func(transferEvent) bool) transferEvent {
	t.Helper()
	return waitFor(t, r, func() []transferEvent { return r.transfers }, match)
}

func (r *recorder) waitPairing(t *testing.T, match func(pairingEvent) bool) pairingEvent {
	t.Helper()
	return waitFor(t, r, func() []pairingEvent { return r.pairings }, match)
}

func testKey(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }

func startNode(t *testing.T, name string) (*NearbyNode, *recorder, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nearby.bin")
	rec := newRecorder()
	n, err := StartNearby(path, testKey(1), name, "phone", rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(n.Close)
	return n, rec, path
}

// meet makes a find b by address, as multicast would on a real network.
func meet(t *testing.T, a, b *NearbyNode) string {
	t.Helper()
	d, err := a.m.AddAddress("127.0.0.1:" + strconv.Itoa(b.m.State().Port))
	if err != nil {
		t.Fatal(err)
	}
	return d.ID
}

func fetch(t *testing.T, endpoint, name string) string {
	t.Helper()
	resp, err := http.Get(strings.TrimRight(endpoint, "/") + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %s %s", name, resp.Status, body)
	}
	return string(body)
}

func TestStartValidatesArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nearby.bin")
	if _, err := StartNearby(path, testKey(1)[:16], "x", "phone", newRecorder()); err == nil {
		t.Fatal("a short key was accepted")
	}
	if _, err := StartNearby(path, testKey(1), "x", "phone", nil); err == nil {
		t.Fatal("a missing listener was accepted")
	}
}

func TestQuickSendHandsTheAppAnEndpoint(t *testing.T) {
	alice, aliceEvents, _ := startNode(t, "Alice's phone")
	bob, bobEvents, _ := startNode(t, "Bob's phone")
	devBob := meet(t, alice, bob)

	if _, err := alice.Send(devBob, newMemorySource(nil), true); err == nil {
		t.Fatal("sending nothing was accepted")
	}
	if _, err := alice.Send(devBob, newMemorySource(map[string]string{"x": "1"}), false); err == nil {
		t.Fatal("a paired send to an unpaired device was accepted")
	}
	id, err := alice.Send(devBob, newMemorySource(map[string]string{"flyer.txt": "public flyer"}), true)
	if err != nil {
		t.Fatal(err)
	}
	offer := bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.Direction == "receive" && x.Status == "offered" })
	if !offer.Quick || offer.PeerName != "Alice's phone" || offer.FileCount != 1 || offer.TotalBytes != 12 ||
		len(offer.Items) != 1 || offer.Items[0].Name != "flyer.txt" {
		t.Fatalf("offer: %+v", offer)
	}
	if err := bob.Accept(offer.ID); err != nil {
		t.Fatal(err)
	}
	ready := bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == offer.ID && x.Status == "ready" })
	if !strings.HasPrefix(ready.Endpoint, "http://") {
		t.Fatalf("ready endpoint: %q", ready.Endpoint)
	}
	if got := fetch(t, ready.Endpoint, "flyer.txt"); got != "public flyer" {
		t.Fatalf("fetched %q", got)
	}
	bob.FinishReceive(offer.ID, true, "")
	done := bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == offer.ID && x.Status == "completed" })
	if done.Endpoint != "" {
		t.Fatal("a finished transfer still reports its endpoint")
	}
	aliceEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == id && x.Status == "completed" })

	// Log lines arrive as plain text under a short kind name.
	aliceEvents.mu.Lock()
	kinds, logs := aliceEvents.kinds, append([]string(nil), aliceEvents.logs...)
	aliceEvents.mu.Unlock()
	if kinds["state"] == 0 || kinds["transfer"] == 0 || len(logs) == 0 || strings.HasPrefix(logs[0], "{") {
		t.Fatalf("event kinds %v, logs %q", kinds, logs)
	}
}

func TestPairingPersistsAndPairedSendCanBeDeclined(t *testing.T) {
	alice, aliceEvents, _ := startNode(t, "Alice")
	bob, bobEvents, bobPath := startNode(t, "Bob")
	devBob := meet(t, alice, bob)
	if err := alice.Pair(devBob); err != nil {
		t.Fatal(err)
	}
	pa := aliceEvents.waitPairing(t, func(p pairingEvent) bool { return p.Status == "waiting" })
	pb := bobEvents.waitPairing(t, func(p pairingEvent) bool { return p.Status == "waiting" })
	if pa.Code == "" || pa.Code != pb.Code || !pb.Incoming || pb.PeerName != "Alice" {
		t.Fatalf("codes: %+v / %+v", pa, pb)
	}
	if err := alice.ConfirmPairing(pa.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := bob.ConfirmPairing(pb.ID, true); err != nil {
		t.Fatal(err)
	}
	aliceEvents.waitPairing(t, func(p pairingEvent) bool { return p.Status == "paired" })
	bobEvents.waitPairing(t, func(p pairingEvent) bool { return p.Status == "paired" })

	// The pairing is saved, sealed with the app's key.
	if _, err := nearbystore.LoadWith(bobPath, nearbystore.Options{Key: testKey(2)}); err == nil {
		t.Fatal("the store opened with the wrong key")
	}
	saved, err := nearbystore.LoadWith(bobPath, nearbystore.Options{Key: testKey(1)})
	if err != nil || len(saved.Peers) != 1 || saved.Peers[0].Nickname != "Alice" || saved.Nickname != "Bob" {
		t.Fatalf("saved store: %+v, %v", saved, err)
	}

	var paired string
	for _, d := range alice.m.State().Devices {
		if d.Paired {
			paired = d.ID
		}
	}
	if paired == "" {
		t.Fatal("Bob is not listed as paired")
	}
	id, err := alice.Send(paired, newMemorySource(map[string]string{"report.txt": "quarterly"}), false)
	if err != nil {
		t.Fatal(err)
	}
	offer := bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.Status == "offered" })
	if offer.Quick || offer.DeviceID == "" {
		t.Fatalf("paired offer: %+v", offer)
	}
	if err := bob.Decline(offer.ID); err != nil {
		t.Fatal(err)
	}
	aliceEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == id && x.Status == "declined" })

	var state struct {
		Peers []struct {
			PeerID string `json:"peerId"`
		} `json:"peers"`
	}
	if err := json.Unmarshal([]byte(bob.StateJSON()), &state); err != nil || len(state.Peers) != 1 {
		t.Fatalf("state peers: %+v, %v", state, err)
	}
	if err := bob.Forget(state.Peers[0].PeerID); err != nil {
		t.Fatal(err)
	}
	if len(bob.m.State().Peers) != 0 {
		t.Fatal("pairing still listed after Forget")
	}
}

// Cancelling while the app downloads tells the sender it was the recipient.
func TestRecipientCancelWhileDownloading(t *testing.T) {
	alice, aliceEvents, _ := startNode(t, "Alice")
	bob, bobEvents, _ := startNode(t, "Bob")
	devBob := meet(t, alice, bob)
	id, err := alice.Send(devBob, newMemorySource(map[string]string{"a.txt": "a"}), true)
	if err != nil {
		t.Fatal(err)
	}
	offer := bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.Status == "offered" })
	if err := bob.Accept(offer.ID); err != nil {
		t.Fatal(err)
	}
	bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == offer.ID && x.Status == "ready" })
	if err := bob.Cancel(offer.ID); err != nil {
		t.Fatal(err)
	}
	bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == offer.ID && x.Status == "cancelled" })
	got := aliceEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == id && x.finished() })
	if got.Status != "cancelled" || got.Message != "cancelled by the recipient" {
		t.Fatalf("sender after recipient cancel: %+v", got)
	}
}

// A failed download is reported to the sender with the app's reason.
func TestFailedDownloadIsReported(t *testing.T) {
	alice, aliceEvents, _ := startNode(t, "Alice")
	bob, bobEvents, _ := startNode(t, "Bob")
	devBob := meet(t, alice, bob)
	id, err := alice.Send(devBob, newMemorySource(map[string]string{"a.txt": "a"}), true)
	if err != nil {
		t.Fatal(err)
	}
	offer := bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.Status == "offered" })
	if err := bob.Accept(offer.ID); err != nil {
		t.Fatal(err)
	}
	bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == offer.ID && x.Status == "ready" })
	bob.FinishReceive(offer.ID, false, "storage is full")
	bobEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == offer.ID && x.Status == "failed" })
	got := aliceEvents.waitTransfer(t, func(x transferEvent) bool { return x.ID == id && x.finished() })
	if got.Status != "failed" || !strings.Contains(got.Message, "storage is full") {
		t.Fatalf("sender after failed download: %+v", got)
	}
}

func TestSettingsPersist(t *testing.T) {
	n, _, path := startNode(t, "Pixel 8")
	st := n.m.State()
	if st.Nickname != "Pixel 8" || !st.Visible || !st.QuickReceive {
		t.Fatalf("new device state: %+v", st)
	}
	if err := n.SetNickname("  "); err == nil {
		t.Fatal("an empty name was accepted")
	}
	if err := n.SetNickname("Kitchen tablet"); err != nil {
		t.Fatal(err)
	}
	if err := n.SetVisible(false); err != nil {
		t.Fatal(err)
	}
	if err := n.SetQuickReceive(false); err != nil {
		t.Fatal(err)
	}
	saved, err := nearbystore.LoadWith(path, nearbystore.Options{Key: testKey(1)})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Nickname != "Kitchen tablet" || saved.Visible || !saved.RefuseQuick {
		t.Fatalf("saved settings: %+v", saved)
	}
}
