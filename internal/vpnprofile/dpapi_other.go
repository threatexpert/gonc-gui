//go:build !windows

package vpnprofile

func protect(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func unprotect(data []byte) ([]byte, error) {
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}
