//go:build !windows

package fsutil

import "os"

func restrict(path string, isDir bool) error {
	if isDir {
		return os.Chmod(path, 0o700)
	}
	return os.Chmod(path, 0o600)
}
