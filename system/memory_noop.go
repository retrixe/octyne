// +build !linux,!windows,!darwin,!freebsd,!openbsd,!dragonfly,!netbsd

package system

// GetTotalSystemMemory ...  Get the total system memory in the current system.
func GetTotalSystemMemory() uint64 {
	return 0
}
