package cli

import (
	"fmt"
	"os"
	"strings"
)

// uninstall removes the one installed SDK the version argument selects. The
// directory is re-read under the root lock and left alone if it changed since
// the listing, and an argument that matches several SDKs is refused rather than
// guessed at.
func (a *app) uninstall(o options) error {
	request := o.filter
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
	a.ui.pair("Path:", path)
	stop := a.ui.activity("Removing AIR SDK "+sdk.Version.String(), nil)
	err = removeChild(root, path)
	stop()
	if err != nil {
		return fmt.Errorf("cannot completely remove SDK at %s: %w", path, err)
	}
	a.ui.say("Uninstalled AIR SDK "+sdk.Version.String(), "accent")
	return nil
}
