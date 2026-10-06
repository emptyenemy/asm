package cli

// list prints the installed SDKs, newest first, with their paths.
func (a *app) list(o options) error {
	sdks, err := a.installed()
	if err != nil {
		return err
	}
	a.ui.heading("Installed AIR SDKs")
	if len(sdks) == 0 {
		a.ui.say("No local AIR SDK versions found.", "")
		if a.ui.interactive {
			a.ui.gap()
			a.ui.hint("Install:", "asm install latest")
		}
		return nil
	}
	var rows [][]string
	for _, sdk := range sdks {
		rows = append(rows, []string{sdk.Version.String(), sdk.Path})
	}
	a.ui.rows([]string{"Version", "Path"}, rows)
	return nil
}
