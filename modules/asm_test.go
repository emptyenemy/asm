package modules

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, text := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0755)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func testApp(t *testing.T) (*app, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, stderr bytes.Buffer
	ui := &terminal{out: &out, err: &stderr, width: func() int { return 80 }}
	a := newApp(context.Background(), ui, "1.0.0")
	a.configFile = filepath.Join(t.TempDir(), ".airsdk", "airsdkmanager.cfg")
	// Closed local port: a source nobody tests against fails fast instead of reaching the network.
	closed := "http://127.0.0.1:1"
	a.apiURL, a.newsURL, a.mirrorURL = closed, closed, closed
	a.apiTimeout, a.retryDelay, a.stallTimeout = 2*time.Second, time.Millisecond, 2*time.Second
	a.settings = map[string]string{"AIR_SDKS": filepath.Join(t.TempDir(), "SDKs [local] пробел"), "HAS_ACCEPTED_LICENSE": "true"}
	if err := os.MkdirAll(a.settings["AIR_SDKS"], 0755); err != nil {
		t.Fatal(err)
	}
	return a, &out, &stderr
}

func fixtureInstalled(t *testing.T, a *app, number string) string {
	t.Helper()
	root, err := a.root()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "arbitrary_"+number)
	if err := os.MkdirAll(filepath.Join(path, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	v, err := parseVersion(number)
	if err != nil {
		t.Fatal(err)
	}
	description := fmt.Sprintf(`<air-sdk-description xmlns="urn:fixture"><version>%d.%d.%d</version><build>%d</build></air-sdk-description>`, v[0], v[1], v[2], v[3])
	if err := os.WriteFile(filepath.Join(path, "air-sdk-description.xml"), []byte(description), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "lib", "adt.cfg"), []byte("original config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "lib", "adt.lic"), []byte("original license"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertClean(t *testing.T, a *app) {
	t.Helper()
	root, err := a.root()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".asm-") {
			t.Errorf("temporary file remains: %s", entry.Name())
		}
	}
}

