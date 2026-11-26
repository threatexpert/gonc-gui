package main

import (
	"context"
	"errors"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"gonc-gui/internal/goncrunner"
	"runtime"
	"strings"
	"testing"
)

func TestBypassOverridesRequireExplicitConfirmation(t *testing.T) {
	plan := goncrunner.ProtectionPlan{DNS: "9.9.9.9:53", Local: "192.168.50.2:0", Overrides: []string{"-dns 9.9.9.9:53", "-local 192.168.50.2:0"}}
	for _, lang := range []string{"zh", "en"} {
		dialogType, yes, no := wailsruntime.QuestionDialog, "Yes", "No"
		if runtime.GOOS == "darwin" {
			dialogType, yes, no = wailsruntime.WarningDialog, "Start VPN", "Cancel"
			if lang == "zh" {
				yes, no = "继续启用 VPN", "取消"
			}
		}
		for _, action := range []string{"confirm", "cancel", "dismiss", "ok", "error"} {
			got, err := confirmVPNBypassOverrides(context.Background(), plan, lang, func(_ context.Context, o wailsruntime.MessageDialogOptions) (string, error) {
				if o.Type != dialogType || len(o.Buttons) != 2 || o.Buttons[0] != yes || o.Buttons[1] != no || o.DefaultButton != no || o.CancelButton != no {
					t.Fatalf("%s: unexpected dialog options: %+v", lang, o)
				}
				if !strings.Contains(o.Message, plan.DNS) || !strings.Contains(o.Message, plan.Local) {
					t.Fatalf("%s: missing effective settings: %s", lang, o.Message)
				}
				switch action {
				case "confirm":
					return yes, nil
				case "cancel":
					return no, nil
				case "ok":
					return "Ok", nil
				case "error":
					return yes, errors.New("dialog unavailable")
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
