//go:build linux

package main

import "syscall"

func setSOReuseport(fd int) error {
	const soReuseport = 15 // SO_REUSEPORT on Linux (arch-specific in syscall; 15 on amd64/arm64)
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, soReuseport, 1)
}
