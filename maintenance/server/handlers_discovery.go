package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"contrabass-agent/maintenance/discovery"
	"contrabass-agent/maintenance/hostinfoapi"
)

const (
	sseContentType = "text/event-stream"
	sseNoCache     = "no-cache"
	sseKeepAlive   = "keep-alive"
)

func (s *Server) handleHostInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if ip == "" || ip == "self" {
		s.handleSelf(w, r)
		return
	}
	resp, err := hostinfoapi.RemoteHostInfo(s.discovery, ip)
	if err != nil {
		log.Printf("discovery: ERROR: DoDiscoveryUnicast(ip=%s) failed: %v", ip, err)
		s.send(w, "fail", err.Error(), http.StatusOK)
		return
	}
	s.remoteRegistry.UpsertFromDiscovery(*resp)
	s.send(w, "success", resp, http.StatusOK)
}

// Query params for GET .../discovery and .../discovery/stream:
//   exclude_self — true/1/yes/on: omit this host from results; omitted or false: include self ("self": true in JSON when applicable).
//   exclude-self — same as exclude_self (optional alias).
//   timeout      — integer seconds (1–600); omitted: Maintenance.DiscoveryTimeoutSeconds (0 or unset in YAML → 10s).
func requestQueryValues(r *http.Request) url.Values {
	if r.URL != nil && r.URL.RawQuery != "" {
		if q, err := url.ParseQuery(r.URL.RawQuery); err == nil {
			return q
		}
	}
	if r.RequestURI != "" {
		if i := strings.IndexByte(r.RequestURI, '?'); i >= 0 {
			if q, err := url.ParseQuery(r.RequestURI[i+1:]); err == nil {
				return q
			}
		}
	}
	return url.Values{}
}

func parseDiscoveryRunOptions(r *http.Request) (discovery.DiscoveryRunOptions, error) {
	q := requestQueryValues(r)
	var opts discovery.DiscoveryRunOptions
	v := strings.TrimSpace(q.Get("exclude_self"))
	if v == "" {
		v = strings.TrimSpace(q.Get("exclude-self"))
	}
	if v != "" {
		opts.ExcludeSelf = parseQueryBoolTrue(v)
	}
	if v := strings.TrimSpace(q.Get("timeout")); v != "" {
		sec, err := strconv.Atoi(v)
		if err != nil {
			return opts, fmt.Errorf("timeout must be an integer (seconds)")
		}
		if sec < 1 || sec > 600 {
			return opts, fmt.Errorf("timeout must be between 1 and 600 seconds")
		}
		opts.Timeout = time.Duration(sec) * time.Second
	}
	return opts, nil
}

func parseQueryBoolTrue(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

func parseReusePreviousConfig(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return true
	}
	if s == "0" || s == "false" || s == "no" || s == "off" {
		return false
	}
	return parseQueryBoolTrue(s)
}

func writeDiscoverySSEFail(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", sseContentType)
	w.Header().Set("Cache-Control", sseNoCache)
	w.Header().Set("Connection", sseKeepAlive)
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	payload, _ := json.Marshal(map[string]string{"message": message})
	if _, err := fmt.Fprintf(w, "event: discoveryfail\ndata: %s\n\n", payload); err != nil {
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	opts, err := parseDiscoveryRunOptions(r)
	if err != nil {
		s.send(w, "fail", err.Error(), http.StatusBadRequest)
		return
	}
	list, err := s.discovery.DoDiscovery(opts)
	if err != nil {
		log.Printf("discovery: ERROR: DoDiscovery failed: %v", err)
		s.send(w, "fail", err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []discovery.DiscoveryResponse{}
	}
	for _, host := range list {
		s.remoteRegistry.UpsertFromDiscovery(host)
	}
	log.Printf("discovery API: returning %d host(s)", len(list))
	s.send(w, "success", list, http.StatusOK)
}

func (s *Server) handleDiscoveryStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.send(w, "fail", nil, http.StatusMethodNotAllowed)
		return
	}
	opts, err := parseDiscoveryRunOptions(r)
	if err != nil {
		writeDiscoverySSEFail(w, err.Error())
		return
	}
	ch, err := s.discovery.DoDiscoveryStream(opts)
	if err != nil {
		log.Printf("discovery: ERROR: DoDiscoveryStream failed: %v", err)
		writeDiscoverySSEFail(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", sseContentType)
	w.Header().Set("Cache-Control", sseNoCache)
	w.Header().Set("Connection", sseKeepAlive)
	w.WriteHeader(http.StatusOK)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	enc := json.NewEncoder(w)
	for host := range ch {
		s.remoteRegistry.UpsertFromDiscovery(host)
		if _, err := w.Write([]byte("data: ")); err != nil {
			return
		}
		if err := enc.Encode(host); err != nil {
			return
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
	if _, err := w.Write([]byte("event: done\ndata: {}\n\n")); err != nil {
		return
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
