//go:build !windows

package system

// IsFileLocked checks if a file is locked.
func IsFileLocked(_ error) bool {
	// We could look for advisory locks with `lsof` but this is generally not what Unix users expect...
	return false
}
