package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// licenseURL is where the AIR SDK license can be read before accepting it.
const licenseURL = "https://airsdk.harman.com/download"

// loadSettings reads the AIR SDK Manager settings: KEY=value lines, the last
// one winning. A missing file is not an error; asm creates it once there is
// something to keep.
func (a *app) loadSettings() error {
	if a.settings != nil {
		return nil
	}
	data, err := os.ReadFile(a.configFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
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

// root returns the SDK directory. When the settings name none, asm takes
// ~/sdks/air, the folder the AIR SDK installation guides recommend, and saves
// it as AIR_SDKS so that AIR SDK Manager finds the same SDKs.
func (a *app) root() (string, error) {
	if err := a.loadSettings(); err != nil {
		return "", err
	}
	root := a.settings["AIR_SDKS"]
	if root == "" {
		root = filepath.Join(filepath.Dir(filepath.Dir(a.configFile)), "sdks", "air")
		if err := a.saveSetting("AIR_SDKS", root); err != nil {
			return "", fmt.Errorf("cannot save the SDK folder in %s: %w", a.configFile, err)
		}
		a.ui.note("New settings:", a.configFile)
		a.ui.note("SDK folder:  ", root)
	}
	return filepath.Abs(root)
}

// saveSetting records KEY=value in the settings file as AIR SDK Manager keeps
// it. The line that sets the key is replaced, or a line is added at the end;
// every other line, a byte order mark and the line endings stay as they are.
func (a *app) saveSetting(key, value string) error {
	if err := a.loadSettings(); err != nil {
		return err
	}
	data, err := os.ReadFile(a.configFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(data)
	newline := "\n"
	if strings.Contains(text, "\r\n") || (text == "" && a.os == "windows") {
		newline = "\r\n"
	}
	lines := strings.SplitAfter(text, "\n")
	replaced := false
	for i := len(lines) - 1; i >= 0 && !replaced; i-- {
		line := strings.TrimPrefix(lines[i], "\ufeff")
		name, _, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		bom := lines[i][:len(lines[i])-len(line)]
		ending := line[len(strings.TrimRight(line, "\r\n")):]
		lines[i] = bom + key + "=" + value + ending
		replaced = true
	}
	text = strings.Join(lines, "")
	if !replaced {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += newline
		}
		text += key + "=" + value + newline
	}
	if err := os.MkdirAll(filepath.Dir(a.configFile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(a.configFile, []byte(text), 0644); err != nil {
		return err
	}
	a.settings[key] = value
	return nil
}

// acceptLicense makes sure the AIR SDK license is accepted before anything is
// downloaded: in the settings, with --accept-license for this run only, or by
// a yes in the terminal. A yes is saved as HAS_ACCEPTED_LICENSE, which is what
// AIR SDK Manager records when its license is accepted.
func (a *app) acceptLicense(o options) error {
	if o.license || a.settings["HAS_ACCEPTED_LICENSE"] == "true" {
		return nil
	}
	if !a.ui.canAsk() {
		return errors.New("accept the AIR SDK license with --accept-license; read it at " + licenseURL)
	}
	a.ui.say("The AIR SDK has its own license: "+licenseURL, "")
	accepted, err := a.ui.ask(a.ctx, "Accept the AIR SDK license? [y/N]")
	if err != nil {
		return err
	}
	if !accepted {
		return errors.New("the AIR SDK license was not accepted, so nothing was downloaded")
	}
	return a.saveSetting("HAS_ACCEPTED_LICENSE", "true")
}

// endpoint returns the AIR SDK API base: API_ENDPOINT from the manager
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
