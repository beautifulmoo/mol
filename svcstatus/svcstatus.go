package svcstatus

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const localTimeout = 5 * time.Second

// GetLocal runs `systemctl status <serviceName>` on the local host and returns the combined output. mol.service runs as root so sudo is not used.
func GetLocal(serviceName string) (string, error) {
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "status", serviceName)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		// systemctl status returns non-zero when unit is not active; we still return the output
		if text == "" {
			return "", fmt.Errorf("systemctl: %w", err)
		}
		return text, nil
	}
	return text, nil
}

// StartLocal runs `systemctl start <serviceName>` on the local host.
func StartLocal(serviceName string) error {
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "start", serviceName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// StopLocal runs `systemctl stop <serviceName>` on the local host.
func StopLocal(serviceName string) error {
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "stop", serviceName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
