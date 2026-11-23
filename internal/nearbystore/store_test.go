package nearbystore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/threatexpert/gonc/v2/nearby"
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
