package server

import (
	"errors"
	"testing"
)

func TestServiceStatusOutputActive(t *testing.T) {
	if !serviceStatusOutputActive("     Active: active (running) since ...") {
		t.Fatal("expected active")
	}
	if serviceStatusOutputActive("Active: inactive (dead)") {
		t.Fatal("expected inactive")
	}
}

func TestIsRestartInProgressTransport(t *testing.T) {
	if !isRestartInProgressTransport(errors.New("connection reset by peer"), "") {
		t.Fatal("expected transport error")
	}
	if isRestartInProgressTransport(nil, "systemctl failed") {
		t.Fatal("expected false")
	}
}
