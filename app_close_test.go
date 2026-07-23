package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestPreventCloseForStatusSkipsDialogWhenNoModuleRuns(t *testing.T) {
	called := false
	prevent := preventCloseForStatus(context.Background(), AppStatus{}, "zh", func(context.Context, wailsruntime.MessageDialogOptions) (string, error) {
		called = true
		return "Yes", nil
	})
	if prevent || called {
		t.Fatalf("prevent=%v called=%v, want direct close", prevent, called)
	}
}

func TestPreventCloseForStatusListsOnlyRunningModules(t *testing.T) {
	status := AppStatus{
		SendRunning:      true,
		ReceiveRunning:   true,
		VPNServerRunning: true,
		VPNClientRunning: true,
		Downloading:      true,
	}
	var options wailsruntime.MessageDialogOptions
	prevent := preventCloseForStatus(context.Background(), status, "zh", func(_ context.Context, current wailsruntime.MessageDialogOptions) (string, error) {
		options = current
		return "No", nil
	})
	if !prevent {
		t.Fatal("cancel must prevent close")
	}
	for _, name := range []string{"文件发送", "文件接收", "VPN 服务端", "VPN 客户端"} {
		if !strings.Contains(options.Message, name) {
			t.Fatalf("message %q does not list %q", options.Message, name)
		}
	}
	if strings.Contains(options.Message, "下载") {
		t.Fatalf("message %q must not list internal download work", options.Message)
	}
	if options.Type != wailsruntime.QuestionDialog || options.DefaultButton != "No" {
		t.Fatalf("options = %+v, want question with cancel default", options)
	}
}

func TestPreventCloseForStatusAllowsOnlyExplicitConfirmation(t *testing.T) {
	status := AppStatus{VPNServerRunning: true}
	tests := []struct {
		name    string
		answer  string
		err     error
		prevent bool
	}{
		{name: "yes", answer: "Yes", prevent: false},
		{name: "cancel", answer: "No", prevent: true},
		{name: "dismissed", answer: "", prevent: true},
		{name: "dialog error", err: errors.New("dialog failed"), prevent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prevent := preventCloseForStatus(context.Background(), status, "en", func(context.Context, wailsruntime.MessageDialogOptions) (string, error) {
				return tt.answer, tt.err
			})
			if prevent != tt.prevent {
				t.Fatalf("prevent=%v, want %v", prevent, tt.prevent)
			}
		})
	}
}

func TestSetUILanguageNormalizesToSupportedLanguages(t *testing.T) {
	app := NewApp(nil)
	app.SetUILanguage("zh")
	if got := app.currentUILanguage(); got != "zh" {
		t.Fatalf("language=%q, want zh", got)
	}
	app.SetUILanguage("fr")
	if got := app.currentUILanguage(); got != "en" {
		t.Fatalf("language=%q, want en fallback", got)
	}
}
