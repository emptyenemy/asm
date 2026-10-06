package cli

import (
	"errors"
	"fmt"
	"os"
)

// platform maps the host Go reports to the three names the catalogs use: the
// shockpkg platform, the component name, and the AIR_* archive key. An
// unsupported host is rejected here, before any request is made.
func (a *app) platform() (string, string, string, error) {
	switch a.os {
	case "windows":
		if a.arch == "amd64" {
			return "windows", "window", "AIR_Win", nil
		}
	case "darwin":
		if a.arch == "amd64" || a.arch == "arm64" {
			return "mac", "macos", "AIR_Mac", nil
		}
	case "linux":
		if a.arch == "amd64" || a.arch == "arm64" {
			return "linux", "linux", "AIR_Linux", nil
		}
	}
	return "", "", "", fmt.Errorf("unsupported SDK host: %s/%s", a.os, a.arch)
}

// occupied reports whether anything at all exists at path. Lstat is used so a
// dangling symbolic link still counts, which is what the install and clean
// paths want when they refuse to touch a destination.
func occupied(path string) bool { _, err := os.Lstat(path); return !errors.Is(err, os.ErrNotExist) }
