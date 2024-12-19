package system

import "syscall"

// GetTotalSystemMemory gets the total system memory in the current system.
func GetTotalSystemMemory() uint64 {
	sysinfo := &syscall.Sysinfo_t{}
	// Populate sysinfo.
	err := syscall.Sysinfo(sysinfo)
	if err != nil {
		return 0
	}
	return uint64(sysinfo.Totalram) * uint64(sysinfo.Unit)
}
