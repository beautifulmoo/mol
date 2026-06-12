package remoteregistry

import (
	"sort"
	"strings"
	"sync"
	"time"

	"contrabass-agent/maintenance/discovery"
)

// Health tracks the latest remote HTTP health-check state for a registry entry.
// Entries are never removed when health fails; Dead only marks consecutive failures.
type Health struct {
	OK                  bool
	ConsecutiveFailures int
	Dead                bool
	LastCheckedAt       time.Time
	LastError           string
}

// Remote is one discovered mole agent kept in volatile server memory.
type Remote struct {
	PrimaryIP         string
	CPUUUID           string
	Hostname          string
	HostIP            string
	HostIPs           []string
	RespondedFromIP   string
	Version           string
	BuildVariant      string
	ServicePort       int
	FirstDiscoveredAt time.Time
	LastDiscoveredAt  time.Time
	Health            Health
}

// Registry holds discovered remote agents for the lifetime of the maintenance process.
type Registry struct {
	mu              sync.RWMutex
	byKey           map[string]*Remote
	healthThreshold int
}

// New creates an empty in-memory registry. healthFailureThreshold is the consecutive
// failure count at which Health.Dead becomes true (same semantics as web remote health).
func New(healthFailureThreshold int) *Registry {
	if healthFailureThreshold <= 0 {
		healthFailureThreshold = 3
	}
	return &Registry{
		byKey:           make(map[string]*Remote),
		healthThreshold: healthFailureThreshold,
	}
}

func entryKey(cpuUUID, primaryIP string) string {
	if u := strings.TrimSpace(cpuUUID); u != "" {
		return "uuid:" + u
	}
	return "ip:" + strings.TrimSpace(primaryIP)
}

func primaryIPFromDiscovery(r discovery.DiscoveryResponse) string {
	if ip := strings.TrimSpace(r.RespondedFromIP); ip != "" {
		return ip
	}
	return strings.TrimSpace(r.HostIP)
}

func copyHostIPs(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}

func (reg *Registry) ipMatches(e *Remote, ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false
	}
	if e.PrimaryIP == ip || e.HostIP == ip || e.RespondedFromIP == ip {
		return true
	}
	for _, h := range e.HostIPs {
		if strings.TrimSpace(h) == ip {
			return true
		}
	}
	return false
}

func (reg *Registry) findEntryLocked(cpuUUID, primaryIP string) (*Remote, string) {
	uuid := strings.TrimSpace(cpuUUID)
	if uuid != "" {
		if e, ok := reg.byKey["uuid:"+uuid]; ok {
			return e, "uuid:" + uuid
		}
	}
	ip := strings.TrimSpace(primaryIP)
	if ip != "" {
		if e, ok := reg.byKey["ip:"+ip]; ok {
			return e, "ip:" + ip
		}
		for key, e := range reg.byKey {
			if reg.ipMatches(e, ip) {
				return e, key
			}
		}
	}
	return nil, ""
}

// UpsertFromRemoteIP records a minimal remote entry when only the connect IP is known.
func (reg *Registry) UpsertFromRemoteIP(ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "self" {
		return
	}
	reg.UpsertFromDiscovery(discovery.DiscoveryResponse{
		HostIP:          ip,
		RespondedFromIP: ip,
	})
}

// UpsertFromDiscovery records or refreshes a remote host from a discovery response.
// Self responses are ignored. Existing entries are kept when health is failing.
func (reg *Registry) UpsertFromDiscovery(r discovery.DiscoveryResponse) {
	if r.IsSelf {
		return
	}
	primary := primaryIPFromDiscovery(r)
	if primary == "" {
		return
	}
	now := time.Now()
	newKey := entryKey(r.CPUUUID, primary)

	reg.mu.Lock()
	defer reg.mu.Unlock()

	e, oldKey := reg.findEntryLocked(r.CPUUUID, primary)
	if e == nil {
		e = &Remote{FirstDiscoveredAt: now}
		reg.byKey[newKey] = e
	} else if oldKey != "" && oldKey != newKey {
		delete(reg.byKey, oldKey)
		reg.byKey[newKey] = e
	}

	e.PrimaryIP = primary
	e.CPUUUID = strings.TrimSpace(r.CPUUUID)
	e.Hostname = r.Hostname
	e.HostIP = strings.TrimSpace(r.HostIP)
	e.HostIPs = copyHostIPs(r.HostIPs)
	e.RespondedFromIP = strings.TrimSpace(r.RespondedFromIP)
	e.Version = r.Version
	e.BuildVariant = strings.TrimSpace(r.BuildVariant)
	e.ServicePort = r.ServicePort
	e.LastDiscoveredAt = now
	if e.FirstDiscoveredAt.IsZero() {
		e.FirstDiscoveredAt = now
	}
}

// RecordHealthCheck updates health state for a known remote IP. Unknown IPs are ignored.
func (reg *Registry) RecordHealthCheck(ip string, ok bool, errMsg string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	now := time.Now()

	reg.mu.Lock()
	defer reg.mu.Unlock()

	var e *Remote
	for _, candidate := range reg.byKey {
		if reg.ipMatches(candidate, ip) {
			e = candidate
			break
		}
	}
	if e == nil {
		return
	}

	e.Health.LastCheckedAt = now
	e.Health.LastError = strings.TrimSpace(errMsg)
	if ok {
		e.Health.OK = true
		e.Health.ConsecutiveFailures = 0
		e.Health.Dead = false
		e.Health.LastError = ""
		return
	}
	e.Health.OK = false
	e.Health.ConsecutiveFailures++
	if e.Health.ConsecutiveFailures >= reg.healthThreshold {
		e.Health.Dead = true
	}
}

// List returns a snapshot of all known remotes, including health-dead entries.
func (reg *Registry) List() []Remote {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	out := make([]Remote, 0, len(reg.byKey))
	for _, e := range reg.byKey {
		cp := *e
		cp.HostIPs = copyHostIPs(e.HostIPs)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PrimaryIP != out[j].PrimaryIP {
			return out[i].PrimaryIP < out[j].PrimaryIP
		}
		return out[i].CPUUUID < out[j].CPUUUID
	})
	return out
}

// Count returns the number of tracked remotes.
func (reg *Registry) Count() int {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return len(reg.byKey)
}

// ListForPush returns one entry per logical remote host (merged by cpu_uuid or shared IP).
func (reg *Registry) ListForPush() []Remote {
	return mergeRemotesForPush(reg.List())
}
