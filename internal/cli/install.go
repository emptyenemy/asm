package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// install resolves the requested version, assembles it in a staging directory
// inside the SDK root, and moves it into place in one step, so an interrupted
// run leaves a .asm-install- directory for clean to find rather than a partly
// written version directory that looks installed.
func (a *app) install(o options) error {
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
				a.reportInstalled(sdk)
				return nil
			}
		}
	}
	destination := filepath.Join(root, "AIRSDK_"+v.String())
	if occupied(destination) {
		return fmt.Errorf("installation path is already occupied: %s", destination)
	}
	if err := a.acceptLicense(o); err != nil {
		return err
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
			a.reportInstalled(sdk)
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
	a.ui.pair("Destination:", destination)
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
	a.ui.say("Installed AIR SDK "+v.String(), "accent")
	return nil
}

// reportInstalled answers an install of a build that is already in place.
func (a *app) reportInstalled(sdk installedSDK) {
	a.ui.say(fmt.Sprintf("AIR SDK %s is already installed.", sdk.Version), "accent")
	a.ui.pair("Path:", sdk.Path)
}
