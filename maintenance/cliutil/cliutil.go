// Package cliutil holds small helpers shared by maintenance CLIs (apply, versions, etc.).
package cliutil

import (
	"net"
	"strconv"
	"strings"
	"time"

	"contrabass-agent/maintenance/config"
)

// NormalizeAPIPrefix returns a path prefix for Gin API routes (leading slash, no trailing slash).
func NormalizeAPIPrefix(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/api/v1"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimSuffix(p, "/")
}

// HTTPPortOrDefault returns Server.HTTPPort, or 8888 if unset or invalid.
func HTTPPortOrDefault(cfg *config.Config) int {
	if cfg == nil || cfg.ServerHTTPPort <= 0 || cfg.ServerHTTPPort > 65535 {
		return 8888
	}
	return cfg.ServerHTTPPort
}

// RemoteDialAddr returns "ip:port" for TCP checks to a remote agent's Gin listener.
func RemoteDialAddr(cfg *config.Config, ip string) string {
	return net.JoinHostPort(ip, strconv.Itoa(HTTPPortOrDefault(cfg)))
}

// RemoteBaseURL returns "http://ip:port" for the remote agent HTTP API (Gin).
func RemoteBaseURL(cfg *config.Config, ip string) string {
	return "http://" + RemoteDialAddr(cfg, ip)
}

// DialTCP tries a TCP connection and closes it immediately (reachability check).
func DialTCP(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	_ = conn.Close()
	return nil
}
