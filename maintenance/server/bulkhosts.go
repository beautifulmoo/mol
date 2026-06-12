package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

type bulkHostsRequest struct {
	Hosts []pushHostInput `json:"hosts"`
	IPs   []string        `json:"ips"`
}

func parseBulkHostsRequest(r *http.Request) (bulkHostsRequest, error) {
	var req bulkHostsRequest
	if r.Body == nil {
		return req, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		return req, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	return req, nil
}
