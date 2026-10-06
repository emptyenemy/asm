package cli

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type installedSDK struct {
	Version sdkVersion
	Path    string
}

func readSDK(path string) (installedSDK, error) {
	data, err := os.ReadFile(filepath.Join(path, "air-sdk-description.xml"))
	if err != nil {
		return installedSDK{}, err
	}
	var description struct {
		XMLName xml.Name `xml:"air-sdk-description"`
		Version string   `xml:"version"`
		Build   string   `xml:"build"`
	}
	if err = xml.Unmarshal(data, &description); err != nil {
		return installedSDK{}, err
	}
	parts, err := versionParts(strings.TrimSpace(description.Version))
	if err != nil || len(parts) < 3 {
		return installedSDK{}, errors.New("invalid SDK description version")
	}
	v := sdkVersion{parts[0], parts[1], parts[2], -1}
	if len(parts) == 4 {
		v[3] = parts[3]
	} else if strings.TrimSpace(description.Build) != "" {
		build, err := versionParts(strings.TrimSpace(description.Build))
		if err != nil || len(build) != 1 {
			return installedSDK{}, errors.New("invalid SDK description build")
		}
		v[3] = build[0]
	}
	return installedSDK{v, path}, nil
}

func (a *app) installed() ([]installedSDK, error) {
	root, err := a.root()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // the first install creates it
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read SDK directory %s: %w", root, err)
	}
	var sdks []installedSDK
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".asm-") {
			continue
		}
		if !entry.IsDir() {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			info, err := os.Stat(filepath.Join(root, entry.Name()))
			if err != nil || !info.IsDir() {
				continue
			}
		}
		sdk, err := readSDK(filepath.Join(root, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			a.ui.warning(fmt.Sprintf("Cannot read SDK description in %s: %v", filepath.Join(root, entry.Name()), err))
			continue
		}
		sdks = append(sdks, sdk)
	}
	sort.Slice(sdks, func(i, j int) bool { return sdks[i].Version.newer(sdks[j].Version) })
	return sdks, nil
}
