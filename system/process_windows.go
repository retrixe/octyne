// +build windows

package system

import (
	"syscall"
	"unsafe"
)

// ProcessStats ... Statistics of a process.
type ProcessStats struct {
	CPUUsage  float64
	RSSMemory float64
}

const processQueryLimitedInformation = 0x1000

type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uint64
	WorkingSetSize             uint64
	QuotaPeakPagedPoolUsage    uint64
	QuotaPagedPoolUsage        uint64
	QuotaPeakNonPagedPoolUsage uint64
	QuotaNonPagedPoolUsage     uint64
	PagefileUsage              uint64
	PeakPagefileUsage          uint64
}

// GetProcessStats ... Get the stats of a process.
func GetProcessStats(pid int) (ProcessStats, error) {
	// Open the process.
	process, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return ProcessStats{}, nil
	}
	defer syscall.CloseHandle(process)

	// Get memory info.
	psapi := syscall.NewLazyDLL("psapi.dll")
	getProcessMemoryInfo := psapi.NewProc("GetProcessMemoryInfo")
	memoryInfo := processMemoryCounters{
		cb: 72,
	}
	res, _, _ := getProcessMemoryInfo.Call(uintptr(process), uintptr(unsafe.Pointer(&memoryInfo)), uintptr(memoryInfo.cb))
	if res == 0 {
		return ProcessStats{}, nil
	}

	return ProcessStats{
		RSSMemory: float64(memoryInfo.WorkingSetSize),
		CPUUsage:  0, // TODO: Fix CPU usage on Windows.
	}, nil
}
