//go:build !windows

package secrets

// On Linux/macOS the key file is protected by 0600 permissions in a 0700 directory.
// Server deployments should additionally use disk encryption or a secrets manager.
func protect(data []byte) ([]byte, error)   { return append([]byte(nil), data...), nil }
func unprotect(data []byte) ([]byte, error) { return append([]byte(nil), data...), nil }
