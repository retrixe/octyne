// +build windows

package system

import (
	"syscall"
	"unsafe"
)

// https://docs.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-globalmemorystatusex
type memoryStatusEx struct {
	dwLength     uint32
	dwMemoryLoad uint32
	ullTotalPhys uint64
	unused       [6]uint64 // Contains omitted fields.
}

// GetTotalSystemMemory gets the total system memory in the current system.
func GetTotalSystemMemory() uint64 {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	// GetPhysicallyInstalledSystemMemory returns physically installed, not available, RAM.
	// It is also not available on older versions of Windows (however this is not supported).
	globalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")
	msx := &memoryStatusEx{
		dwLength: 64,
	}
	res, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(msx)))
	if res == 0 {
		return 0
	}
	return msx.ullTotalPhys
}
