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

// RunRemote runs `systemctl start|stop <serviceName>` on the remote host via SSH.
// Used when the remote mol service is stopped and thus its API is unreachable.
// port is the SSH port (use 22 if 0). user is the SSH user (e.g. "root").
func RunRemote(host, user string, port int, serviceName, action string) error {
	if serviceName == "" {
		serviceName = "mol.service"
	}
	if port <= 0 {
		port = 22
	}
	if user == "" {
		user = "root"
	}
	if action != "start" && action != "stop" {
		return fmt.Errorf("action must be start or stop")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// -o StrictHostKeyChecking=no: avoid prompt on first connect; -o ConnectTimeout=10
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=10",
		"-p", fmt.Sprintf("%d", port),
		user+"@"+host,
		"systemctl", action, serviceName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