func TestVersionOrderingAndFilters(t *testing.T) {
	a, out, _ := testApp(t)
	for _, v := range []string{"50.2.5.1", "51.3.4.3", "51.4.1.1", "51.3.4.12"} {
		fixtureInstalled(t, a, v)
	}
	if err := a.run([]string{"ls"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	for i, v := range []string{"51.4.1.1", "51.3.4.12", "51.3.4.3", "50.2.5.1"} {
		if !strings.HasPrefix(lines[i], v) {
			t.Errorf("wrong order: %s", out)
		}
	}
	v, _ := parseVersion("51.40.1.1")
	if v.matches([]int{51, 4}) {
		t.Fatal("51.4 matches 51.40")
	}
	for _, value := range []string{"", "51..4", "51.-4", "51.4.1.1.1", "no", "9999999999999999"} {
		if _, err := versionParts(value); err == nil {
			t.Errorf("accepted invalid version %q", value)
		}
	}
}

func TestCommandsAndVersionOutput(t *testing.T) {
	a, out, stderr := testApp(t)
	for _, flag := range []string{"--version", "-v"} {
		out.Reset()
		if err := a.run([]string{flag}); err != nil {
			t.Fatal(err)
		}
		if out.String() != "1.0.0\n" || stderr.Len() != 0 {
			t.Fatalf("version output: %q %q", out, stderr)
		}
	}
	for _, args := range [][]string{{"install"}, {"update", "51", "--all"}, {"list", "extra"}, {"search", "bad"}, {"install", "51", "--json"}, {"help", "no"}, {"unknown"}} {
		if err := a.run(args); err == nil {
			t.Errorf("accepted invalid arguments: %v", args)
		}
	}
	for _, args := range [][]string{nil, {"help"}, {"help", "update"}, {"search", "-h"}, {"install", "--help"}} {
		if err := a.run(args); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerSettings(t *testing.T) {
	a, _, _ := testApp(t)
	root := a.settings["AIR_SDKS"]
	a.settings = nil
	if err := os.MkdirAll(filepath.Dir(a.configFile), 0755); err != nil {
		t.Fatal(err)
	}
	text := "\ufeff# comment\r\nAIR_SDKS=old\r\nAIR_SDKS=\"" + root + "\"\r\nHAS_ACCEPTED_LICENSE=true\r\n"
	if err := os.WriteFile(a.configFile, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := a.root()
	if err != nil || got != root {
		t.Fatalf("root %q: %v", got, err)
	}
	if a.settings["HAS_ACCEPTED_LICENSE"] != "true" {
		t.Fatal("license setting")
	}
}

func fixtureAPI(t *testing.T, a *app, recipe string) func() []string {
	t.Helper()
	adt := "bin/adt"
	if a.os == "windows" {
		adt += ".bat"
	}
	files := map[string]string{adt: "fixture", "lib/adt.jar": strings.Repeat("fixture", 12000), "lib/adt.cfg": "new config"}
	if a.os == "linux" {
		files["bin/configure_linux.sh"] = "#!/bin/sh\nexit 0\n"
	}
	archive := fixtureZIP(t, files)
	if recipe == "traversal" {
		archive = fixtureZIP(t, map[string]string{"../escaped.txt": "bad"})
	}
	if recipe == "invalid" {
		archive = fixtureZIP(t, map[string]string{"readme.txt": "bad"})
	}
	hash := sha256.Sum256(archive)
	checksum := hex.EncodeToString(hash[:])
	if recipe == "hash" {
		checksum = strings.Repeat("0", 64)
	}
	size := int64(len(archive))
	if recipe == "size" {
		size++
	}
	var requests []string
	var mutex sync.Mutex
	_, component, key, _ := a.platform()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		requests = append(requests, r.URL.Path)
		mutex.Unlock()
		if r.URL.Path == "/releases" {
			json.NewEncoder(w).Encode(map[string]any{"releases": []manifest{{Name: "51.4.1.1", Type: "production"}, {Name: "51.3.4.3", Type: "production"}, {Name: "51.3.3.4", Type: "production"}, {Name: "51.4.2.2", Type: "beta"}}})
			return
		}
		if r.URL.Path == "/archive" || strings.HasPrefix(r.URL.Path, "/releases/components/") {
			if recipe == "http" {
				w.WriteHeader(403)
				return
			}
			if r.Method == "POST" {
				data, _ := io.ReadAll(r.Body)
				if string(data) != "acceptedLicense=true" {
					w.WriteHeader(400)
					return
				}
			}
			w.Write(archive)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/releases/") {
			build := manifest{Name: strings.TrimPrefix(r.URL.Path, "/releases/"), Type: "production", URLs: map[string]archiveInfo{key: {URL: server.URL + "/archive", Checksum: checksum, Size: size}}}
			if recipe == "components" {
				build.URLs = nil
				build.Components = map[string]archiveInfo{component: {Version: "1", Checksum: checksum, Size: size}, "core-tools": {Version: "1", Checksum: checksum, Size: size}}
				for _, other := range []string{"window", "linux", "macos"} {
					if other != component {
						build.Components[other] = archiveInfo{Version: "unwanted"}
					}
				}
			}
			json.NewEncoder(w).Encode(build)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(server.Close)
	a.settings["API_ENDPOINT"] = server.URL
	return func() []string { mutex.Lock(); defer mutex.Unlock(); return append([]string(nil), requests...) }
}

func TestInstallAndUpdate(t *testing.T) {
	for _, request := range []string{"51.4", "51.4.1.1", "latest"} {
		t.Run(request, func(t *testing.T) {
			a, out, _ := testApp(t)
			fixtureAPI(t, a, "normal")
			if err := a.run([]string{"install", request}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Installed AIR SDK 51.4.1.1") {
				t.Fatal(out)
			}
			out.Reset()
			a.settings["HAS_ACCEPTED_LICENSE"] = "false"
			if err := a.run([]string{"install", "51.4.1.1"}); err != nil || !strings.Contains(out.String(), "already installed") {
				t.Fatalf("idempotent: %v %s", err, out)
			}
			assertClean(t, a)
		})
	}
	a, out, _ := testApp(t)
	original := fixtureInstalled(t, a, "51.3.4.1")
	fixtureAPI(t, a, "normal")
	if err := a.run([]string{"update"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "51.3.4.3") || !strings.Contains(out.String(), "New AIR SDK available: 51.4.1.1") {
		t.Fatal(out)
	}
	before, _ := readSDK(original)
	if before.Version.String() != "51.3.4.1" {
		t.Fatal("preview modified SDK")
	}
	if err := a.run([]string{"update", "51.3"}); err != nil {
		t.Fatal(err)
	}
	after, err := readSDK(original)
	if err != nil || after.Version.String() != "51.3.4.3" {
		t.Fatalf("update: %v %v", after, err)
	}
	for _, name := range []string{"adt.cfg", "adt.lic"} {
		data, _ := os.ReadFile(filepath.Join(original, "lib", name))
		if !strings.HasPrefix(string(data), "original ") {
			t.Fatalf("lost %s", name)
		}
	}
	assertClean(t, a)
}

func TestFailedDownloadsPreserveSDK(t *testing.T) {
	for _, recipe := range []string{"http", "hash", "size", "traversal", "invalid"} {
		t.Run(recipe, func(t *testing.T) {
			a, _, _ := testApp(t)
			original := fixtureInstalled(t, a, "51.3.4.1")
			fixtureAPI(t, a, recipe)
			if err := a.run([]string{"update", "51.3"}); err == nil {
				t.Fatal("invalid archive accepted")
			}
			sdk, err := readSDK(original)
			if err != nil || sdk.Version.String() != "51.3.4.1" {
				t.Fatalf("original changed: %v %v", sdk, err)
			}
			assertClean(t, a)
			if occupied(filepath.Join(a.settings["AIR_SDKS"], "escaped.txt")) {
				t.Fatal("ZIP escaped staging directory")
			}
		})
	}
}

func TestPlatformComponentsAndLicense(t *testing.T) {
	a, _, _ := testApp(t)
	requests := fixtureAPI(t, a, "components")
	a.settings["HAS_ACCEPTED_LICENSE"] = "false"
	if err := a.run([]string{"install", "51.4.1.1"}); err == nil {
		t.Fatal("license bypassed")
	}
	if err := a.run([]string{"install", "51.4.1.1", "--accept-license"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range requests() {
		if strings.Contains(path, "unwanted") {
			t.Fatal("downloaded another OS component")
		}
	}
	assertClean(t, a)
	for _, target := range []struct{ os, arch, mirror, component, key string }{{"windows", "amd64", "windows", "window", "AIR_Win"}, {"darwin", "arm64", "mac", "macos", "AIR_Mac"}, {"linux", "arm64", "linux", "linux", "AIR_Linux"}} {
		a.os, a.arch = target.os, target.arch
		mirror, component, key, err := a.platform()
		if err != nil || mirror != target.mirror || component != target.component || key != target.key {
			t.Fatalf("platform %v: %s %s %s %v", target, mirror, component, key, err)
		}
	}
	a.os, a.arch = "windows", "arm64"
	if _, _, _, err := a.platform(); err == nil {
		t.Fatal("accepted unavailable native SDK host")
	}
}

func TestCatalogFallbackAndNews(t *testing.T) {
	a, out, stderr := testApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/news") {
			io.WriteString(w, `<a href="/news/2026/09/23/release">AIR SDK Release 51.4.1.1</a><a href="/news/2026/09/20/beta">Release 51.5.1.1 beta</a><a href="/news/2026/09/10/preview">Release 51.0.0.2</a>`)
			return
		}
		io.WriteString(w, `{"errorType":"Sandbox.Timedout"}`)
	}))
	defer server.Close()
	a.newsURL = server.URL + "/news"
	if err := a.run([]string{"search", "51.4"}); err != nil || out.String() != "51.4.1.1\n" || !strings.Contains(stderr.String(), "Using the announcement archive") {
		t.Fatalf("news: %v %s %s", err, out, stderr)
	}
	stderr.Reset()
	a.settings["API_ENDPOINT"] = server.URL
	a.newsURL = "http://127.0.0.1:1"
	if err := os.MkdirAll(filepath.Dir(a.configFile), 0755); err != nil {
		t.Fatal(err)
	}
	cached := `{"latestSDKs":[{"build":{"name":"51.4.1.1","type":"production"}}],"unrelated":{"value":true}}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(a.configFile), "airsdkmanager.db"), []byte(cached), 0644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := a.run([]string{"search", "51.4"}); err != nil || out.String() != "51.4.1.1\n" || !strings.Contains(stderr.String(), "Using cached") {
		t.Fatalf("fallback: %v %s %s", err, out, stderr)
	}
}

func TestLockAndDestinationProtection(t *testing.T) {
	a, _, _ := testApp(t)
	root, _ := a.root()
	unlock, err := lockSDKRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockSDKRoot(root); err == nil {
		second()
		t.Error("concurrent writer acquired lock")
	}
	unlock()
	assertClean(t, a)
	source, err := os.MkdirTemp(root, ".asm-stage-")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "occupied")
	if err := os.Mkdir(destination, 0755); err != nil {
		t.Fatal(err)
	}
	if err := moveNewDirectory(source, destination); err == nil {
		t.Fatal("replaced an existing directory")
	}
}

func TestCancellationCleanup(t *testing.T) {
	a, _, _ := testApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	a.ctx = ctx
	root, _ := a.root()
	checksum := strings.Repeat("0", 64)
	_, err := a.download(archiveInfo{URL: server.URL, Checksum: checksum, Size: 100000}, "GET", "fixture", root)
	if err == nil {
		t.Fatal("canceled download succeeded")
	}
	// Only the partial download stays, so that the next run can continue it.
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".asm-") && entry.Name() != partialDirectory {
			t.Errorf("temporary file remains: %s", entry.Name())
		}
	}
	data, err := os.ReadFile(filepath.Join(root, partialDirectory, checksum+".part"))
	if err != nil || string(data) != "first" {
		t.Fatalf("partial download: %q %v", data, err)
	}
}

func TestZIPPathsAndUnixLinks(t *testing.T) {
	for _, name := range []string{"../escape", `..\escape`, "/absolute", "C:/drive", "lib/../../escape", "file:stream"} {
		t.Run(name, func(t *testing.T) {
			a, _, _ := testApp(t)
			root, _ := a.root()
			file := filepath.Join(t.TempDir(), "fixture.zip")
			if err := os.WriteFile(file, fixtureZIP(t, map[string]string{name: "bad"}), 0600); err != nil {
				t.Fatal(err)
			}
			if err := a.extract(file, root); err == nil {
				t.Fatal("unsafe ZIP accepted")
			}
			if occupied(file) {
				t.Fatal("archive retained after extraction error")
			}
		})
	}
	if runtime.GOOS == "windows" {
		return
	}
	a, _, _ := testApp(t)
	root, _ := a.root()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	regular, _ := writer.Create("bin/real")
	io.WriteString(regular, "tool")
	header := &zip.FileHeader{Name: "bin/tool"}
	header.SetMode(os.ModeSymlink | 0777)
	link, _ := writer.CreateHeader(header)
	io.WriteString(link, "real")
	writer.Close()
	file := filepath.Join(t.TempDir(), "fixture.zip")
	os.WriteFile(file, buffer.Bytes(), 0600)
	if err := a.extract(file, root); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(root, "bin", "tool")); err != nil || target != "real" {
		t.Fatalf("symlink: %s %v", target, err)
	}
}

func TestLinuxArchitectureSetup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux configuration")
	}
	directory := t.TempDir()
	for _, path := range []string{"bin", "lib/linux_arm64", "lib/nai/bin", "runtimes/air/linux"} {
		if err := os.MkdirAll(filepath.Join(directory, path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"bin/configure_linux.sh":                    "#!/bin/sh\nprintf '%s' \"$1\" > configuration-architecture\n",
		"bin/adt":                                   "#!/bin/sh\n",
		"lib/FlashRuntimeExtensions.so":             "x64 extension",
		"lib/linux_arm64/FlashRuntimeExtensions.so": "arm64 extension",
		"lib/nai/bin/naip":                          "legacy tool",
	}
	for path, text := range files {
		if err := os.WriteFile(filepath.Join(directory, path), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := configureSDK(context.Background(), directory, "arm64"); err != nil {
		t.Fatal(err)
	}
	architecture, _ := os.ReadFile(filepath.Join(directory, "configuration-architecture"))
	if string(architecture) != "arm64" {
		t.Fatalf("configured wrong architecture: %q", architecture)
	}
	for _, path := range []string{"lib/FlashRuntimeExtensions_linux64.so", "lib/FlashRuntimeExtensions_linux_arm64.so", "runtimes/air/linux-x64"} {
		if !occupied(filepath.Join(directory, path)) {
			t.Errorf("missing platform correction: %s", path)
		}
	}
	if occupied(filepath.Join(directory, "lib/nai/bin/naip")) {
		t.Error("legacy naip retained")
	}
	info, err := os.Stat(filepath.Join(directory, "bin/adt"))
	if err != nil || info.Mode().Perm()&0111 == 0 {
		t.Fatal("adt is not executable")
	}
}

func TestUninstallSelectionAndCleanup(t *testing.T) {
	a, out, _ := testApp(t)
	first := fixtureInstalled(t, a, "51.3.4.1")
	second := fixtureInstalled(t, a, "51.3.3.4")
	latest := fixtureInstalled(t, a, "51.4.1.1")
	if err := a.run([]string{"uninstall", "51.3"}); err == nil || !strings.Contains(err.Error(), "multiple SDKs") {
		t.Fatalf("ambiguous removal: %v", err)
	}
	for _, path := range []string{first, second, latest} {
		if !occupied(path) {
			t.Fatal("ambiguity removed an SDK")
		}
	}
	if err := a.run([]string{"uninstall", "51.3.4.1"}); err != nil {
		t.Fatal(err)
	}
	if occupied(first) || !occupied(second) || !occupied(latest) {
		t.Fatal("removed wrong SDK")
	}
	if err := a.run([]string{"remove", "51.4"}); err != nil {
		t.Fatal(err)
	}
	if occupied(latest) || !occupied(second) {
		t.Fatal("short version or alias removed wrong SDK")
	}
	if !strings.Contains(out.String(), "Uninstalled AIR SDK 51.4.1.1") {
		t.Fatal(out)
	}
	for _, args := range [][]string{{"uninstall"}, {"uninstall", "latest"}, {"uninstall", "51.4"}, {"remove", "--all"}, {"remove", "51.3", "extra"}} {
		if err := a.run(args); err == nil {
			t.Errorf("accepted invalid removal: %v", args)
		}
	}
	assertClean(t, a)
}
