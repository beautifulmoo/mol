package hostinfo

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info holds host information (CPU, memory, hostname, IP).
type Info struct {
	Hostname             string  `json:"hostname"`
	HostIP               string  `json:"host_ip"`
	CPUInfo              string  `json:"cpu_info"`
	CPUUsagePercent      float64 `json:"cpu_usage_percent"`
	MemoryTotalMB        uint64  `json:"memory_total_mb"`
	MemoryUsedMB         uint64  `json:"memory_used_mb"`
	MemoryUsagePercent   float64 `json:"memory_usage_percent"`
}

// Get returns host info. Linux uses /proc; other OSes return best-effort.
func Get() (Info, error) {
	var h Info
	hostname, _ := os.Hostname()
	h.Hostname = hostname
	h.HostIP = primaryIPv4()
	if runtime.GOOS == "linux" {
		h.CPUInfo, _ = cpuInfoLinux()
		h.CPUUsagePercent, _ = cpuUsagePercentLinux()
		h.MemoryTotalMB, h.MemoryUsedMB, h.MemoryUsagePercent, _ = memoryLinux()
	}
	return h, nil
}

func primaryIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

func cpuInfoLinux() (string, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	var model string
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "model name") {
			if i := strings.Index(line, ":"); i >= 0 {
				model = strings.TrimSpace(line[i+1:])
				break
			}
		}
	}
	if model == "" {
		model = "unknown"
	}
	return model, nil
}

func cpuUsagePercentLinux() (float64, error) {
	parse := func() (total, idle uint64, err error) {
		data, err := os.ReadFile("/proc/stat")
		if err != nil {
			return 0, 0, err
		}
		line := strings.Split(string(data), "\n")[0]
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0, nil
		}
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			total += v
			if i == 4 {
				idle = v
			}
		}
		return total, idle, nil
	}
	t0, i0, err := parse()
	if err != nil {
		return 0, err
	}
	time.Sleep(200 * time.Millisecond)
	t1, i1, err := parse()
	if err != nil {
		return 0, err
	}
	dt := t1 - t0
	di := i1 - i0
	if dt == 0 {
		return 0, nil
	}
	return 100 * (1 - float64(di)/float64(dt)), nil
}

func memoryLinux() (totalMB, usedMB uint64, usagePercent float64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, err
	}
	var memTotal, memAvailable uint64
	s := bufio.NewScanner(strings.NewReader(string(data)))
	for s.Scan() {
		line := s.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			memTotal = val / 1024
		case "MemAvailable:":
			memAvailable = val / 1024
		}
	}
	if memTotal == 0 {
		return 0, 0, 0, nil
	}
	usedMB = memTotal - memAvailable
	usagePercent = 100 * float64(usedMB) / float64(memTotal)
	return memTotal, usedMB, usagePercent, nil
}
