package modules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
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

type httpError struct {
	code            int
	status, address string
}

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %s from %s", e.status, e.address) }

func (a *app) request(ctx context.Context, method, address string) (*http.Response, error) {
	return a.send(ctx, method, address, -1)
}

// send performs a request. A negative offset marks a metadata request; a
// non-negative one is an archive download that continues at that byte.
func (a *app) send(ctx context.Context, method, address string, offset int64) (*http.Response, error) {
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
	if offset >= 0 {
		req.Header.Set("Accept-Encoding", "identity")
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, &httpError{response.StatusCode, response.Status, address}
	}
	return response, nil
}

func (a *app) metadata(address, label string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(a.ctx, timeout)
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

func (a *app) fetchAPIReleases() ([]sdkVersion, error) {
	data, err := a.metadata(a.endpoint()+"/releases?types=production", "Checking AIR SDK releases", a.apiTimeout)
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
		return nil, fmt.Errorf("server error %s", response.ErrorType)
	}
	if response.Releases == nil {
		return nil, errors.New("response has no releases array")
	}
	return releaseNumbers(response.Releases)
}

func (a *app) fetchNewsReleases() ([]sdkVersion, error) {
	data, err := a.metadata(a.newsURL, "Checking AIR SDK releases", 15*time.Second)
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

// releases reads the catalog from the official API, then from the
// announcement archive, then from the AIR SDK Manager database.
func (a *app) releases() ([]sdkVersion, error) {
	sources := []struct {
		name  string
		fetch func() ([]sdkVersion, error)
	}{
		{"AIR SDK API", a.fetchAPIReleases},
		{"announcement archive", a.fetchNewsReleases},
	}
	var failures []string
	for _, source := range sources {
		versions, err := source.fetch()
		if err == nil {
			if len(failures) > 0 {
				a.ui.warning(fmt.Sprintf("%s. Using the %s.", strings.Join(failures, "; "), source.name))
			}
			return versions, nil
		}
		if a.ctx.Err() != nil {
			return nil, a.ctx.Err()
		}
		failures = append(failures, fmt.Sprintf("%s failed: %v", source.name, err))
	}
	for _, file := range a.catalogFiles() {
		cached, cacheErr := releaseNumbers(cachedBuilds(file))
		if cacheErr == nil && len(cached) > 0 {
			a.ui.warning(fmt.Sprintf("%s. Using cached AIR SDK Manager catalog: %s", strings.Join(failures, "; "), file))
			return cached, nil
		}
	}
	return nil, fmt.Errorf("cannot load AIR SDK catalog: %s", strings.Join(failures, "; "))
}

// apiManifest loads the build manifest, preferring one saved by AIR SDK Manager.
func (a *app) apiManifest(v sdkVersion) (manifest, error) {
	_, _, key, err := a.platform()
	if err != nil {
		return manifest{}, err
	}
	for _, file := range a.catalogFiles() {
		for _, build := range cachedBuilds(file) {
			if build.Name == v.String() && build.Type == "production" && (len(build.Components) > 0 || build.URLs[key].Checksum != "") {
				return build, nil
			}
		}
	}
	data, err := a.metadata(a.endpoint()+"/releases/"+v.String()+"?types=production", "Loading the SDK manifest", a.apiTimeout)
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

// mirrorManifest describes the full host archive from the shockpkg catalog.
func (a *app) mirrorManifest(v sdkVersion) (manifest, error) {
	mirrorPlatform, _, key, err := a.platform()
	if err != nil {
		return manifest{}, err
	}
	data, err := a.metadata(a.mirrorURL, "Finding the "+mirrorPlatform+" SDK download", 15*time.Second)
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
