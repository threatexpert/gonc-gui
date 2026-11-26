//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestToastXMLHasTitleAndBody(t *testing.T) {
	xml, err := toastXML("VPN is on", "System VPN routes & DNS are ready.")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<![CDATA[VPN is on]]>`,
		`<![CDATA[System VPN routes & DNS are ready.]]>`,
		`activationType="foreground"`,
		`ms-winsoundevent:Notification.Default`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("toast XML lacks %s:\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "<actions>") {
		t.Errorf("toast XML has actions:\n%s", xml)
	}
}

func TestNotificationAppHasNameAndIconButNoClickHandler(t *testing.T) {
	appID := notificationAppID + ".test"
	keyPath := notificationAppKeyRoot + appID
	t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, keyPath) })
	dir := filepath.Join(t.TempDir(), "gonc-gui")

	// Twice: the second run finds everything in place.
	for i := 0; i < 2; i++ {
		if err := registerNotificationApp(appID, dir); err != nil {
			t.Fatal(err)
		}
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	names, err := key.ReadValueNames(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Errorf("values %v, want DisplayName and IconUri only", names)
	}
	if name, _, _ := key.GetStringValue("DisplayName"); name != notificationDisplayName {
		t.Errorf("DisplayName %q", name)
	}
	icon, _, _ := key.GetStringValue("IconUri")
	if icon != filepath.Join(dir, "notification-icon.png") {
		t.Errorf("IconUri %q", icon)
	}
	png, err := os.ReadFile(icon)
	if err != nil || !bytes.Equal(png, desktopIcon) {
		t.Errorf("icon file: %v", err)
	}
}

// GONC_GUI_TOAST_TEST=1 go test -run TestShowNotification shows a real toast.
func TestShowNotification(t *testing.T) {
	if os.Getenv("GONC_GUI_TOAST_TEST") == "" {
		t.Skip("set GONC_GUI_TOAST_TEST=1 to show a toast")
	}
	if err := showNotification(context.Background(), "Gonc Transfer test", "A toast with no click handler."); err != nil {
		t.Fatal(err)
	}
}
