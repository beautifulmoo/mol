package svcstatus

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	localTimeout  = 5 * time.Second
	remoteTimeout = 15 * time.Second
)

// GetLocal runs `sudo systemctl status <serviceName>` on the local host and returns the combined output.
func GetLocal(serviceName string) (string, error) {
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "status", serviceName)
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

// StartLocal runs `sudo systemctl start <serviceName>` on the local host.
func StartLocal(serviceName string) error {
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "start", serviceName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// StopLocal runs `sudo systemctl stop <serviceName>` on the local host.
func StopLocal(serviceName string) error {
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "systemctl", "stop", serviceName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func runRemoteSystemctl(ip, serviceName, sshUser, identityFile, action string) error {
	if ip == "" {
		return fmt.Errorf("ip required")
	}
	if sshUser == "" {
		sshUser = "kt"
	}
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
	}
	if identityFile != "" {
		args = append(args, "-i", identityFile)
	}
	args = append(args, sshUser+"@"+ip, "sudo systemctl "+action+" "+serviceName)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return fmt.Errorf("%s", text)
		}
		return fmt.Errorf("ssh: %w", err)
	}
	return nil
}

// StartRemote SSHs to user@ip and runs `sudo systemctl start <serviceName>`.
func StartRemote(ip, serviceName, sshUser, identityFile string) error {
	return runRemoteSystemctl(ip, serviceName, sshUser, identityFile, "start")
}

// StopRemote SSHs to user@ip and runs `sudo systemctl stop <serviceName>`.
func StopRemote(ip, serviceName, sshUser, identityFile string) error {
	return runRemoteSystemctl(ip, serviceName, sshUser, identityFile, "stop")
}

// GetRemote SSHs to user@ip and runs `sudo systemctl status <serviceName>`, returns the combined output.
// identityFile: optional path to private key; use when the process runs as a user that doesn't have kt's default ~/.ssh/ key (e.g. systemd runs as root).
func GetRemote(ip, serviceName, sshUser, identityFile string) (string, error) {
	if ip == "" {
		return "", fmt.Errorf("ip required")
	}
	if sshUser == "" {
		sshUser = "kt"
	}
	if serviceName == "" {
		serviceName = "mol.service"
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteTimeout)
	defer cancel()
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5",
		"-o", "BatchMode=yes",
	}
	if identityFile != "" {
		args = append(args, "-i", identityFile)
	}
	args = append(args, sshUser+"@"+ip, "sudo systemctl status "+serviceName)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("ssh: %w", err)
		}
		return text, nil
	}
	return text, nil
}
