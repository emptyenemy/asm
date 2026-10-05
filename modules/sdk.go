package modules

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type archiveInfo struct {
	URL      string `json:"url"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"fileSize"`
	Version  string `json:"version"`
}
type manifest struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Components map[string]archiveInfo `json:"components"`
	URLs       map[string]archiveInfo `json:"urls"`
}

func (a *app) endpoint() string {
	if a.settings == nil {
		_ = a.loadSettings()
	}
	return strings.TrimRight(a.settings["API_ENDPOINT"], "/")
}

func (a *app) request(ctx context.Context, method, address string) (*http.Response, error) {
	var body io.Reader
	if method == "POST" {
		body = strings.NewReader("acceptedLicense=true")
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "asm/"+a.version)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("HTTP %s from %s", response.Status, address)
	}
	return response, nil
}

func (a *app) metadata(address, label string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	stop := a.ui.activity(label, nil)
	defer stop()
	response, err := a.request(ctx, "GET", address)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024+1))
	if len(data) > 16*1024*1024 {
		return nil, errors.New("SDK metadata response is too large")
	}
	return data, err
}

func (a *app) catalogFiles() []string {
	directory := filepath.Dir(a.configFile)
	files := []string{filepath.Join(directory, "airsdkmanager.db")}
	backups, _ := filepath.Glob(filepath.Join(directory, "airsdkmanager.db.backup*"))
	sort.Slice(backups, func(i, j int) bool {
		a, ea := os.Stat(backups[i])
		b, eb := os.Stat(backups[j])
		return ea == nil && (eb != nil || a.ModTime().After(b.ModTime()))
	})
	return append(files, backups...)
}

func cachedBuilds(path string) []manifest {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	type sdk struct {
		Build json.RawMessage `json:"build"`
	}
	var catalog struct {
		Available   []sdk `json:"availableSDKs"`
		Latest      []sdk `json:"latestSDKs"`
		Installable []sdk `json:"installableSDKs"`
	}
	if json.Unmarshal(data, &catalog) != nil {
		return nil
	}
	var builds []manifest
	for _, field := range [][]sdk{catalog.Available, catalog.Latest, catalog.Installable} {
		for _, sdk := range field {
			var build manifest
			if err := json.Unmarshal(sdk.Build, &build); err != nil {
				var identity struct {
					Name string `json:"name"`
					Type string `json:"type"`
				}
				if json.Unmarshal(sdk.Build, &identity) != nil {
					continue
				}
				build = manifest{Name: identity.Name, Type: identity.Type}
			}
			if build.Name != "" {
				builds = append(builds, build)
			}
		}
	}
	return builds
}

func releaseNumbers(builds []manifest) ([]sdkVersion, error) {
	var result []sdkVersion
	seen := make(map[sdkVersion]bool)
	for _, build := range builds {
		if build.Type != "" && build.Type != "production" {
			continue
		}
		v, err := parseVersion(build.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid version in AIR SDK catalog: %s", build.Name)
		}
		if !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].newer(result[j]) })
	return result, nil
}

