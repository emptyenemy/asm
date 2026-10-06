package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// command is everything asm knows about one command: how it is called, what
// its help says, which arguments it takes, and the method that runs it.
// Dispatch, argument checks, --help and both kinds of help come from here, so
// a new command is one more entry in commands plus that method.
type command struct {
	name, alias string
	summary     string      // the line in the general help
	usage       string      // what follows the name in "Usage:"
	version     argument    // whether a VERSION argument is taken
	flags       []string    // the options it accepts
	lines       []string    // the command help, a sentence per line
	options     [][2]string // the options as the command help lists them
	run         func(*app, options) error
}

type argument int

const (
	noVersion argument = iota
	optionalVersion
	requiredVersion
)

type options struct {
	filter              string
	all, check, license bool
}

var commands = []command{
	{
		name: "list", alias: "ls",
		summary: "List installed SDKs and their paths.",
		lines: []string{
			"Lists installed SDK versions and paths, newest first.",
			"Reads AIR_SDKS from ~/.airsdk/airsdkmanager.cfg; the first run sets it to ~/sdks/air.",
		},
		run: (*app).list,
	},
	{
		name: "search", usage: "[VERSION]", version: optionalVersion,
		summary: "Find available stable releases.",
		lines: []string{
			"Lists announced stable releases, newest first.",
			"VERSION is optional: a branch such as 51.4 or an exact build.",
			"Falls back to the announcement archive and the manager catalog.",
		},
		run: (*app).search,
	},
	{
		name: "install", usage: "[VERSION] [--accept-license]", version: requiredVersion,
		flags:   []string{"--accept-license"},
		summary: "Install a branch, exact build, or latest.",
		lines: []string{
			"VERSION: a branch such as 51.4, an exact build, or latest.",
			"Installs into AIR_SDKS; an installed build is kept.",
			"Falls back to the mirror if the official API fails.",
			"An interrupted download resumes on the next attempt.",
			"Asks once to accept the AIR SDK license and saves the answer.",
		},
		options: [][2]string{{"--accept-license", "Accept the AIR SDK license for this operation."}},
		run:     (*app).install,
	},
	{
		name: "update", usage: "[VERSION] [--all] [--check] [--accept-license]", version: optionalVersion,
		flags:   []string{"--all", "--check", "--accept-license"},
		summary: "Check updates; a version applies them.",
		lines: []string{
			"Without arguments, shows installed SDKs with a newer build and a newer SDK branch.",
			"Updates keep each SDK's path and three-component version.",
			"Install new branches separately with asm install.",
		},
		options: [][2]string{
			{"VERSION", "Update matching installed SDKs, such as 51.3."},
			{"--all", "Update all installed SDKs with a newer build."},
			{"--check", "Preview only, including with VERSION or --all."},
			{"--accept-license", "Accept the AIR SDK license for this operation."},
		},
		run: (*app).update,
	},
	{
		name: "uninstall", alias: "remove", usage: "[VERSION]", version: requiredVersion,
		summary: "Remove one installed SDK.",
		lines: []string{
			"VERSION: an exact build or a prefix matching one installed SDK.",
			"Ambiguous versions are rejected; use a full version from asm list.",
			"Deletes that SDK directory from AIR_SDKS without keeping a backup.",
			"Does not change AIR SDK Manager settings or PATH.",
		},
		run: (*app).uninstall,
	},
	{
		name: "clean", usage: "[--check]",
		flags:   []string{"--check"},
		summary: "Remove what an interrupted operation left behind.",
		lines: []string{
			"Removes incomplete downloads and temporary directories left by an interrupted install or update.",
			"Restores an SDK that an interrupted update saved for rollback and left out of place.",
			"Removes empty version directories that would block installing that version again.",
			"Does not touch installed SDKs, settings, or unrelated files.",
		},
		options: [][2]string{{"--check", "List what would be removed without changing anything."}},
		run:     (*app).clean,
	},
}

// findCommand looks a command up by its name or its alias.
func findCommand(name string) (command, bool) {
	for _, c := range commands {
		if name == c.name || (c.alias != "" && name == c.alias) {
			return c, true
		}
	}
	return command{}, false
}

// parse reads the arguments that follow the command's name: at most one
// version, where the command takes one, and only the options it accepts.
func (c command) parse(args []string) (options, error) {
	var o options
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			if !slices.Contains(c.flags, arg) {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			switch arg {
			case "--accept-license":
				o.license = true
			case "--all":
				o.all = true
			case "--check":
				o.check = true
			}
			continue
		}
		if c.version == noVersion || o.filter != "" {
			return o, fmt.Errorf("unexpected arguments; run asm help %s", c.name)
		}
		o.filter = arg
	}
	if c.version == requiredVersion && o.filter == "" {
		return o, fmt.Errorf("usage: asm %s %s; run asm help %s", c.name, c.usage, c.name)
	}
	if o.all && o.filter != "" {
		return o, errors.New("a version and --all cannot be combined")
	}
	return o, nil
}
