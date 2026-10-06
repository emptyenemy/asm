package cli

// search prints the stable releases that match the version, newest first.
// Redirected, it prints one version per line and nothing else.
func (a *app) search(o options) error {
	var parts []int
	if o.filter != "" {
		var err error
		if parts, err = versionParts(o.filter); err != nil {
			return err
		}
	}
	versions, err := a.releases()
	if err != nil {
		return err
	}
	a.ui.heading("Available AIR SDKs")
	var matches []sdkVersion
	for _, v := range versions {
		if v.matches(parts) {
			matches = append(matches, v)
		}
	}
	if len(matches) == 0 {
		a.ui.say("No matching AIR SDK versions found.", "")
		return nil
	}
	for _, v := range matches {
		a.ui.say(v.String(), "accent")
	}
	if a.ui.interactive {
		a.ui.gap()
		a.ui.hint("Install:", "asm install "+matches[0].String())
	}
	return nil
}
