//go:build !linux

package main

func setSOReuseport(fd int) error {
	return nil
}
