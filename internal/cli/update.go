package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

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
		a.ui.say("Installed SDKs are up to date.", "")
	} else {
		a.ui.say("No local AIR SDK versions found.", "")
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
			a.ui.gap()
			a.ui.say("New AIR SDK available: "+latest.String(), "accent")
			a.ui.hint("Install:", fmt.Sprintf("asm install %d.%d", latest[0], latest[1]))
		}
	}
	if len(updates) == 0 || o.check || (!o.all && o.filter == "") {
		if len(updates) > 0 && a.ui.interactive {
			action := o.filter
			if action == "" {
				action = "--all"
			}
			a.ui.gap()
			a.ui.hint("Apply:", "asm update "+action)
		}
		return nil
	}
	if err := a.acceptLicense(o); err != nil {
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
	for _, update := range updates {
		if err := a.replaceSDK(update.sdk, update.available); err != nil {
			return err
		}
	}
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
	a.ui.gap()
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
	a.ui.say(fmt.Sprintf("Updated %s -> %s", sdk.Version, v), "accent")
	a.ui.pair("Path:", current)
	return nil
}
