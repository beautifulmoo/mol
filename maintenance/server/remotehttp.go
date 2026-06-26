package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// remoteHTTPClient is used to call another agent's upload/apply APIs (no SSH/SCP).
var remoteHTTPClient = &http.Client{Timeout: 300 * time.Second}

var errRemoteAPIParse = errors.New("failed to parse remote response")

// remoteAPIURL builds http://{ip}:{port}{apiPrefix}{apiPath}. apiPath must start with "/".
func (s *Server) remoteAPIURL(ip, apiPath string) (string, error) {
	baseURL, err := s.remoteBaseURL(ip)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	return baseURL + s.apiPrefix + apiPath, nil
}

func callRemoteAPIAtURL(method, url string, body []byte) (APIResponse, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return APIResponse{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := remoteHTTPClient.Do(req)
	if err != nil {
		return APIResponse{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return APIResponse{}, err
	}
	var out APIResponse
	if json.Unmarshal(respBody, &out) != nil {
		return APIResponse{}, errRemoteAPIParse
	}
	return out, nil
}

// fetchRemoteAPI calls a remote agent API at ip and returns the parsed envelope.
func (s *Server) fetchRemoteAPI(ip, method, apiPath string, body []byte) (APIResponse, error) {
	url, err := s.remoteAPIURL(ip, apiPath)
	if err != nil {
		return APIResponse{}, err
	}
	return callRemoteAPIAtURL(method, url, body)
}

func sendRemoteAPIError(w http.ResponseWriter, send func(http.ResponseWriter, string, interface{}, int), errPrefix string, err error) {
	if errors.Is(err, errRemoteAPIParse) {
		send(w, "fail", errRemoteAPIParse.Error(), http.StatusOK)
		return
	}
	send(w, "fail", errPrefix+err.Error(), http.StatusOK)
}

// proxyRemoteAPI forwards a remote agent APIResponse to the client via send.
// urlErrPrefix is used when remoteAPIURL fails; reqErrPrefix when the HTTP call or parse fails.
// When reqErrPrefix is empty, urlErrPrefix is used for all errors.
func (s *Server) proxyRemoteAPI(w http.ResponseWriter, ip, urlErrPrefix, reqErrPrefix, method, apiPath string, body []byte) {
	if reqErrPrefix == "" {
		reqErrPrefix = urlErrPrefix
	}
	url, err := s.remoteAPIURL(ip, apiPath)
	if err != nil {
		sendRemoteAPIError(w, s.send, urlErrPrefix, err)
		return
	}
	out, err := callRemoteAPIAtURL(method, url, body)
	if err != nil {
		sendRemoteAPIError(w, s.send, reqErrPrefix, err)
		return
	}
	s.send(w, out.Status, out.Data, http.StatusOK)
}

func apiResponseErrorMessage(out APIResponse) string {
	switch v := out.Data.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return v
		}
	case nil:
	default:
		if b, err := json.Marshal(v); err == nil && len(b) > 0 && string(b) != "null" {
			return string(b)
		}
	}
	return "remote request failed"
}

func remoteAPIFailFromResponse(out APIResponse, fallback string) error {
	msg := apiResponseErrorMessage(out)
	if msg == "remote request failed" && fallback != "" {
		msg = fallback
	}
	return fmt.Errorf("%s", msg)
}
