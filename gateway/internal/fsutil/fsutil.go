// Package fsutil creates directories and files readable only by the current user.
package fsutil

import (
	"os"
	"path/filepath"
)

// EnsurePrivateDir creates dir (if needed) and restricts it to the current user,
// SYSTEM and Administrators on Windows, or mode 0700 elsewhere.
func EnsurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return restrict(dir, true)
}

// WritePrivateFile atomically writes data to path with owner-only permissions.
func WritePrivateFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	return restrict(path, false)
}
