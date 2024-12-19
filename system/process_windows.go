package system

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// ProcessStats is statistics of a process.
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

// GetProcessStats gets the stats of a process.
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

	// Get CPU info.
	creationTime1 := &syscall.Filetime{}
	exitTime1 := &syscall.Filetime{}
	kernelTime1 := &syscall.Filetime{}
	userTime1 := &syscall.Filetime{}
	err = syscall.GetProcessTimes(process, creationTime1, exitTime1, kernelTime1, userTime1)
	if err != nil {
		return ProcessStats{RSSMemory: float64(memoryInfo.WorkingSetSize)}, nil
	}
	<-time.After(time.Millisecond * 50) // Not the most accurate, but it'll do.
	creationTime2 := &syscall.Filetime{}
	exitTime2 := &syscall.Filetime{}
	kernelTime2 := &syscall.Filetime{}
	userTime2 := &syscall.Filetime{}
	err = syscall.GetProcessTimes(process, creationTime2, exitTime2, kernelTime2, userTime2)
	if err != nil {
		return ProcessStats{RSSMemory: float64(memoryInfo.WorkingSetSize)}, nil
	}
	cpuTime := float64((userTime2.Nanoseconds() - userTime1.Nanoseconds()) / int64(runtime.NumCPU()))

	return ProcessStats{
		RSSMemory: float64(memoryInfo.WorkingSetSize),
		CPUUsage:  cpuTime / 500000, // Conversion: (cpuTime / (50*1000*1000)) * 100
	}, nil
}
