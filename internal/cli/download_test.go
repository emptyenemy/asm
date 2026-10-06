package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// flakyArchive serves data and misbehaves for the first requests.
type flakyArchive struct {
	data        []byte
	statusFirst int // answer this many requests with status
	status      int
	dropFirst   int // send half of the body, then close the connection
	stallFirst  int // send half of the body, then go silent
	ignoreRange bool

	mutex  sync.Mutex
	ranges []string
}

func (s *flakyArchive) requests() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]string(nil), s.ranges...)
}

func (s *flakyArchive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mutex.Lock()
	n := len(s.ranges)
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.mutex.Unlock()
	if n < s.statusFirst {
		w.WriteHeader(s.status)
		return
	}
	start := 0
	if header := r.Header.Get("Range"); header != "" && !s.ignoreRange {
		fmt.Sscanf(header, "bytes=%d-", &start)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(s.data)-1, len(s.data)))
		w.Header().Set("Content-Length", fmt.Sprint(len(s.data)-start))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", fmt.Sprint(len(s.data)))
	}
	body := s.data[start:]
	if n < s.dropFirst || n < s.stallFirst {
		w.Write(body[:len(body)/2])
		w.(http.Flusher).Flush()
		if n < s.stallFirst {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		}
		panic(http.ErrAbortHandler)
	}
	w.Write(body)
}

func archiveData() ([]byte, string) {
	data := bytes.Repeat([]byte("asm download fixture "), 20000)
	hash := sha256.Sum256(data)
	return data, hex.EncodeToString(hash[:])
}

func partialSize(t *testing.T, a *app, checksum string) int64 {
	t.Helper()
	root, _ := a.root()
	info, err := os.Stat(filepath.Join(root, partialDirectory, checksum+".part"))
	if err != nil {
		return -1
	}
	return info.Size()
}

func TestDownloadRetriesAndResumes(t *testing.T) {
	data, checksum := archiveData()
	cases := []struct {
		name     string
		server   *flakyArchive
		requests int
		resumed  bool // the second request continues from a byte offset
	}{
		{"connection drop continues with Range", &flakyArchive{dropFirst: 1}, 2, true},
		{"server error is retried", &flakyArchive{statusFirst: 2, status: 503}, 3, false},
		{"rate limit is retried", &flakyArchive{statusFirst: 1, status: 429}, 2, false},
		{"server without Range restarts", &flakyArchive{dropFirst: 1, ignoreRange: true}, 2, true},
		{"stalled transfer is retried", &flakyArchive{stallFirst: 1}, 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _, _ := testApp(t)
			a.stallTimeout = 300 * time.Millisecond
			c.server.data = data
			server := httptest.NewServer(c.server)
			defer server.Close()
			root, _ := a.root()
			file, err := a.download(archiveInfo{URL: server.URL, Checksum: checksum, Size: int64(len(data))}, "GET", "fixture", root)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(file)
			if !bytes.Equal(got, data) {
				t.Fatal("downloaded data differs")
			}
			requests := c.server.requests()
			if len(requests) != c.requests {
				t.Fatalf("requests %q", requests)
			}
			if c.resumed && c.server.ignoreRange == false && !strings.HasPrefix(requests[1], "bytes=") {
				t.Fatalf("second request did not continue the download: %q", requests)
			}
			os.Remove(file)
			assertClean(t, a)
		})
	}
}

func TestDownloadDoesNotRetryClientErrors(t *testing.T) {
	data, checksum := archiveData()
	a, _, _ := testApp(t)
	archive := &flakyArchive{data: data, statusFirst: 100, status: 403}
	server := httptest.NewServer(archive)
	defer server.Close()
	root, _ := a.root()
	if _, err := a.download(archiveInfo{URL: server.URL, Checksum: checksum, Size: int64(len(data))}, "GET", "fixture", root); err == nil {
		t.Fatal("HTTP 403 accepted")
	}
	if n := len(archive.requests()); n != 1 {
		t.Fatalf("HTTP 403 was retried: %d requests", n)
	}
	assertClean(t, a)
}

func TestDownloadContinuesInTheNextRun(t *testing.T) {
	data, checksum := archiveData()
	a, _, _ := testApp(t)
	archive := &flakyArchive{data: data, dropFirst: downloadAttempts}
	server := httptest.NewServer(archive)
	defer server.Close()
	root, _ := a.root()
	info := archiveInfo{URL: server.URL, Checksum: checksum, Size: int64(len(data))}
	_, err := a.download(info, "GET", "fixture", root)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("after %d attempts", downloadAttempts)) {
		t.Fatalf("expected exhausted retries: %v", err)
	}
	stored := partialSize(t, a, checksum)
	if stored <= 0 || stored >= int64(len(data)) {
		t.Fatalf("partial download holds %d bytes", stored)
	}
	before := len(archive.requests())
	file, err := a.download(info, "GET", "fixture", root)
	if err != nil {
		t.Fatal(err)
	}
	requests := archive.requests()
	if want := fmt.Sprintf("bytes=%d-", stored); requests[before] != want {
		t.Fatalf("next run requested %q, want %q", requests[before], want)
	}
	got, _ := os.ReadFile(file)
	if !bytes.Equal(got, data) {
		t.Fatal("resumed data differs")
	}
	os.Remove(file)
	if partialSize(t, a, checksum) != -1 {
		t.Fatal("partial download remains after success")
	}
	assertClean(t, a)
}

