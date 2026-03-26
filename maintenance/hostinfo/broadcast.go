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

// shouldExcludeByName returns true if the interface name is in the virtual/excluded list
// (docker*, veth*, virbr*, br-int, br-tun, cni*, flannel*, vxlan_sys*, genev_sys*).
func shouldExcludeByName(name string) bool {
	switch {
	case name == "lo":
		return true
	case strings.HasPrefix(name, "docker"):
		return true
	case strings.HasPrefix(name, "veth"):
		return true
	case strings.HasPrefix(name, "virbr"):
		return true
	case name == "br-int" || strings.HasPrefix(name, "br-int"):
		return true
	case name == "br-tun" || strings.HasPrefix(name, "br-tun"):
		return true
	case strings.HasPrefix(name, "cni"):
		return true
	case strings.HasPrefix(name, "flannel"):
		return true
	case strings.HasPrefix(name, "vxlan_sys"):
		return true
	case strings.HasPrefix(name, "genev_sys"):
		return true
	}
	return false
}

// isVirtualButAllowed returns true if the interface is under /virtual/ but we keep it
// (bond*, br*, vlan*, eth*, en* — bonding, bridge, vlan, physical-like names).
func isVirtualButAllowed(name string) bool {
	return strings.HasPrefix(name, "bond") ||
		strings.HasPrefix(name, "br") ||
		strings.HasPrefix(name, "vlan") ||
		strings.HasPrefix(name, "eth") ||
		strings.HasPrefix(name, "en")
}

// includeInterface returns true if the interface should be used for broadcast discovery.
// Excludes: lo, down interfaces, excluded names, and /virtual/ interfaces except bond/br/vlan/eth/en.
func includeInterface(name string) bool {
	if shouldExcludeByName(name) {
		return false
	}
	operstatePath := filepath.Join(netDir, name, "operstate")
	if data, err := os.ReadFile(operstatePath); err == nil {
		if strings.TrimSpace(string(data)) != "up" {
			return false
		}
	}
	linkPath := filepath.Join(netDir, name)
	target, err := os.Readlink(linkPath)
	if err != nil {
		return true // e.g. not a symlink, allow and let ip output decide
	}
	if strings.Contains(target, "/virtual/") {
		return isVirtualButAllowed(name)
	}
	return true
}

// getInterfaceBrdPairs runs "ip -o -4 addr show" for each included interface and returns (iface, brd) pairs.
// Includes bonding (bond*), bridge (br*), vlan (vlan*), eth*, en* and physical NICs; excludes lo, down, docker*, veth*, etc.
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
		if !includeInterface(name) {
			continue
		}
		cmd := exec.Command(ipCmd, "-o", "-4", "addr", "show", name)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			if !strings.Contains(line, "brd") {
				continue
			}
			fields := strings.Fields(line)
			for i := 0; i < len(fields)-1; i++ {
				if fields[i] == "brd" {
					brd := strings.TrimSpace(fields[i+1])
					if brd != "" && brd != "0.0.0.0" {
						pairs = append(pairs, NicBrdPair{Iface: name, Brd: brd})
					}
					break
				}
			}
		}
	}
	return pairs
}

// GetPhysicalNICBroadcastAddresses returns IPv4 broadcast addresses for discovery.
// Includes physical NICs and bonding/bridge/vlan (bond*, br*, vlan*, eth*, en*) that are UP and have brd.
// Excludes lo, down interfaces, docker*, veth*, virbr*, br-int, br-tun, cni*, flannel*, vxlan_sys*, genev_sys*.
// Duplicate brd addresses are deduplicated.
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
	Brd  string
}

// GetPhysicalNICBrdPairs returns (interface name, brd address) for each included interface's IPv4 brd.
// Same inclusion rules as GetPhysicalNICBroadcastAddresses (bonding, bridge, vlan, physical; excludes virtual-only).
// For CLI: mol --nic-brd.
func GetPhysicalNICBrdPairs() []NicBrdPair {
	return getInterfaceBrdPairs()
}
