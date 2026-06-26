package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

const bulkJSONBodyLimit = 65536

func readOptionalJSONBody(r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	return body, nil
}

type bulkHostsRequest struct {
	Hosts []pushHostInput `json:"hosts"`
	IPs   []string        `json:"ips"`
}

func parseBulkHostsRequest(r *http.Request) (bulkHostsRequest, error) {
	var req bulkHostsRequest
	body, err := readOptionalJSONBody(r, bulkJSONBodyLimit)
	if err != nil {
		return req, err
	}
	if body == nil {
		return req, nil
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	return req, nil
}

type bulkApplyUpdateRequest struct {
	Hosts               []pushHostInput `json:"hosts"`
	IPs                 []string        `json:"ips"`
	Version             string          `json:"version"`
	AgentVariant        string          `json:"agent_variant"`
	ReusePreviousConfig *bool           `json:"reuse_previous_config"`
}

func parseBulkApplyUpdateRequest(r *http.Request) (bulkApplyUpdateRequest, error) {
	var req bulkApplyUpdateRequest
	body, err := readOptionalJSONBody(r, bulkJSONBodyLimit)
	if err != nil {
		return req, err
	}
	if body == nil {
		return req, nil
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return req, err
	}
	return req, nil
}
