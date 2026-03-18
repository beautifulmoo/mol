package hostinfo

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Info holds host information (CPU, memory, hostname, IP).
type Info struct {
	Hostname             string   `json:"hostname"`
	HostIP               string   `json:"host_ip"`
	HostIPs              []string `json:"host_ips,omitempty"` // optional; for self, all IPs this host responds with in discovery
	CPUInfo              string   `json:"cpu_info"`
	CPUUsagePercent      float64 `json:"cpu_usage_percent"`
	CPUUUID              string  `json:"cpu_uuid"`
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
		h.CPUUUID, _ = cpuUUIDLinux()
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

// cpuUUIDLinux returns a stable host identifier for discovery/UI.
// Order: /proc/cpuinfo Serial (if meaningful) → dmidecode → sysfs product_uuid → /etc/machine-id.
// Minimal installs often lack dmidecode; machine-id exists on typical systemd-based Ubuntu.
func cpuUUIDLinux() (string, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err == nil {
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := s.Text()
			if strings.HasPrefix(line, "Serial") {
				if i := strings.Index(line, ":"); i >= 0 {
					v := strings.TrimSpace(line[i+1:])
					if !uselessHostID(v) {
						_ = f.Close()
						return v, nil
					}
				}
			}
		}
		_ = f.Close()
	}

	if v := runDmidecodeUUID(); v != "" {
		return v, nil
	}
	if v := readTrimmedFile("/sys/class/dmi/id/product_uuid"); v != "" && !uselessHostID(v) {
		return v, nil
	}
	if v := readTrimmedFile("/etc/machine-id"); v != "" {
		return v, nil
	}
	// dbus machine-id (non-systemd rare; often duplicate of /etc/machine-id)
	if v := readTrimmedFile("/var/lib/dbus/machine-id"); v != "" {
		return v, nil
	}
	return "", nil
}

func uselessHostID(s string) bool {
	s = strings.TrimSpace(strings.Trim(s, "\x00"))
	if s == "" {
		return true
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "not set") || strings.Contains(low, "not available") || strings.Contains(low, "to be filled") {
		return true
	}
	if low == "0" {
		return true
	}
	// e.g. 00000000-0000-0000-0000-000000000000
	alnum := make([]rune, 0, len(low))
	for _, r := range low {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			alnum = append(alnum, r)
		}
	}
	if len(alnum) >= 8 {
		for _, r := range alnum {
			if r != '0' {
				return false
			}
		}
		return true
	}
	return false
}

func readTrimmedFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func runDmidecodeUUID() string {
	cmd := exec.Command("dmidecode", "-s", "system-uuid")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if uselessHostID(v) {
		return ""
	}
	return v
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
