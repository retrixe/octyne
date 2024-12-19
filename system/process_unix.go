//go:build !(windows || linux)

package system

import (
	"log"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// ProcessStats is statistics of a process.
type ProcessStats struct {
	CPUUsage  float64
	RSSMemory float64
}

// GetProcessStats gets the stats of a process.
func GetProcessStats(pid int) (ProcessStats, error) {
	cmd := "pcpu,rss,cmd"
	if runtime.GOOS == "aix" {
		cmd = "pcpu,rssize,cmd"
	}
	output, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", cmd).Output()
	if err != nil {
		_, ok := err.(*exec.Error)
		if ok {
			log.Println("Octyne requires ps on a non-Linux/Windows OS to return statistics!")
		}
		return ProcessStats{}, err
	}

	usage := strings.Split(strings.Split(string(output), "\n")[1], " ")
	var cpuUsage, rssMemory float64
	cpuFound := false
	for _, stat := range usage {
		if stat != "" && !cpuFound {
			cpuUsage, err = strconv.ParseFloat(stat, 64)
			cpuFound = true
			if err != nil {
				return ProcessStats{}, err
			}
		} else if stat != "" {
			rssMemory, err = strconv.ParseFloat(stat, 64)
			if err != nil {
				return ProcessStats{}, err
			}
			break
		}
	}

	return ProcessStats{
		CPUUsage:  cpuUsage,
		RSSMemory: rssMemory,
	}, nil
}
