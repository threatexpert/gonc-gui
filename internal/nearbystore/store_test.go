package nearbystore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"gonc-gui/internal/nearby"
)

func TestLoadCreatesIdentityAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nearby.json.enc")
	t.Setenv(PathEnv, path)

	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.InstallationID) != 32 || len(first.IRK) != 16 || !first.Visible || first.Nickname == "" {
		t.Fatalf("unexpected new store %+v", first)
	}
	first.UpsertPeer(nearby.PeerRecord{InstallationID: "aa", PairKey: []byte("secret-pair-key")})
	if err := Save(first); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if Protected && bytes.Contains(raw, []byte("secret-pair-key")) {
		t.Fatal("pairing key stored in plain text although protection is reported")
	}

	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if second.InstallationID != first.InstallationID || !bytes.Equal(second.IRK, first.IRK) || len(second.Peers) != 1 {
		t.Fatalf("store did not round-trip: %+v", second)
	}
	if !second.RemovePeer("aa") || second.RemovePeer("aa") {
		t.Fatal("RemovePeer did not remove exactly once")
	}
}

func TestLoadRefusesUnreadableStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nearby.json.enc")
	t.Setenv(PathEnv, path)
	if err := os.WriteFile(path, []byte("not a valid store"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load replaced an unreadable store instead of reporting it")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "not a valid store" {
		t.Fatal("Load overwrote an unreadable store")
	}
}

// A key-sealed store hides pairing keys, needs the same key to open, and
// names a new device with the given default name.
func TestKeySealedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nearby.bin")
	key := bytes.Repeat([]byte{7}, 32)
	opt := Options{Key: key, DefaultName: "Pixel 8"}
	first, err := LoadWith(path, opt)
	if err != nil {
		t.Fatal(err)
	}
	if first.Nickname != "Pixel 8" {
		t.Fatalf("new device name %q", first.Nickname)
	}
	first.UpsertPeer(nearby.PeerRecord{InstallationID: "aa", PairKey: []byte("secret-pair-key")})
	if err := SaveWith(path, opt, first); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-pair-key")) || bytes.Contains(raw, []byte("Pixel 8")) {
		t.Fatal("sealed store contains plain text")
	}
	second, err := LoadWith(path, opt)
	if err != nil || second.InstallationID != first.InstallationID || len(second.Peers) != 1 {
		t.Fatalf("sealed store did not round-trip: %+v, %v", second, err)
	}
	if _, err := LoadWith(path, Options{Key: bytes.Repeat([]byte{8}, 32)}); err == nil {
		t.Fatal("opened with the wrong key")
	}
	if _, err := LoadWith(path, Options{Key: key[:16]}); err == nil {
		t.Fatal("accepted a short key")
	}
	if raw2, _ := os.ReadFile(path); !bytes.Equal(raw, raw2) {
		t.Fatal("a failed open changed the file")
	}
}
