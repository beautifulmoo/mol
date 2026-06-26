package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"contrabass-agent/maintenance/appmeta"
	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/server/remoteregistry"
)

func (s *Server) readLocalCurrentConfigContent() (string, error) {
	configPath := s.currentConfigPath()
	if configPath == "" {
		return "", fmt.Errorf("current version symlink not found")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s not found under current", appmeta.ConfigFileName)
		}
		return "", fmt.Errorf("%s read failed: %w", appmeta.ConfigFileName, err)
	}
	content := string(data)
	if err := validateCurrentConfigContent(content); err != nil {
		return "", err
	}
	return content, nil
}

func (s *Server) pushConfigContentToRemote(content, ip string) error {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return fmt.Errorf("remote ip is required")
	}
	payload, err := json.Marshal(map[string]interface{}{
		"content":             content,
		"backup_before_write": true,
	})
	if err != nil {
		return err
	}
	out, err := s.fetchRemoteAPI(ip, http.MethodPost, "/current-config", payload)
	if err != nil {
		if errors.Is(err, errRemoteAPIParse) {
			return errRemoteAPIParse
		}
		return fmt.Errorf("remote config push failed: %w", err)
	}
	if out.Status == "success" {
		return nil
	}
	return remoteAPIFailFromResponse(out, "")
}

func remoteRegistryEntryDTO(r remoteregistry.Remote) map[string]interface{} {
	return map[string]interface{}{
		"primary_ip":         r.PrimaryIP,
		"cpu_uuid":           r.CPUUUID,
		"hostname":           r.Hostname,
		"host_ip":            r.HostIP,
		"host_ips":           r.HostIPs,
		"responded_from_ip":  r.RespondedFromIP,
		"version":            r.Version,
		"build_variant":      r.BuildVariant,
		"service_port":       r.ServicePort,
		"first_discovered_at": r.FirstDiscoveredAt,
		"last_discovered_at":  r.LastDiscoveredAt,
		"health_ok":          r.Health.OK,
		"health_dead":        r.Health.Dead,
		"health_failures":    r.Health.ConsecutiveFailures,
		"health_last_error":  r.Health.LastError,
	}
}

type pushHostInput struct {
	PrimaryIP string   `json:"primary_ip"`
	Hostname  string   `json:"hostname"`
	CPUUUID   string   `json:"cpu_uuid"`
	IPs       []string `json:"ips"`
}

func pushHostInputToRemote(h pushHostInput) remoteregistry.Remote {
	r := remoteregistry.Remote{
		PrimaryIP: strings.TrimSpace(h.PrimaryIP),
		Hostname:  strings.TrimSpace(h.Hostname),
		CPUUUID:   strings.TrimSpace(h.CPUUUID),
	}
	seen := make(map[string]bool)
	var ips []string
	add := func(ip string) {
		ip = strings.TrimSpace(ip)
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		ips = append(ips, ip)
	}
	add(r.PrimaryIP)
	for _, ip := range h.IPs {
		add(ip)
	}
	if r.PrimaryIP == "" && len(ips) > 0 {
		r.PrimaryIP = ips[0]
	}
	r.HostIP = r.PrimaryIP
	r.HostIPs = ips
	if len(ips) > 0 {
		r.RespondedFromIP = ips[0]
	}
	return r
}

func (s *Server) syncRemoteRegistryIPs(ips []string) {
	for _, ip := range ips {
		s.remoteRegistry.UpsertFromRemoteIP(ip)
	}
}

func (s *Server) syncRemoteRegistryFromPushHosts(hosts []pushHostInput) {
	for _, h := range hosts {
		r := pushHostInputToRemote(h)
		if len(remoteregistry.ConnectIPs(r)) == 0 {
			continue
		}
		s.remoteRegistry.UpsertFromDiscovery(discovery.DiscoveryResponse{
			Type:            "DISCOVERY_RESPONSE",
			HostIP:          r.HostIP,
			HostIPs:         r.HostIPs,
			RespondedFromIP: r.RespondedFromIP,
			Hostname:        r.Hostname,
			CPUUUID:         r.CPUUUID,
		})
	}
}

// remotesForConfigPush returns push targets. When the UI sends hosts (one per card), that list is authoritative.
func (s *Server) remotesForConfigPush(hosts []pushHostInput, legacyIPs []string) []remoteregistry.Remote {
	if len(hosts) > 0 {
		s.syncRemoteRegistryFromPushHosts(hosts)
		out := make([]remoteregistry.Remote, 0, len(hosts))
		for _, h := range hosts {
			r := pushHostInputToRemote(h)
			if len(remoteregistry.ConnectIPs(r)) == 0 {
				continue
			}
			out = append(out, r)
		}
		if len(out) > 0 {
			return out
		}
	}
	s.syncRemoteRegistryIPs(legacyIPs)
	remotes := s.remoteRegistry.ListForPush()
	if len(remotes) > 0 {
		return remotes
	}
	list, err := s.discovery.DoDiscovery(discovery.DiscoveryRunOptions{ExcludeSelf: true})
	if err != nil {
		return remotes
	}
	for _, host := range list {
		s.remoteRegistry.UpsertFromDiscovery(host)
	}
	return s.remoteRegistry.ListForPush()
}

func (s *Server) pushConfigToRemoteHost(content string, remote remoteregistry.Remote) (connectIP string, tried []string, err error) {
	return tryRemoteHostVoid(remote, func(ip string) error {
		return s.pushConfigContentToRemote(content, ip)
	})
}

func (s *Server) handleDiscoveredRemotes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	remotes := s.DiscoveredRemotes()
	out := make([]map[string]interface{}, 0, len(remotes))
	for _, remote := range remotes {
		out = append(out, remoteRegistryEntryDTO(remote))
	}
	s.send(w, "success", map[string]interface{}{"remotes": out}, http.StatusOK)
}

func writeNDJSONLine(w http.ResponseWriter, flusher http.Flusher, v interface{}) error {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

func (s *Server) handleCurrentConfigPushLocalAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	req, err := parseBulkHostsRequest(r)
	if err != nil {
		s.send(w, "fail", "invalid body", http.StatusBadRequest)
		return
	}
	content, err := s.readLocalCurrentConfigContent()
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	remotes := s.remotesForConfigPush(req.Hosts, req.IPs)

	s.runBulkRemoteNDJSON(w, remotes, bulkNDJSONOptions{
		HistoryFmt: func(sum bulkRunSummary) string {
			return fmt.Sprintf("config push-all finished succeeded=%d failed=%d", sum.Succeeded, sum.Failed)
		},
	}, func(remote remoteregistry.Remote, evt map[string]interface{}) bulkHostOutcome {
		connectIP, tried, pushErr := s.pushConfigToRemoteHost(content, remote)
		if pushErr != nil {
			return bulkHostOutcome{Status: bulkHostFail, Message: pushErr.Error(), TriedIPs: tried}
		}
		return bulkHostOutcome{Status: bulkHostSuccess, ConnectIP: connectIP, TriedIPs: tried}
	})
}

