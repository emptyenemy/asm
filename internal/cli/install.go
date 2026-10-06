package cli

import (
	"errors"
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

// clearDirectory empties a directory without removing it, so the staging
// directory can be reused for the next source in the fallback chain.
func clearDirectory(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
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

// uninstall removes the one installed SDK the version argument selects. The
// directory is re-read under the root lock and left alone if it changed since
// the listing, and an argument that matches several SDKs is refused rather than
// guessed at.
func (a *app) uninstall(request string) error {
	parts, err := versionParts(request)
	if err != nil {
		return err
	}
	root, err := a.root()
	if err != nil {
		return err
	}
	unlock, err := lockSDKRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	sdks, err := a.installed()
	if err != nil {
		return err
	}
	var matches []installedSDK
	for _, sdk := range sdks {
		if sdk.Version.matches(parts) {
			matches = append(matches, sdk)
		}
	}
	if len(matches) == 0 {
		return fmt.Errorf("no installed SDK matches %s; run asm list", request)
	}
	if len(matches) > 1 {
		var choices []string
		for _, sdk := range matches {
			choices = append(choices, sdk.Version.String()+" at "+sdk.Path)
		}
		return fmt.Errorf("multiple SDKs match %s: %s; select one full version from asm list", request, strings.Join(choices, "; "))
	}
	sdk := matches[0]
	path, err := childPath(root, sdk.Path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("uninstall requires a regular SDK directory: %s", path)
	}
	current, err := readSDK(path)
	if err != nil || current.Version != sdk.Version {
		return fmt.Errorf("SDK changed before removal: %s", path)
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	a.ui.heading("Uninstall AIR SDK " + sdk.Version.String())
	a.ui.line("Path: "+path, "muted")
	stop := a.ui.activity("Removing AIR SDK "+sdk.Version.String(), nil)
	err = removeChild(root, path)
	stop()
	if err != nil {
		return fmt.Errorf("cannot completely remove SDK at %s: %w", path, err)
	}
	a.ui.line("Uninstalled AIR SDK "+sdk.Version.String(), "accent")
	return nil
}

// install resolves the requested version, assembles it in a staging directory
// inside the SDK root, and moves it into place in one step, so an interrupted
// run leaves a .asm-install- directory for clean to find rather than a partly
// written version directory that looks installed.
func (a *app) install(o options) error {
	if o.filter == "" {
		return errors.New("usage: asm install [VERSION] [--accept-license]; run asm help install")
	}
	root, err := a.root()
	if err != nil {
		return err
	}
	var v sdkVersion
	if o.filter == "latest" {
		versions, err := a.releases()
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return errors.New("no stable AIR SDK releases found")
		}
		v = versions[0]
	} else {
		parts, err := versionParts(o.filter)
		if err != nil {
			return err
		}
		if len(parts) == 4 {
			v = sdkVersion{parts[0], parts[1], parts[2], parts[3]}
		} else {
			versions, err := a.releases()
			if err != nil {
				return err
			}
			found := false
			for _, candidate := range versions {
				if candidate.matches(parts) {
					v, found = candidate, true
					break
				}
			}
			if !found {
				return fmt.Errorf("no AIR SDK version matches %s; run asm search", o.filter)
			}
		}
	}
	if occupied(root) {
		sdks, err := a.installed()
		if err != nil {
			return err
		}
		for _, sdk := range sdks {
			if sdk.Version == v {
				a.ui.line(fmt.Sprintf("AIR SDK %s is already installed at %s", v, sdk.Path), "")
				return nil
			}
		}
	}
	destination := filepath.Join(root, "AIRSDK_"+v.String())
	if occupied(destination) {
		return fmt.Errorf("installation path is already occupied: %s", destination)
	}
	if !o.license && a.settings["HAS_ACCEPTED_LICENSE"] != "true" {
		return errors.New("accept the AIR SDK license using --accept-license, or use AIR SDK Manager first")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	unlock, err := lockSDKRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	sdks, err := a.installed()
	if err != nil {
		return err
	}
	for _, sdk := range sdks {
		if sdk.Version == v {
			a.ui.line(fmt.Sprintf("AIR SDK %s is already installed at %s", v, sdk.Path), "")
			return nil
		}
	}
	if occupied(destination) {
		return fmt.Errorf("installation path is already occupied: %s", destination)
	}
	stage, err := os.MkdirTemp(root, ".asm-install-")
	if err != nil {
		return err
	}
	defer removeChild(root, stage)
	a.ui.heading("Install AIR SDK " + v.String())
	a.ui.line("Destination: "+destination, "muted")
	if err := a.buildSDK(v, stage); err != nil {
		return err
	}
	if err := os.Chmod(stage, 0755); err != nil {
		return err
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if err := moveNewDirectory(stage, destination); err != nil {
		return err
	}
	a.ui.line("Installed AIR SDK "+v.String(), "accent")
	a.ui.line("Path: "+destination, "muted")
	return nil
}

// replaceSDK builds the new version beside the installed one and keeps the old
// directory until the swap has succeeded, moving it back if the final step
// fails. adt.cfg and adt.lic are carried over, since they hold the license and
// the local configuration that no download can recreate.
func (a *app) replaceSDK(sdk installedSDK, v sdkVersion) error {
	root, err := a.root()
	if err != nil {
		return err
	}
	current, err := childPath(root, sdk.Path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(current)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("SDK updates require a regular directory: %s", current)
	}
	stage, err := os.MkdirTemp(root, ".asm-update-")
	if err != nil {
		return err
	}
	defer removeChild(root, stage)
	if err := a.buildSDK(v, stage); err != nil {
		return err
	}
	for _, name := range []string{"adt.cfg", "adt.lic"} {
		path := filepath.Join(current, "lib", name)
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		metadata, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage, "lib", name), data, metadata.Mode().Perm()); err != nil {
			return err
		}
	}
	original, err := readSDK(current)
	if err != nil || original.Version != sdk.Version {
		return fmt.Errorf("SDK changed while downloading: %s", current)
	}
	if err := os.Chmod(stage, info.Mode().Perm()); err != nil {
		return err
	}
	if err := a.ctx.Err(); err != nil {
		return err
	}
	previous, err := os.MkdirTemp(root, ".asm-old-")
	if err != nil {
		return err
	}
	if err := os.Remove(previous); err != nil {
		return err
	}
	if err := os.Rename(current, previous); err != nil {
		return err
	}
	if err := moveNewDirectory(stage, current); err != nil {
		if occupied(current) {
			return fmt.Errorf("cannot replace SDK: %s; original SDK remains at %s: %w", current, previous, err)
		}
		if restoreErr := moveNewDirectory(previous, current); restoreErr != nil {
			return fmt.Errorf("cannot restore SDK; original SDK remains at %s: %w", previous, restoreErr)
		}
		return err
	}
	if err := removeChild(root, previous); err != nil {
		return fmt.Errorf("SDK updated, but cannot remove temporary old SDK at %s: %w", previous, err)
	}
	a.ui.line(fmt.Sprintf("Updated %s -> %s", sdk.Version, v), "accent")
	a.ui.line("Path: "+current, "muted")
	return nil
}

// update looks for a newer build in the same branch for each installed SDK and
// reports what it found. A bare run and --check stop there; replacing the
// installed SDKs needs a version argument or --all.
func (a *app) update(o options) error {
	var parts []int
	if o.filter != "" {
		var err error
		parts, err = versionParts(o.filter)
		if err != nil {
			return err
		}
	}
	all, err := a.installed()
	if err != nil {
		return err
	}
	var sdks []installedSDK
	for _, sdk := range all {
		if sdk.Version.matches(parts) {
			sdks = append(sdks, sdk)
		}
	}
	if len(sdks) == 0 && o.filter != "" {
		return fmt.Errorf("no installed SDK matches %s; run asm list", o.filter)
	}
	versions, err := a.releases()
	if err != nil {
		return err
	}
	type update struct {
		sdk       installedSDK
		available sdkVersion
	}
	var updates []update
	var rows [][]string
	for _, sdk := range sdks {
		for _, v := range versions {
			if v.branch(sdk.Version) && v.newer(sdk.Version) {
				updates = append(updates, update{sdk, v})
				rows = append(rows, []string{sdk.Version.String(), v.String(), sdk.Path})
				break
			}
		}
	}
	if len(updates) > 0 {
		a.ui.heading("Available updates")
		a.ui.rows([]string{"Installed", "Available", "Path"}, rows)
	} else if len(sdks) > 0 {
		a.ui.line("Installed SDKs are up to date.", "")
	} else {
		a.ui.line("No local AIR SDK versions found.", "")
	}
	if o.filter == "" && len(versions) > 0 {
		latest := versions[0]
		branch := false
		for _, sdk := range sdks {
			if latest.branch(sdk.Version) {
				branch = true
			}
		}
		if !branch && (len(sdks) == 0 || latest.newer(sdks[0].Version)) {
			a.ui.line("", "")
			a.ui.line("New AIR SDK available: "+latest.String(), "accent")
			a.ui.line(fmt.Sprintf("Install: asm install %d.%d", latest[0], latest[1]), "")
		}
	}
	if len(updates) == 0 || o.check || (!o.all && o.filter == "") {
		if len(updates) > 0 && a.ui.interactive {
			action := o.filter
			if action == "" {
				action = "--all"
			}
			a.ui.line("", "")
			a.ui.wrap("Apply: asm update "+action, 2, "accent")
		}
		return nil
	}
	if !o.license && a.settings["HAS_ACCEPTED_LICENSE"] != "true" {
		return errors.New("accept the AIR SDK license using --accept-license, or use AIR SDK Manager first")
	}
	root, err := a.root()
	if err != nil {
		return err
	}
	unlock, err := lockSDKRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	for _, update := range updates {
		if err := a.replaceSDK(update.sdk, update.available); err != nil {
			return err
		}
	}
	return nil
}
