//go:build !windows

package main

import (
	"context"
	"fmt"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// macOS and Linux use Wails' notifications: the user notification center on
// macOS, org.freedesktop.Notifications over D-Bus on Linux.

func setupNotifications(ctx context.Context) bool {
	if err := wailsruntime.InitializeNotifications(ctx); err != nil {
		return false
	}
	if !wailsruntime.IsNotificationAvailable(ctx) {
		return false
	}
	allowed, err := wailsruntime.RequestNotificationAuthorization(ctx)
	if err != nil {
		// Linux has nothing to ask; a failed request on macOS still lets the
		// system decide when a notification is sent.
		return true
	}
	return allowed
}

func showNotification(ctx context.Context, title, body string) error {
	return wailsruntime.SendNotification(ctx, wailsruntime.NotificationOptions{
		ID:    fmt.Sprintf("gonc-gui-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
	})
}
