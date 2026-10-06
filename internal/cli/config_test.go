package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstRunCreatesSettings(t *testing.T) {
	a, out, stderr := testApp(t)
	a.settings = nil
	home := filepath.Dir(filepath.Dir(a.configFile))
	if err := a.run([]string{"list"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(a.configFile)
	want := "AIR_SDKS=" + filepath.Join(home, "sdks", "air")
	if err != nil || strings.TrimSpace(string(data)) != want {
		t.Fatalf("settings %q, want %q: %v", data, want, err)
	}
	if !strings.Contains(out.String(), "No local AIR SDK versions found.") || !strings.Contains(stderr.String(), "SDK folder:") {
		t.Fatalf("output %q %q", out, stderr)
	}
	stderr.Reset()
	if err := a.run([]string{"list"}); err != nil || stderr.Len() != 0 {
		t.Fatalf("second run: %v %q", err, stderr)
	}
}

func TestSettingsKeepTheManagersLines(t *testing.T) {
	a, _, _ := testApp(t)
	a.settings = nil
	if err := os.MkdirAll(filepath.Dir(a.configFile), 0755); err != nil {
		t.Fatal(err)
	}
	original := "\ufeff# kept\r\nAPI_ENDPOINT=https://example.test\r\nHAS_ACCEPTED_LICENSE=false\r\nCUSTOM=1"
	if err := os.WriteFile(a.configFile, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := a.root()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.saveSetting("HAS_ACCEPTED_LICENSE", "true"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(a.configFile)
	want := "\ufeff# kept\r\nAPI_ENDPOINT=https://example.test\r\nHAS_ACCEPTED_LICENSE=true\r\nCUSTOM=1\r\nAIR_SDKS=" + root + "\r\n"
	if string(data) != want {
		t.Fatalf("settings %q, want %q", data, want)
	}
	if a.endpoint() != "https://example.test" {
		t.Fatalf("endpoint %q", a.endpoint())
	}
}

func TestLicenseQuestion(t *testing.T) {
	for _, c := range []struct {
		answer   string
		accepted bool
	}{{"y\n", true}, {"yes\n", true}, {"\n", false}, {"no\n", false}} {
		t.Run(strings.TrimSpace(c.answer), func(t *testing.T) {
			a, _, stderr := testApp(t)
			fixtureAPI(t, a, "normal")
			delete(a.settings, "HAS_ACCEPTED_LICENSE")
			a.ui.in = strings.NewReader(c.answer)
			err := a.run([]string{"install", "51.4.1.1"})
			if (err == nil) != c.accepted {
				t.Fatalf("install: %v", err)
			}
			if !strings.Contains(stderr.String(), "Accept the AIR SDK license?") {
				t.Fatalf("no question: %q", stderr)
			}
			data, _ := os.ReadFile(a.configFile)
			if strings.Contains(string(data), "HAS_ACCEPTED_LICENSE=true") != c.accepted {
				t.Fatalf("saved settings %q", data)
			}
			sdks, err := a.installed()
			if err != nil || (len(sdks) == 1) != c.accepted {
				t.Fatalf("installed %v: %v", sdks, err)
			}
			assertClean(t, a)
		})
	}
}
