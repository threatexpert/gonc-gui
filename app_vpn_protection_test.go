package main

import (
	"context"
	"errors"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gonc-gui/internal/goncrunner"
	"strings"
	"testing"
)

func TestBypassOverridesRequireExplicitConfirmation(t *testing.T) {
	plan := goncrunner.ProtectionPlan{DNS: "9.9.9.9:53", Local: "192.168.50.2:0", Overrides: []string{"-dns 9.9.9.9:53", "-local 192.168.50.2:0"}}
	for _, lang := range []string{"zh", "en"} {
		for _, action := range []string{"confirm", "cancel", "dismiss", "error"} {
			got, err := confirmVPNBypassOverrides(context.Background(), plan, lang, func(_ context.Context, o wailsruntime.MessageDialogOptions) (string, error) {
				if !strings.Contains(o.Message, plan.DNS) || !strings.Contains(o.Message, plan.Local) || o.DefaultButton != o.CancelButton || o.Type != wailsruntime.WarningDialog {
					t.Fatal(o)
				}
				switch action {
				case "confirm":
					return o.Buttons[0], nil
				case "cancel":
					return o.CancelButton, nil
				case "error":
					return "", errors.New("dialog unavailable")
				default:
					return "", nil
				}
			})
			if got != (action == "confirm") || (err != nil) != (action == "error") {
				t.Fatalf("%s %s %v %v", lang, action, got, err)
			}
		}
	}
}
