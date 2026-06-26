package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestRemoteAPIURL(t *testing.T) {
	s := &Server{remoteProxyPort: 8080, apiPrefix: "/api/v1"}
	u, err := s.remoteAPIURL("10.0.0.5", "/versions/list")
	if err != nil {
		t.Fatal(err)
	}
	want := "http://10.0.0.5:8080/api/v1/versions/list"
	if u != want {
		t.Fatalf("url = %q, want %q", u, want)
	}
}

func TestFetchRemoteAPI(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/service-status" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q", r.Method)
		}
		_ = json.NewEncoder(w).Encode(APIResponse{
			Status: "success",
			Data:   map[string]string{"output": "active"},
		})
	}))
	defer remote.Close()

	host, portStr, err := net.SplitHostPort(remote.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{remoteProxyPort: port, apiPrefix: "/api/v1"}

	out, err := s.fetchRemoteAPI(host, http.MethodGet, "/service-status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "success" {
		t.Fatalf("status = %q", out.Status)
	}
}

func TestCallRemoteAPIAtURL_parseError(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer remote.Close()

	_, err := callRemoteAPIAtURL(http.MethodGet, remote.URL, nil)
	if err == nil || err.Error() != errRemoteAPIParse.Error() {
		t.Fatalf("err = %v, want %q", err, errRemoteAPIParse)
	}
}