func (a *app) fetchReleases() ([]sdkVersion, error) {
	if endpoint := a.endpoint(); endpoint != "" {
		data, err := a.metadata(endpoint+"/releases?types=production", "Checking AIR SDK releases")
		if err != nil {
			return nil, err
		}
		var response struct {
			ErrorType string     `json:"errorType"`
			Releases  []manifest `json:"releases"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, err
		}
		if response.ErrorType != "" {
			return nil, fmt.Errorf("AIR SDK API returned %s", response.ErrorType)
		}
		if response.Releases == nil {
			return nil, errors.New("AIR SDK API response has no releases array")
		}
		return releaseNumbers(response.Releases)
	}
	data, err := a.metadata(a.newsURL, "Checking AIR SDK releases")
	if err != nil {
		return nil, err
	}
	var builds []manifest
	for _, link := range newsLinks.FindAllStringSubmatch(string(data), -1) {
		title := html.UnescapeString(htmlTags.ReplaceAllString(link[1], ""))
		match := newsNumber.FindStringSubmatch(title)
		if len(match) == 0 || previewTitle.MatchString(title) {
			continue
		}
		if match[1] == "51.0.0.2" || match[1] == "51.0.0.4" {
			continue
		}
		builds = append(builds, manifest{Name: match[1], Type: "production"})
	}
	if len(builds) == 0 {
		return nil, errors.New("the AIR SDK announcement archive contains no recognized releases")
	}
	return releaseNumbers(builds)
}

func (a *app) releases() ([]sdkVersion, error) {
	versions, err := a.fetchReleases()
	if err == nil {
		return versions, nil
	}
	if a.ctx.Err() != nil {
		return nil, a.ctx.Err()
	}
	for _, file := range a.catalogFiles() {
		cached, cacheErr := releaseNumbers(cachedBuilds(file))
		if cacheErr == nil && len(cached) > 0 {
			a.ui.warning(fmt.Sprintf("%v. Using cached AIR SDK Manager catalog: %s", err, file))
			return cached, nil
		}
	}
	return nil, fmt.Errorf("cannot load AIR SDK catalog: %w", err)
}

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

func (a *app) sdkManifest(v sdkVersion) (manifest, error) {
	mirrorPlatform, _, key, err := a.platform()
	if err != nil {
		return manifest{}, err
	}
	if endpoint := a.endpoint(); endpoint != "" {
		for _, file := range a.catalogFiles() {
			for _, build := range cachedBuilds(file) {
				if build.Name == v.String() && build.Type == "production" && (len(build.Components) > 0 || build.URLs[key].Checksum != "") {
					return build, nil
				}
			}
		}
		data, err := a.metadata(endpoint+"/releases/"+v.String()+"?types=production", "Loading the SDK manifest")
		if err != nil {
			return manifest{}, err
		}
		var build manifest
		if err := json.Unmarshal(data, &build); err != nil {
			return build, err
		}
		if build.Name != v.String() || build.Type != "production" {
			return build, fmt.Errorf("invalid manifest for AIR SDK %s", v)
		}
		return build, nil
	}
	data, err := a.metadata(a.mirrorURL, "Finding the "+mirrorPlatform+" SDK download")
	if err != nil {
		return manifest{}, err
	}
	var catalog struct {
		Packages []struct {
			Name, Source, SHA256 string
			Size                 int64
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return manifest{}, err
	}
	if catalog.Packages == nil {
		return manifest{}, errors.New("invalid shockpkg SDK catalog")
	}
	for _, pkg := range catalog.Packages {
		if pkg.Name == "air-sdk-"+v.String()+"-"+mirrorPlatform+"-compiler" {
			a.ui.notice("Using the shockpkg download mirror with SHA-256 verification.")
			return manifest{Name: v.String(), Type: "production", URLs: map[string]archiveInfo{key: {URL: pkg.Source, Checksum: pkg.SHA256, Size: pkg.Size}}}, nil
		}
	}
	return manifest{}, fmt.Errorf("no %s SDK download found for %s", mirrorPlatform, v)
}

func (a *app) download(info archiveInfo, method, name, destination string) (file string, err error) {
	info.Checksum = strings.TrimSpace(info.Checksum)
	checksum, err := hex.DecodeString(strings.TrimSpace(info.Checksum))
	if err != nil || len(checksum) != sha256.Size {
		return "", fmt.Errorf("invalid SHA-256 for %s", name)
	}
	ctx, cancel := context.WithTimeout(a.ctx, 600*time.Second)
	defer cancel()
	progress := &transfer{started: time.Now()}
	progress.total.Store(info.Size)
	if a.ui.interactive {
		a.ui.notice("Downloading " + name)
	}
	stop := a.ui.activity("Downloading "+name, progress)
	defer stop()
	response, err := a.request(ctx, method, info.URL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if info.Size <= 0 {
		progress.total.Store(response.ContentLength)
	}
	output, err := os.CreateTemp(destination, ".asm-download-*.zip")
	if err != nil {
		return "", err
	}
	file = output.Name()
	defer func() {
		output.Close()
		if err != nil {
			_ = os.Remove(file)
		}
	}()
	hash := sha256.New()
	count, err := io.Copy(io.MultiWriter(output, hash, progress), response.Body)
	if err != nil {
		return file, err
	}
	if err = output.Close(); err != nil {
		return file, err
	}
	if info.Size > 0 && count != info.Size {
		return file, fmt.Errorf("download size mismatch for %s", name)
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), info.Checksum) {
		return file, fmt.Errorf("SHA-256 mismatch for %s", name)
	}
	return file, nil
}

func childPath(root, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path is outside the SDK directory: %s", path)
	}
	return absolute, nil
}

func removeChild(root, path string) error {
	safe, err := childPath(root, path)
	if err != nil {
		return err
	}
	return os.RemoveAll(safe)
}

func regularParents(root, target string) error {
	for path := target; path != root; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ZIP entry traverses a symbolic link: %s", path)
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(data)
}

func (a *app) extract(file, destination string) error {
	defer os.Remove(file)
	stop := a.ui.activity("Extracting the SDK", nil)
	defer stop()
	archive, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer archive.Close()
	type link struct{ path, target string }
	var links []link
	for _, entry := range archive.File {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		if strings.Contains(name, ":") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("invalid ZIP entry: %s", name)
		}
		target, err := childPath(destination, filepath.Join(destination, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if err := regularParents(destination, target); err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			if a.os == "windows" {
				return fmt.Errorf("unexpected symbolic link in a Windows SDK: %s", name)
			}
			source, err := entry.Open()
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(source, 8193))
			source.Close()
			if err != nil {
				return err
			}
			text := string(data)
			if len(data) > 8192 || text == "" || strings.ContainsAny(text, "\x00:") || strings.HasPrefix(text, "/") || strings.HasPrefix(text, "\\") {
				return fmt.Errorf("invalid ZIP symbolic link: %s", name)
			}
			if _, err := childPath(destination, filepath.Join(filepath.Dir(target), filepath.FromSlash(strings.ReplaceAll(text, "\\", "/")))); err != nil {
				return err
			}
			links = append(links, link{target, text})
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("unsupported ZIP entry: %s", name)
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		mode := entry.Mode().Perm()
		if mode == 0 {
			mode = 0644
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(output, contextReader{a.ctx, source})
		closeErr := output.Close()
		source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Chmod(target, mode); err != nil {
			return err
		}
	}
	for _, item := range links {
		if err := regularParents(destination, filepath.Dir(item.path)); err != nil {
			return err
		}
		if target, err := os.Readlink(item.path); err == nil && target == item.target {
			continue
		}
		if err := os.Symlink(item.target, item.path); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) buildSDK(v sdkVersion, destination string) error {
	build, err := a.sdkManifest(v)
	if err != nil {
		return err
	}
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
		if endpoint == "" {
			endpoint = "https://api.airsdk.harman.com"
		}
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
	stop := a.ui.activity("Configuring the SDK", nil)
	err = configureSDK(a.ctx, destination, a.arch)
	stop()
	if err != nil {
		return err
	}
	number := fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	description := fmt.Sprintf("<air-sdk-description><name>AIR %s</name><version>%s</version><build>%d</build></air-sdk-description>\n", number, number, v[3])
	return os.WriteFile(filepath.Join(destination, "air-sdk-description.xml"), []byte(description), 0644)
}

func occupied(path string) bool { _, err := os.Lstat(path); return !errors.Is(err, os.ErrNotExist) }

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

func (a *app) install(o options) error {
	if o.filter == "" {
		return errors.New("usage: asm install <VERSION> [--accept-license]; run asm help install")
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