func TestDownloadRestartsDamagedPartial(t *testing.T) {
	data, checksum := archiveData()
	a, _, _ := testApp(t)
	root, _ := a.root()
	directory := filepath.Join(root, partialDirectory)
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, checksum+".part"), bytes.Repeat([]byte("x"), 1000), 0644); err != nil {
		t.Fatal(err)
	}
	archive := &flakyArchive{data: data}
	server := httptest.NewServer(archive)
	defer server.Close()
	file, err := a.download(archiveInfo{URL: server.URL, Checksum: checksum, Size: int64(len(data))}, "GET", "fixture", root)
	if err != nil {
		t.Fatal(err)
	}
	if requests := archive.requests(); len(requests) != 2 || requests[0] != "bytes=1000-" || requests[1] != "" {
		t.Fatalf("requests %q", requests)
	}
	got, _ := os.ReadFile(file)
	if !bytes.Equal(got, data) {
		t.Fatal("downloaded data differs")
	}
}

func TestDownloadRejectsCorruptDataAndDropsIt(t *testing.T) {
	data, _ := archiveData()
	a, _, _ := testApp(t)
	server := httptest.NewServer(&flakyArchive{data: data})
	defer server.Close()
	root, _ := a.root()
	wrong := strings.Repeat("0", 64)
	if _, err := a.download(archiveInfo{URL: server.URL, Checksum: wrong, Size: int64(len(data))}, "GET", "fixture", root); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("corrupt download accepted: %v", err)
	}
	if partialSize(t, a, wrong) != -1 {
		t.Fatal("corrupt data kept for resuming")
	}
	assertClean(t, a)
}

func TestStalePartialDownloadsAreRemoved(t *testing.T) {
	data, checksum := archiveData()
	a, _, _ := testApp(t)
	root, _ := a.root()
	directory := filepath.Join(root, partialDirectory)
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(directory, strings.Repeat("1", 64)+".part")
	if err := os.WriteFile(stale, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-partialMaxAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(&flakyArchive{data: data})
	defer server.Close()
	file, err := a.download(archiveInfo{URL: server.URL, Checksum: checksum, Size: int64(len(data))}, "GET", "fixture", root)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(file)
	if occupied(stale) {
		t.Fatal("abandoned partial download remains")
	}
}

func sdkArchive(t *testing.T, a *app) []byte {
	t.Helper()
	adt := "bin/adt"
	if a.os == "windows" {
		adt += ".bat"
	}
	files := map[string]string{adt: "fixture", "lib/adt.jar": strings.Repeat("fixture", 12000), "lib/adt.cfg": "new config"}
	if a.os == "linux" {
		files["bin/configure_linux.sh"] = "#!/bin/sh\nexit 0\n"
	}
	return fixtureZIP(t, files)
}

func TestInstallFallsBackToMirror(t *testing.T) {
	for _, failure := range []string{"manifest", "components"} {
		t.Run(failure, func(t *testing.T) {
			a, out, stderr := testApp(t)
			archive := sdkArchive(t, a)
			hash := sha256.Sum256(archive)
			checksum := hex.EncodeToString(hash[:])
			mirror, component, _, _ := a.platform()
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/mirror":
					json.NewEncoder(w).Encode(map[string]any{"packages": []map[string]any{{"name": "air-sdk-51.4.1.1-" + mirror + "-compiler", "source": server.URL + "/archive", "sha256": checksum, "size": len(archive)}}})
				case r.URL.Path == "/archive":
					w.Write(archive)
				case r.URL.Path == "/releases/51.4.1.1" && failure == "manifest":
					w.WriteHeader(502)
				case r.URL.Path == "/releases/51.4.1.1":
					json.NewEncoder(w).Encode(manifest{Name: "51.4.1.1", Type: "production", Components: map[string]archiveInfo{component: {Version: "1", Checksum: checksum, Size: int64(len(archive))}}})
				default:
					w.WriteHeader(403)
				}
			}))
			defer server.Close()
			a.apiURL, a.mirrorURL = server.URL, server.URL+"/mirror"
			if err := a.run([]string{"install", "51.4.1.1"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Installed AIR SDK 51.4.1.1") || !strings.Contains(stderr.String(), "AIR SDK API failed") || !strings.Contains(stderr.String(), "Trying the shockpkg mirror") {
				t.Fatalf("out %s\nerr %s", out, stderr)
			}
			assertClean(t, a)
		})
	}
}

func TestInstallReportsEverySourceFailure(t *testing.T) {
	a, _, _ := testApp(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer server.Close()
	a.apiURL, a.mirrorURL = server.URL, server.URL+"/mirror"
	err := a.run([]string{"install", "51.4.1.1"})
	if err == nil || !strings.Contains(err.Error(), "AIR SDK API") || !strings.Contains(err.Error(), "shockpkg mirror") {
		t.Fatalf("error does not name both sources: %v", err)
	}
	assertClean(t, a)
}
