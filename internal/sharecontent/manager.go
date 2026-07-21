package sharecontent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const generatedDirectory = "gonc-gui-share"

type Manager struct {
	mu            sync.Mutex
	root          string
	owned         map[string]struct{}
	exclusiveRoot bool
}

func NewManager() *Manager {
	return &Manager{
		owned:         make(map[string]struct{}),
		exclusiveRoot: true,
	}
}

func newManagerAt(root string) *Manager {
	resolved, err := filepath.Abs(root)
	if err == nil {
		root = resolved
	}
	return &Manager{
		root:  filepath.Clean(root),
		owned: make(map[string]struct{}),
	}
}

func (m *Manager) CreateText(prefix, text string) (string, error) {
	return m.create(prefix, ".txt", func(file *os.File) error {
		_, err := io.WriteString(file, text)
		return err
	})
}

func (m *Manager) CreatePNG(prefix string, image image.Image) (string, error) {
	return m.create(prefix, ".png", func(file *os.File) error {
		return png.Encode(file, image)
	})
}

func (m *Manager) create(prefix, extension string, write func(*os.File) error) (string, error) {
	if prefix == "" || prefix == "." || filepath.Base(prefix) != prefix {
		return "", fmt.Errorf("invalid generated file prefix %q", prefix)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.ensureRoot(); err != nil {
		return "", fmt.Errorf("create generated file directory: %w", err)
	}

	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate file name: %w", err)
	}
	name := fmt.Sprintf("%s-%s-%s%s", prefix, time.Now().Format("20060102-150405.000"), hex.EncodeToString(random), extension)
	path := filepath.Join(m.root, name)
	if !m.contains(path) {
		return "", fmt.Errorf("generated file path escapes root: %q", path)
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create generated file: %w", err)
	}
	if err := write(file); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write generated file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close generated file: %w", err)
	}

	m.owned[path] = struct{}{}
	return path, nil
}

func (m *Manager) Release(paths []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for _, path := range paths {
		resolved, err := filepath.Abs(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("resolve generated file path %q: %w", path, err))
			continue
		}
		resolved = filepath.Clean(resolved)
		if _, ok := m.owned[resolved]; !ok || !m.contains(resolved) {
			continue
		}
		if err := os.Remove(resolved); err != nil {
			errs = append(errs, fmt.Errorf("remove generated file %q: %w", resolved, err))
			continue
		}
		delete(m.owned, resolved)
	}
	return errors.Join(errs...)
}

func (m *Manager) Cleanup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.exclusiveRoot {
		if m.root == "" {
			return nil
		}
		if err := os.RemoveAll(m.root); err != nil {
			return fmt.Errorf("remove generated file directory: %w", err)
		}
		clear(m.owned)
		m.root = ""
		return nil
	}

	var errs []error
	for path := range m.owned {
		if !m.contains(path) {
			continue
		}
		if err := os.Remove(path); err != nil {
			errs = append(errs, fmt.Errorf("remove generated file %q: %w", path, err))
			continue
		}
		delete(m.owned, path)
	}
	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	entries, err := os.ReadDir(m.root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect generated file directory: %w", err)
	}
	if len(entries) == 0 {
		if err := os.Remove(m.root); err != nil {
			return fmt.Errorf("remove generated file directory: %w", err)
		}
	}
	return nil
}

func (m *Manager) Owns(path string) bool {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	resolved = filepath.Clean(resolved)

	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.owned[resolved]
	return ok && m.contains(resolved)
}

func (m *Manager) contains(path string) bool {
	relative, err := filepath.Rel(m.root, path)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (m *Manager) ensureRoot() error {
	if !m.exclusiveRoot {
		return os.MkdirAll(m.root, 0700)
	}
	if m.root != "" {
		return nil
	}

	root, err := os.MkdirTemp("", generatedDirectory+"-")
	if err != nil {
		return err
	}
	resolved, err := filepath.Abs(root)
	if err != nil {
		_ = os.RemoveAll(root)
		return err
	}
	m.root = filepath.Clean(resolved)
	return nil
}
