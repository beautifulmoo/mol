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

const netDir = "/sys/class/net"

// includeInterfaceForDiscovery reports whether iface should be used for BM-style broadcast discovery.
// Rules (aligned with brd_for_bm.sh / PRD §3.1.1): skip lo; sysfs type must be 1 (ARPHRD_ETHER);
// if brif/ exists (bridge master), require at least one slave in brif/ (exclude empty internal bridges).
// No name-based filtering (docker*, veth*, etc.).
func includeInterfaceForDiscovery(name string) bool {
	if name == "lo" {
		return false
	}
	typePath := filepath.Join(netDir, name, "type")
	data, err := os.ReadFile(typePath)
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(data)) != "1" {
		return false
	}
	brifPath := filepath.Join(netDir, name, "brif")
	if fi, err := os.Stat(brifPath); err == nil && fi.IsDir() {
		ents, err := os.ReadDir(brifPath)
		if err != nil || len(ents) == 0 {
			return false
		}
	}
	return true
}

// getInterfaceBrdPairs runs "ip -o -4 addr show" per included interface and returns (iface, brd) pairs.
// Duplicate brd on the same interface is collapsed; the same brd on different interfaces appears as multiple pairs.
func getInterfaceBrdPairs() []NicBrdPair {
	if runtime.GOOS != "linux" {
		return nil
	}
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil
	}
	var pairs []NicBrdPair
	ipCmd := ipPath()
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}
		if !includeInterfaceForDiscovery(name) {
			continue
		}
		cmd := exec.Command(ipCmd, "-o", "-4", "addr", "show", "dev", name)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		seenBrdOnIface := make(map[string]struct{})
		for _, line := range lines {
			if line == "" || !strings.Contains(line, "brd") {
				continue
			}
			fields := strings.Fields(line)
			for i := 0; i < len(fields)-1; i++ {
				if fields[i] != "brd" {
					continue
				}
				brd := strings.TrimSpace(fields[i+1])
				if brd == "" || brd == "0.0.0.0" {
					break
				}
				if _, dup := seenBrdOnIface[brd]; dup {
					break
				}
				seenBrdOnIface[brd] = struct{}{}
				pairs = append(pairs, NicBrdPair{Iface: name, Brd: brd})
				break
			}
		}
	}
	return pairs
}

// GetPhysicalNICBroadcastAddresses returns IPv4 broadcast addresses for discovery sends.
// Deduplicates by brd string only (same subnet from multiple NICs → one send per address).
func GetPhysicalNICBroadcastAddresses() []string {
	pairs := getInterfaceBrdPairs()
	seen := make(map[string]struct{})
	var addrs []string
	for _, p := range pairs {
		if _, ok := seen[p.Brd]; !ok {
			seen[p.Brd] = struct{}{}
			addrs = append(addrs, p.Brd)
		}
	}
	return addrs
}

// NicBrdPair is an interface name and one of its IPv4 broadcast addresses.
type NicBrdPair struct {
	Iface string
	Brd   string
}

// GetPhysicalNICBrdPairs returns (interface name, brd) for each distinct brd per interface.
// For CLI: contrabass-moleU agent --nic-brd (same rules as automatic discovery brd collection).
func GetPhysicalNICBrdPairs() []NicBrdPair {
	return getInterfaceBrdPairs()
}
