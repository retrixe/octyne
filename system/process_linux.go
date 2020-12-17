// +build linux

package system

import (
	"errors"
	"io/ioutil"
	"math"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"sync"
)

// Extracted from https://github.com/struCoder/pidusage which is licensed under MIT License.

// ProcessStats ... Statistics of a process.
type ProcessStats struct {
	CPUUsage  float64
	RSSMemory float64
}

// Stat will store CPU time struct
type Stat struct {
	utime  float64
	stime  float64
	cutime float64
	cstime float64
	start  float64
	rss    float64
	uptime float64
}

var history map[int]Stat
var historyLock sync.Mutex
var eol string

func formatStdOut(stdout []byte, userfulIndex int) []string {
	infoArr := strings.Split(string(stdout), eol)[userfulIndex]
	ret := strings.Fields(infoArr)
	return ret
}

func parseFloat(val string) float64 {
	floatVal, _ := strconv.ParseFloat(val, 64)
	return floatVal
}

// GetProcessStats ... Get the stats of a process.
func GetProcessStats(pid int) (ProcessStats, error) {
	processStats := ProcessStats{}
	_history := history[pid]
	// default clkTck and pageSize
	var clkTck float64 = 100
	var pageSize float64 = 4096

	uptimeFileBytes, _ := ioutil.ReadFile(path.Join("/proc", "uptime"))
	uptime := parseFloat(strings.Split(string(uptimeFileBytes), " ")[0])

	clkTckStdout, err := exec.Command("getconf", "CLK_TCK").Output()
	if err == nil {
		clkTck = parseFloat(formatStdOut(clkTckStdout, 0)[0])
	}

	pageSizeStdout, err := exec.Command("getconf", "PAGESIZE").Output()
	if err == nil {
		pageSize = parseFloat(formatStdOut(pageSizeStdout, 0)[0])
	}

	procStatFileBytes, _ := ioutil.ReadFile(path.Join("/proc", strconv.Itoa(pid), "stat"))
	splitAfter := strings.SplitAfter(string(procStatFileBytes), ")")

	if len(splitAfter) == 0 || len(splitAfter) == 1 {
		return processStats, errors.New("Can't find process with this PID: " + strconv.Itoa(pid))
	}
	infos := strings.Split(splitAfter[1], " ")
	stat := &Stat{
		utime:  parseFloat(infos[12]),
		stime:  parseFloat(infos[13]),
		cutime: parseFloat(infos[14]),
		cstime: parseFloat(infos[15]),
		start:  parseFloat(infos[20]) / clkTck,
		rss:    parseFloat(infos[22]),
		uptime: uptime,
	}

	_stime := 0.0
	_utime := 0.0
	if _history.stime != 0 {
		_stime = _history.stime
	}

	if _history.utime != 0 {
		_utime = _history.utime
	}
	total := stat.stime - _stime + stat.utime - _utime
	total = total / clkTck

	seconds := stat.start - uptime
	if _history.uptime != 0 {
		seconds = uptime - _history.uptime
	}

	seconds = math.Abs(seconds)
	if seconds == 0 {
		seconds = 1
	}

	historyLock.Lock()
	history[pid] = *stat
	historyLock.Unlock()
	processStats.CPUUsage = (total / seconds) * 100
	processStats.RSSMemory = stat.rss * pageSize
	return processStats, nil
}
