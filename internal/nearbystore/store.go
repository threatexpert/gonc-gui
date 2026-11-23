// Package nearbystore keeps this installation's Nearby identity, settings
// and saved pairings. Pairing keys are secrets, so the file is encrypted with
// the operating system's user-scoped protection where available.
package nearbystore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/threatexpert/gonc/v2/nearby"
)

// PathEnv overrides the store location. It exists so two copies of the app
// can run side by side on one computer for testing.
const PathEnv = "GONC_NEARBY_STORE"

type Store struct {
	Version        int    `json:"version"`
	InstallationID string `json:"installationId"`
	IRK            []byte `json:"irk"`
	Nickname       string `json:"nickname"`
	Visible        bool   `json:"visible"`
	SaveDir        string `json:"saveDir,omitempty"`
	// RefuseQuick turns off offers from unpaired devices. The zero value
	// allows them; each offer is still shown to the user.
	RefuseQuick bool `json:"refuseQuick,omitempty"`
	// Addresses are host:port entries the user added by hand.
	Addresses []string            `json:"addresses,omitempty"`
	Peers     []nearby.PeerRecord `json:"peers"`
}

// Identity returns the nearby.Identity described by the store.
func (s Store) Identity() nearby.Identity {
	return nearby.Identity{InstallationID: s.InstallationID, IRK: s.IRK, Nickname: s.Nickname, DeviceType: "desktop"}
}

// UpsertPeer adds a pairing or replaces the one for the same device.
func (s *Store) UpsertPeer(rec nearby.PeerRecord) {
	for i := range s.Peers {
		if s.Peers[i].InstallationID == rec.InstallationID {
			s.Peers[i] = rec
			return
		}
	}
	s.Peers = append(s.Peers, rec)
}

// RemovePeer forgets a pairing. It reports whether one was removed.
func (s *Store) RemovePeer(installationID string) bool {
	for i := range s.Peers {
		if s.Peers[i].InstallationID == installationID {
			s.Peers = append(s.Peers[:i], s.Peers[i+1:]...)
			return true
		}
	}
	return false
}

func defaultNickname() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "Gonc " + runtime.GOOS
	}
	return name
}

// Load reads the store, creating a new identity on first use. A store that
// exists but cannot be decrypted is reported as an error instead of being
// replaced, so a temporary failure never discards saved pairings.
func Load() (Store, error) {
	path, err := FilePath()
	if err != nil {
		return Store{}, err
	}
	return LoadAt(path)
}

// Options adjusts how a store file is read and written.
type Options struct {
	// Key, when set, seals the file with AES-GCM under this 32-byte key
	// instead of the operating system's protection. Mobile apps keep the
	// key in the platform keystore.
	Key []byte
	// DefaultName names a new device instead of the host name.
	DefaultName string
}

func (o Options) protect(data []byte) ([]byte, error) {
	if o.Key != nil {
		return seal(data, o.Key)
	}
	return protect(data)
}

func (o Options) unprotect(data []byte) ([]byte, error) {
	if o.Key != nil {
		return openSealed(data, o.Key)
	}
	return unprotect(data)
}

func (o Options) defaultName() string {
	if name := strings.TrimSpace(o.DefaultName); name != "" {
		return name
	}
	return defaultNickname()
}

// LoadAt is Load for an explicit file path.
func LoadAt(path string) (Store, error) {
	return LoadWith(path, Options{})
}

// LoadWith is LoadAt with options.
func LoadWith(path string, opt Options) (Store, error) {
	if opt.Key != nil && len(opt.Key) != keyBytes {
		return Store{}, errors.New("nearby store key must be 32 bytes")
	}
	encrypted, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return create(path, opt)
	}
	if err != nil {
		return Store{}, err
	}
	data, err := opt.unprotect(encrypted)
	if err != nil {
		return Store{}, err
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return Store{}, err
	}
	if store.InstallationID == "" || len(store.IRK) == 0 {
		return Store{}, errors.New("nearby store is missing its identity")
	}
	if strings.TrimSpace(store.Nickname) == "" {
		store.Nickname = opt.defaultName()
	}
	return store, nil
}

func create(path string, opt Options) (Store, error) {
	id, err := nearby.NewInstallationID()
	if err != nil {
		return Store{}, err
	}
	irk, err := nearby.NewIRK()
	if err != nil {
		return Store{}, err
	}
	store := Store{Version: 1, InstallationID: id, IRK: irk, Nickname: opt.defaultName(), Visible: true}
	return store, SaveWith(path, opt, store)
}

func Save(store Store) error {
	path, err := FilePath()
	if err != nil {
		return err
	}
	return SaveAt(path, store)
}

// SaveAt is Save for an explicit file path.
func SaveAt(path string, store Store) error {
	return SaveWith(path, Options{}, store)
}

// SaveWith is SaveAt with options.
func SaveWith(path string, opt Options, store Store) error {
	store.Version = 1
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	encrypted, err := opt.protect(data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp-" + time.Now().Format("150405.000000000")
	if err := os.WriteFile(tmp, encrypted, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func FilePath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(PathEnv)); override != "" {
		return filepath.Abs(override)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "Gonc", "gonc-gui", "nearby.json.enc"), nil
}
