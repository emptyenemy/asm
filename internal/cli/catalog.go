package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
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

// catalogFiles lists the manager catalog and its backups, the newest backup
// first, so a caller can fall through to an older copy when one is unreadable.
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

// cachedBuilds collects the builds the AIR SDK Manager has recorded, skipping
// entries that carry no usable build description.
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

// releaseNumbers collects the production builds into a sorted, de-duplicated
// list. A build whose name is not a full version is reported, not skipped, so a
// malformed catalog is visible instead of quietly short.
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

// fetchAPIReleases reads the production release list from the AIR SDK API. A
// response without a releases array is an error rather than an empty catalog.
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

// The announcement archive is an HTML page of links to dated news posts;
// a post title names the release and marks previews.
var (
	newsLinks    = regexp.MustCompile(`(?is)<a\b[^>]*\bhref="/news/\d{4}/\d{2}/\d{2}/[^"\s]+"[^>]*>(.*?)</a>`)
	htmlTags     = regexp.MustCompile(`<[^>]*>`)
	newsNumber   = regexp.MustCompile(`(?i)\bRelease\s+(\d+\.\d+\.\d+\.\d+)\b`)
	previewTitle = regexp.MustCompile(`(?i)\b(beta|alpha|preview|pre[ -]?release)\b`)
)

// fetchNewsReleases rebuilds the release list from the announcement archive for
// the case where the API cannot be reached. It is a best-effort parse of the
// page titles, so previews are skipped and no recognizable release is an error.
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
