package clirest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExpandLocalPath expands leading ~ and cleans a local filesystem path for os.Open.
// Relative paths resolve from the process current working directory (unchanged from the shell).
func ExpandLocalPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	return filepath.Clean(path), nil
}
