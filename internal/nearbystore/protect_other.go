//go:build !windows

package nearbystore

// Protected reports whether pairing keys are encrypted by the operating
// system. On this platform they are stored in a file readable only by the
// current user (mode 0600); Keychain and Secret Service support is pending.
const Protected = false

func protect(data []byte) ([]byte, error) { return append([]byte(nil), data...), nil }

func unprotect(data []byte) ([]byte, error) { return append([]byte(nil), data...), nil }
