package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureLeftover(t *testing.T, a *app, name string) string {
	t.Helper()
	root, err := a.root()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCleanRemovesLeftovers(t *testing.T) {
	a, out, _ := testApp(t)
	root, _ := a.root()
	partial := filepath.Join(root, partialDirectory)
	if err := os.MkdirAll(partial, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, strings.Repeat("a", 64)+".part"), []byte("half a download"), 0644); err != nil {
		t.Fatal(err)
	}
	install := fixtureLeftover(t, a, ".asm-install-123456")
	if err := os.WriteFile(filepath.Join(install, "asm-download-1.zip"), []byte("archive"), 0644); err != nil {
		t.Fatal(err)
	}
	update := fixtureLeftover(t, a, ".asm-update-123456")
	if err := a.run([]string{"clean"}); err != nil {
		t.Fatal(err)
	}
	assertClean(t, a)
	for _, path := range []string{partial, install, update} {
		if occupied(path) {
			t.Errorf("leftover remains: %s", path)
		}
	}
	for _, name := range []string{partialDirectory, ".asm-install-123456", ".asm-update-123456"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("report does not mention %s: %s", name, out)
		}
	}
}

func TestCleanRestoresOrphanedSDK(t *testing.T) {
	a, out, _ := testApp(t)
	root, _ := a.root()
	orphan := filepath.Join(root, ".asm-old-123456")
	// A kill during an update leaves the SDK renamed away from its own path.
	if err := os.Rename(fixtureInstalled(t, a, "51.4.1.1"), orphan); err != nil {
		t.Fatal(err)
	}
	if err := a.run([]string{"clean"}); err != nil {
		t.Fatal(err)
	}
	if occupied(orphan) {
		t.Fatalf("orphaned copy was not moved back: %s", out)
	}
	restored := filepath.Join(root, "AIRSDK_51.4.1.1")
	if _, err := os.Stat(filepath.Join(restored, "lib", "adt.lic")); err != nil {
		t.Fatalf("SDK contents were lost: %v", err)
	}
	sdks, err := a.installed()
	if err != nil {
		t.Fatal(err)
	}
	if len(sdks) != 1 || sdks[0].Version.String() != "51.4.1.1" || sdks[0].Path != restored {
		t.Fatalf("restored SDK is not visible: %v", sdks)
	}
	if !strings.Contains(out.String(), "Restored") {
		t.Errorf("report does not mention the restore: %s", out)
	}
}

func TestCleanDiscardsOldSDKWhenInstalled(t *testing.T) {
	a, _, _ := testApp(t)
	live := fixtureInstalled(t, a, "51.4.1.1")
	root, _ := a.root()
	orphan := filepath.Join(root, ".asm-old-123456")
	if err := os.Rename(fixtureSDKAt(t, a, "second_copy", "51.4.1.1"), orphan); err != nil {
		t.Fatal(err)
	}
	if err := a.run([]string{"clean"}); err != nil {
		t.Fatal(err)
	}
	if occupied(orphan) {
		t.Fatal("redundant copy was not removed")
	}
	if _, err := os.Stat(filepath.Join(live, "lib", "adt.lic")); err != nil {
		t.Fatalf("installed SDK was touched: %v", err)
	}
	if occupied(filepath.Join(root, "AIRSDK_51.4.1.1")) {
		t.Fatal("a duplicate SDK was created next to the installed one")
	}
}

func TestCleanRemovesEmptyVersionDirectoriesOnly(t *testing.T) {
	a, _, _ := testApp(t)
	empty := fixtureLeftover(t, a, "AIRSDK_51.4.1.1")
	live := fixtureInstalled(t, a, "51.3.4.3")
	if err := a.run([]string{"clean"}); err != nil {
		t.Fatal(err)
	}
	if occupied(empty) {
		t.Error("empty version directory remains")
	}
	if _, err := os.Stat(filepath.Join(live, "air-sdk-description.xml")); err != nil {
		t.Fatalf("installed SDK was removed: %v", err)
	}
}

func TestCleanLeavesForeignEntriesAlone(t *testing.T) {
	a, _, _ := testApp(t)
	root, _ := a.root()
	notes := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(notes, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}
	foreign := fixtureLeftover(t, a, "my-stuff")
	if err := os.WriteFile(filepath.Join(foreign, "data"), []byte("keep me too"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.run([]string{"clean"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(notes); err != nil || string(data) != "keep me" {
		t.Fatalf("foreign file: %q %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(foreign, "data")); err != nil || string(data) != "keep me too" {
		t.Fatalf("foreign directory: %q %v", data, err)
	}
	if a.settings["AIR_SDKS"] != root {
		t.Fatalf("settings changed: %q", a.settings["AIR_SDKS"])
	}
}

func TestCleanReportsNothingToDo(t *testing.T) {
	a, out, stderr := testApp(t)
	if err := a.run([]string{"clean"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Nothing to clean.") || stderr.Len() != 0 {
		t.Fatalf("out %q err %q", out, stderr)
	}
}

func TestCleanCheckChangesNothing(t *testing.T) {
	a, out, _ := testApp(t)
	partial := filepath.Join(fixtureLeftover(t, a, partialDirectory), strings.Repeat("b", 64)+".part")
	if err := os.WriteFile(partial, []byte("half a download"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := a.run([]string{"clean", "--check"}); err != nil {
		t.Fatal(err)
	}
	if !occupied(partial) {
		t.Fatal("--check removed a partial download")
	}
	if !strings.Contains(out.String(), "Would remove") || !strings.Contains(out.String(), "Would free") {
		t.Fatalf("--check report: %s", out)
	}
}

func TestCleanRefusesWhileAnotherOperationRuns(t *testing.T) {
	a, _, _ := testApp(t)
	root, _ := a.root()
	fixtureLeftover(t, a, ".asm-install-123456")
	unlock, err := lockSDKRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	err = a.run([]string{"clean"})
	if err == nil || !strings.Contains(err.Error(), "another install or update") {
		t.Fatalf("clean ran while the SDK root was locked: %v", err)
	}
}

func TestCleanRejectsArguments(t *testing.T) {
	a, _, _ := testApp(t)
	for _, args := range [][]string{{"clean", "51.4"}, {"clean", "--all"}} {
		err := a.run(args)
		if err == nil || strings.Contains(err.Error(), "unknown command") {
			t.Errorf("accepted %q: %v", args, err)
		}
	}
}
