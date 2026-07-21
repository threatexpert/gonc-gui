//go:build !windows

package sharecontent

func (m *Manager) ImportNativeClipboard() (ClipboardResult, error) {
	return ClipboardResult{}, ErrClipboardUnsupported
}
