package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sdkSource struct {
	name string
	load func(sdkVersion) (manifest, error)
}

// buildSDK assembles the SDK in destination. It uses the official API recipe
// and falls back to the shockpkg mirror when that recipe fails at any point.
func (a *app) buildSDK(v sdkVersion, destination string) error {
	if _, _, _, err := a.platform(); err != nil {
		return err
	}
	sources := []sdkSource{{"AIR SDK API", a.apiManifest}, {"shockpkg mirror", a.mirrorManifest}}
	var failures []string
	for i, source := range sources {
		build, err := source.load(v)
		if err == nil {
			err = a.fetchSDK(v, build, destination)
		}
		if err == nil {
			return a.finishSDK(v, destination)
		}
		if a.ctx.Err() != nil {
			return a.ctx.Err()
		}
		failures = append(failures, fmt.Sprintf("%s: %v", source.name, err))
		if i+1 < len(sources) {
			a.ui.warning(fmt.Sprintf("%s failed: %v. Trying the %s.", source.name, err, sources[i+1].name))
			if err := clearDirectory(destination); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("cannot download AIR SDK %s: %s", v, strings.Join(failures, "; "))
}

// fetchSDK downloads the archives of one manifest and extracts them into
// destination. A manifest that lists components is unpacked component by
// component, one that carries a single archive is taken whole, and either way
// the result must contain adt and adt.jar for this host to count as an SDK.
func (a *app) fetchSDK(v sdkVersion, build manifest, destination string) error {
	_, componentOS, key, err := a.platform()
	if err != nil {
		return err
	}
	if len(build.Components) > 0 {
		if _, ok := build.Components[componentOS]; !ok {
			return fmt.Errorf("manifest has no %s SDK component", componentOS)
		}
		var names []string
		for name := range build.Components {
			if (name != "linux" && name != "macos" && name != "window" && name != "windows") || name == componentOS {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		endpoint := a.endpoint()
		for _, name := range names {
			info := build.Components[name]
			if info.Version == "" {
				return fmt.Errorf("missing component version: %s", name)
			}
			info.URL = endpoint + "/releases/components/" + url.PathEscape(name) + "/" + url.PathEscape(info.Version)
			file, err := a.download(info, "POST", name, destination)
			if err != nil {
				return err
			}
			if err := a.extract(file, destination); err != nil {
				return err
			}
		}
	} else {
		info := build.URLs[key]
		if info.URL == "" {
			return fmt.Errorf("no %s archive in manifest for %s", key, v)
		}
		if strings.HasPrefix(info.URL, "/") {
			info.URL = "https://airsdk.harman.com" + info.URL
		}
		if strings.HasPrefix(info.URL, "https://airsdk.harman.com/") {
			parsed, err := url.Parse(info.URL)
			if err != nil {
				return err
			}
			query := parsed.Query()
			query.Set("license", "accepted")
			parsed.RawQuery = query.Encode()
			info.URL = parsed.String()
		}
		file, err := a.download(info, "GET", "AIR SDK "+v.String(), destination)
		if err != nil {
			return err
		}
		if err := a.extract(file, destination); err != nil {
			return err
		}
	}
	adt := "adt"
	if a.os == "windows" {
		adt = "adt.bat"
	}
	for _, path := range []string{filepath.Join("bin", adt), filepath.Join("lib", "adt.jar")} {
		info, err := os.Stat(filepath.Join(destination, path))
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("downloaded files do not contain a %s AIR SDK: %s", a.os, v)
		}
	}
	return nil
}

// finishSDK applies the per-OS setup and writes air-sdk-description.xml, the
// file installed SDK discovery reads to recognize the directory.
func (a *app) finishSDK(v sdkVersion, destination string) error {
	stop := a.ui.activity("Configuring the SDK", nil)
	err := configureSDK(a.ctx, destination, a.arch)
	stop()
	if err != nil {
		return err
	}
	number := fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	description := fmt.Sprintf("<air-sdk-description><name>AIR %s</name><version>%s</version><build>%d</build></air-sdk-description>\n", number, number, v[3])
	return os.WriteFile(filepath.Join(destination, "air-sdk-description.xml"), []byte(description), 0644)
}
