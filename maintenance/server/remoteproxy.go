package server

import (
	"fmt"

	"contrabass-agent/maintenance/server/remoteregistry"
)

func tryRemoteHostVoid(remote remoteregistry.Remote, fn func(ip string) error) (connectIP string, tried []string, err error) {
	ips := remoteregistry.ConnectIPs(remote)
	if len(ips) == 0 {
		return "", nil, fmt.Errorf("no connect ip for host")
	}
	var lastErr error
	for _, ip := range ips {
		tried = append(tried, ip)
		if callErr := fn(ip); callErr == nil {
			return ip, tried, nil
		} else {
			lastErr = callErr
		}
	}
	return "", tried, lastErr
}

func tryRemoteHostWithDetail(remote remoteregistry.Remote, fn func(ip string) (detail string, err error)) (connectIP string, tried []string, detail string, err error) {
	ips := remoteregistry.ConnectIPs(remote)
	if len(ips) == 0 {
		return "", nil, "", fmt.Errorf("no connect ip for host")
	}
	var lastErr error
	var lastDetail string
	for _, ip := range ips {
		tried = append(tried, ip)
		d, callErr := fn(ip)
		if callErr == nil {
			return ip, tried, d, nil
		}
		lastErr = callErr
		lastDetail = d
	}
	if lastDetail != "" && lastErr != nil {
		return "", tried, lastDetail, lastErr
	}
	return "", tried, lastDetail, lastErr
}

func tryRemoteHostFirstReachable[T any](remote remoteregistry.Remote, fn func(ip string) (T, error)) (val T, err error) {
	ips := remoteregistry.ConnectIPs(remote)
	if len(ips) == 0 {
		return val, fmt.Errorf("no connect ip for host")
	}
	var lastErr error
	for _, ip := range ips {
		v, callErr := fn(ip)
		if callErr == nil {
			return v, nil
		}
		lastErr = callErr
	}
	if lastErr != nil {
		return val, lastErr
	}
	return val, fmt.Errorf("no connect ip for host")
}
