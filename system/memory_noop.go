//go:build !(linux || windows || darwin || freebsd || openbsd || dragonfly || netbsd)

package system

// GetTotalSystemMemory gets the total system memory in the current system.
func GetTotalSystemMemory() uint64 {
	return 0
}
