package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (a *app) loadSettings() error {
	if a.settings != nil {
		return nil
	}
	data, err := os.ReadFile(a.configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("AIR SDK Manager settings not found: %s", a.configFile)
		}
		return err
	}
	a.settings = make(map[string]string)
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			a.settings[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	return nil
}

func (a *app) root() (string, error) {
	if err := a.loadSettings(); err != nil {
		return "", err
	}
	root := a.settings["AIR_SDKS"]
	if root == "" {
		return "", fmt.Errorf("AIR_SDKS is not set in %s", a.configFile)
	}
	return filepath.Abs(root)
}

// endpoint returns the HARMAN API base: API_ENDPOINT from the manager
// configuration, or the official service.
func (a *app) endpoint() string {
	if a.settings == nil {
		_ = a.loadSettings()
	}
	if custom := strings.TrimRight(a.settings["API_ENDPOINT"], "/"); custom != "" {
		return custom
	}
	return strings.TrimRight(a.apiURL, "/")
}
