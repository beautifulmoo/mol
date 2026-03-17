package hostinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ipPath returns the path to the "ip" command (prefer full path so it works when PATH is minimal).
func ipPath() string {
	if path, err := exec.LookPath("ip"); err == nil {
		return path
	}
	for _, p := range []string{"/usr/sbin/ip", "/sbin/ip"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "ip"
}

// GetPhysicalNICBroadcastAddresses returns IPv4 broadcast addresses of physical NICs.
// Physical NICs are those with /sys/class/net/<iface>/device. For each, "ip -o -4 addr show"
// is run and "brd" addresses are collected (supports multiple IPs per interface).
// Duplicate brd addresses are deduplicated; each appears only once in the result.
// Returns nil or empty on non-Linux or when no addresses are found.
func GetPhysicalNICBroadcastAddresses() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	netDir := "/sys/class/net"
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil
	}
	var ifaces []string
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		// Physical NIC: has .../device (skip virtual like lo, docker0; /sys/class/net/* are often symlinks so don't rely on IsDir())
		devicePath := filepath.Join(netDir, name, "device")
		if _, err := os.Stat(devicePath); err != nil {
			continue
		}
		ifaces = append(ifaces, name)
	}
	seen := make(map[string]struct{}) // 중복 brd 제거, 한 개만 유지
	var addrs []string
	ipCmd := ipPath()
	for _, iface := range ifaces {
		cmd := exec.Command(ipCmd, "-o", "-4", "addr", "show", iface)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			for i := 0; i < len(fields)-1; i++ {
				if fields[i] == "brd" {
					brd := strings.TrimSpace(fields[i+1])
					if brd != "" && brd != "0.0.0.0" {
						if _, ok := seen[brd]; !ok {
							seen[brd] = struct{}{}
							addrs = append(addrs, brd)
						}
					}
					break
				}
			}
		}
	}
	return addrs
}

// NicBrdPair is a physical NIC name and one of its IPv4 broadcast addresses.
type NicBrdPair struct {
	Iface string
	Brd  string
}

// GetPhysicalNICBrdPairs returns (interface name, brd address) for each physical NIC's IPv4 brd.
// Same logic as GetPhysicalNICBroadcastAddresses but keeps iface name per brd (one line per inet/brd, like ip output).
// For CLI: mol --nic-brd.
func GetPhysicalNICBrdPairs() []NicBrdPair {
	if runtime.GOOS != "linux" {
		return nil
	}
	netDir := "/sys/class/net"
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil
	}
	var ifaces []string
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		devicePath := filepath.Join(netDir, name, "device")
		if _, err := os.Stat(devicePath); err != nil {
			continue
		}
		ifaces = append(ifaces, name)
	}
	var pairs []NicBrdPair
	ipCmd := ipPath()
	for _, iface := range ifaces {
		cmd := exec.Command(ipCmd, "-o", "-4", "addr", "show", iface)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			for i := 0; i < len(fields)-1; i++ {
				if fields[i] == "brd" {
					brd := strings.TrimSpace(fields[i+1])
					if brd != "" && brd != "0.0.0.0" {
						pairs = append(pairs, NicBrdPair{Iface: iface, Brd: brd})
					}
					break
				}
			}
		}
	}
	return pairs
}
