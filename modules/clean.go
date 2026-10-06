package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sweep records what one clean run removed or put back.
type sweep struct {
	removed  []string
	restored []string
	freed    int64
}

// remove deletes a leftover and remembers the space it occupied. Under --check
// it only records it.
func (r *sweep) remove(path string, check bool) error {
	size := directorySize(path)
	if !check {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	r.removed = append(r.removed, filepath.Base(path))
	r.freed += size
	return nil
}

func (r *sweep) report(a *app, check bool) {
	if len(r.removed) == 0 && len(r.restored) == 0 {
		a.ui.line("Nothing to clean.", "")
		return
	}
	removed, restored, freed := "Removed", "Restored", "Freed"
	if check {
		removed, restored, freed = "Would remove", "Would restore", "Would free"
	}
	for _, name := range r.removed {
		a.ui.line(removed+" "+name, "")
	}
	for _, name := range r.restored {
		a.ui.line(restored+" "+name, "")
	}
	if r.freed > 0 {
		a.ui.line(freed+" "+formatBytes(float64(r.freed)), "")
	}
}

// directorySize totals the bytes a tree occupies, ignoring entries it cannot read.
func directorySize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			if info, err := entry.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func isEmptyDirectory(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

// clean removes what an interrupted install, update, or download leaves in the
// SDK root. Incomplete downloads and staging directories are junk by
// definition; an SDK saved for rollback is moved back into place when its
// build is no longer installed and removed when it is.
func (a *app) clean(check bool) error {
	root, err := a.root()
	if err != nil {
		return err
	}
	if !occupied(root) {
		a.ui.line("Nothing to clean.", "")
		return nil
	}
	unlock, err := lockSDKRoot(root)
	if err != nil {
		return err
	}
	defer unlock()
	result, err := a.sweepRoot(root, check)
	if err != nil {
		return err
	}
	result.report(a, check)
	return nil
}

func (a *app) sweepRoot(root string, check bool) (*sweep, error) {
	result := &sweep{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var saved []string
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		switch {
		case name == partialDirectory,
			strings.HasPrefix(name, ".asm-install-"),
			strings.HasPrefix(name, ".asm-update-"):
			if err := result.remove(path, check); err != nil {
				return nil, err
			}
		case strings.HasPrefix(name, ".asm-old-"):
			saved = append(saved, path)
		case !strings.HasPrefix(name, ".asm-") && entry.IsDir() && isEmptyDirectory(path):
			// A version directory created before its contents were moved in.
			if err := result.remove(path, check); err != nil {
				return nil, err
			}
		}
	}
	if len(saved) > 0 {
		installed, err := a.installed()
		if err != nil {
			return nil, err
		}
		for _, path := range saved {
			if err := a.restoreSavedSDK(result, root, path, installed, check); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// restoreSavedSDK returns an SDK that was renamed away for a rollback and never
// moved back. The update may also have finished before the process died, in
// which case the copy is redundant.
func (a *app) restoreSavedSDK(result *sweep, root, saved string, installed []installedSDK, check bool) error {
	if isEmptyDirectory(saved) {
		return result.remove(saved, check)
	}
	sdk, err := readSDK(saved)
	if err != nil {
		a.ui.warning(fmt.Sprintf("Cannot read the SDK saved in %s: %v. Leaving it in place.", saved, err))
		return nil
	}
	for _, live := range installed {
		if live.Version == sdk.Version {
			return result.remove(saved, check)
		}
	}
	destination := filepath.Join(root, "AIRSDK_"+sdk.Version.String())
	if occupied(destination) {
		a.ui.warning(fmt.Sprintf("AIR SDK %s is saved in %s, but %s is occupied. Leaving both in place.", sdk.Version, saved, destination))
		return nil
	}
	if !check {
		if err := os.Rename(saved, destination); err != nil {
			return err
		}
	}
	result.restored = append(result.restored, filepath.Base(saved)+" as "+filepath.Base(destination))
	return nil
}
