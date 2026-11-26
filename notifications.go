package main

// SetupNotifications prepares system notifications and reports whether they
// can be shown. On macOS it asks for permission the first time.
func (a *App) SetupNotifications() bool {
	return setupNotifications(a.ctx)
}

// ShowNotification shows a system notification with a title and a body.
func (a *App) ShowNotification(title, body string) error {
	return showNotification(a.ctx, title, body)
}
