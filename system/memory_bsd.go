// +build darwin freebsd openbsd dragonfly netbsd

package system

import (
	"encoding/binary"
	"runtime"
	"syscall"
)

// GetTotalSystemMemory ...  Get the total system memory in the current system.
func GetTotalSystemMemory() uint64 {
	name := "hw.memsize"
	if runtime.GOOS != "darwin" {
		name = "hw.physmem"
	}
	s, err := syscall.Sysctl(name)
	if err != nil {
		return 0
	}
	s += "\x00"
	return uint64(binary.LittleEndian.Uint64([]byte(s)))
}
