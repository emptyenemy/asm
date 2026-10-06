package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type options struct {
	filter              string
	all, check, license bool
}

type app struct {
	version            string
	ctx                context.Context
	ui                 *terminal
	configFile         string
	settings           map[string]string
	os, arch           string
	apiURL             string
	newsURL, mirrorURL string
	apiTimeout         time.Duration
	retryDelay         time.Duration
	stallTimeout       time.Duration
}

func newApp(ctx context.Context, ui *terminal, version string) *app {
	ui.version = version
	home, _ := os.UserHomeDir()
	return &app{version: version, ctx: ctx, ui: ui, configFile: filepath.Join(home, ".airsdk", "airsdkmanager.cfg"),
		os: runtime.GOOS, arch: runtime.GOARCH, apiURL: "https://api.airsdk.harman.com",
		newsURL: "https://airsdk.dev/news/archive", mirrorURL: "https://shockpkg.github.io/packages/api/1/packages.json",
		apiTimeout: 6 * time.Second, retryDelay: time.Second, stallTimeout: 30 * time.Second}
}

func parseOptions(command string, args []string) (options, error) {
	var o options
	for _, arg := range args {
		switch arg {
		case "--accept-license":
			if command != "install" && command != "update" {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			o.license = true
		case "--all":
			if command != "update" {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			o.all = true
		case "--check":
			if command != "update" && command != "clean" {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			o.check = true
		default:
			if strings.HasPrefix(arg, "-") {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			if o.filter != "" {
				return o, errors.New("unexpected arguments; run asm help")
			}
			o.filter = arg
		}
	}
	if o.all && o.filter != "" {
		return o, errors.New("a version and --all cannot be combined")
	}
	return o, nil
}

func (a *app) run(args []string) error {
	if len(args) == 0 {
		a.ui.help("")
		return nil
	}
	command, rest := args[0], args[1:]
	if command == "help" {
		if len(rest) > 1 {
			return errors.New("unexpected arguments; run asm help")
		}
		if len(rest) == 0 {
			a.ui.help("")
			return nil
		}
		if rest[0] == "ls" {
			rest[0] = "list"
		}
		if rest[0] == "remove" {
			rest[0] = "uninstall"
		}
		if !validCommand(rest[0]) {
			return fmt.Errorf("unknown help topic %q", rest[0])
		}
		a.ui.help(rest[0])
		return nil
	}
	if len(rest) == 0 && (command == "--version" || command == "-v") {
		a.ui.line(a.version, "accent")
		return nil
	}
	if len(rest) == 0 && (command == "--help" || command == "-h") {
		a.ui.help("")
		return nil
	}
	if command == "ls" {
		command = "list"
	}
	if command == "remove" {
		command = "uninstall"
	}
	if !validCommand(command) {
		return fmt.Errorf("unknown command %q; run asm help", command)
	}
	if len(rest) == 1 && (rest[0] == "--help" || rest[0] == "-h") {
		a.ui.help(command)
		return nil
	}
	switch command {
	case "uninstall":
		if len(rest) != 1 {
			return errors.New("usage: asm uninstall [VERSION]; run asm help uninstall")
		}
		return a.uninstall(rest[0])
	case "list":
		if len(rest) != 0 {
			return errors.New("unexpected arguments; run asm help list")
		}
		sdks, err := a.installed()
		if err != nil {
			return err
		}
		a.ui.heading("Installed AIR SDKs")
		if len(sdks) == 0 {
			a.ui.say("No local AIR SDK versions found.", "")
			return nil
		}
		var rows [][]string
		for _, sdk := range sdks {
			rows = append(rows, []string{sdk.Version.String(), sdk.Path})
		}
		a.ui.rows([]string{"Version", "Path"}, rows)
	case "search":
		if len(rest) > 1 {
			return errors.New("unexpected arguments; run asm help search")
		}
		var parts []int
		if len(rest) == 1 {
			var err error
			parts, err = versionParts(rest[0])
			if err != nil {
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
	case "clean":
		o, err := parseOptions(command, rest)
		if err != nil {
			return err
		}
		if o.filter != "" {
			return errors.New("unexpected arguments; run asm help clean")
		}
		return a.clean(o.check)
	case "install", "update":
		o, err := parseOptions(command, rest)
		if err != nil {
			return err
		}
		if command == "install" {
			return a.install(o)
		}
		return a.update(o)
	}
	return nil
}

func validCommand(command string) bool {
	return command == "list" || command == "search" || command == "install" || command == "update" || command == "uninstall" || command == "clean"
}

// finish reports how a run ended: the error that stopped it, or the blank
// line that closes an interactive answer.
func (a *app) finish(err error) error {
	if err != nil {
		a.ui.failure(err)
	} else {
		a.ui.close()
	}
	return err
}

// Run executes the CLI with the supplied arguments and build version. An
// error is reported on stderr before it is returned.
func Run(args []string, version string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	a := newApp(ctx, newTerminal(os.Stdout, os.Stderr), version)
	return a.finish(a.run(args))
}
