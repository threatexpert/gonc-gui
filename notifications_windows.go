//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	toast "git.sr.ht/~jackmordaunt/go-toast/v2"
	"git.sr.ht/~jackmordaunt/go-toast/v2/tmpl"
	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
	"golang.org/x/sys/windows/registry"
)

// Windows notifications are toasts sent through the WinRT API under gonc-gui's
// own AppUserModelID.
//
// Wails' notification setup also registers the exe as a COM server
// (HKCU\Software\Classes\CLSID\{random GUID}\LocalServer32) on every launch, so
// that a click on a toast can start the app. gonc-gui's notifications only show
// text, so it registers just the name and icon Windows shows on the toast, and
// only when the first notification is sent.

const (
	notificationAppID       = desktopID
	notificationDisplayName = "Gonc Transfer"
	notificationAppKeyRoot  = `Software\Classes\AppUserModelId\`
)

var notificationApp struct {
	sync.Mutex
	registered bool
}

func setupNotifications(context.Context) bool {
	return true
}

func showNotification(_ context.Context, title, body string) error {
	if err := ensureNotificationApp(); err != nil {
		return err
	}
	xml, err := toastXML(title, body)
	if err != nil {
		return err
	}
	return wintoast.Push(notificationAppID, xml)
}

func ensureNotificationApp() error {
	notificationApp.Lock()
	defer notificationApp.Unlock()
	if notificationApp.registered {
		return nil
	}
	if err := registerNotificationApp(notificationAppID, notificationIconDir()); err != nil {
		return err
	}
	notificationApp.registered = true
	return nil
}

// registerNotificationApp records the name and icon Windows shows on the app's
// toasts. A toast whose AppUserModelID has no CustomActivator is dismissed when
// clicked instead of starting the app.
func registerNotificationApp(appID, iconDir string) error {
	icon := ""
	if iconDir != "" {
		path, err := writeNotificationIcon(iconDir)
		if err != nil {
			// Windows shows a generic icon instead.
			fmt.Fprintln(os.Stderr, "Save notification icon:", err)
		} else {
			icon = path
		}
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, notificationAppKeyRoot+appID, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("register notification app: %w", err)
	}
	defer key.Close()
	if err := setRegistryString(key, "DisplayName", notificationDisplayName); err != nil {
		return fmt.Errorf("register notification app: %w", err)
	}
	if icon != "" {
		if err := setRegistryString(key, "IconUri", icon); err != nil {
			return fmt.Errorf("register notification app: %w", err)
		}
	}
	return nil
}

// setRegistryString writes a value only when it differs from what is stored.
func setRegistryString(key registry.Key, name, value string) error {
	if old, _, err := key.GetStringValue(name); err == nil && old == value {
		return nil
	}
	return key.SetStringValue(name, value)
}

// notificationIconDir is gonc-gui's folder under %APPDATA%, or "" when there
// is none.
func notificationIconDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "Gonc", "gonc-gui")
}

// writeNotificationIcon saves the app icon as a PNG in dir, rewriting it only
// when it changed.
func writeNotificationIcon(dir string) (string, error) {
	path := filepath.Join(dir, "notification-icon.png")
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, desktopIcon) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, desktopIcon, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func toastXML(title, body string) (string, error) {
	n := toast.Notification{
		Title:          title,
		Body:           body,
		ActivationType: toast.Foreground,
		Duration:       toast.Short,
		Audio:          toast.Default,
	}
	var xml bytes.Buffer
	if err := tmpl.XMLTemplate.Execute(&xml, &n); err != nil {
		return "", fmt.Errorf("build notification: %w", err)
	}
	return xml.String(), nil
}
