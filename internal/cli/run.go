package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"
)

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

// run dispatches one command line: the general help, --version, a command's
// help, or the command itself with its arguments checked against the table.
func (a *app) run(args []string) error {
	if len(args) == 0 {
		a.ui.help("")
		return nil
	}
	name, rest := args[0], args[1:]
	switch {
	case name == "help":
		if len(rest) > 1 {
			return errors.New("unexpected arguments; run asm help")
		}
		if len(rest) == 0 {
			a.ui.help("")
			return nil
		}
		c, ok := findCommand(rest[0])
		if !ok {
			return fmt.Errorf("unknown help topic %q", rest[0])
		}
		a.ui.help(c.name)
		return nil
	case len(rest) == 0 && (name == "--version" || name == "-v"):
		a.ui.line(a.version, "accent")
		return nil
	case len(rest) == 0 && (name == "--help" || name == "-h"):
		a.ui.help("")
		return nil
	}
	c, ok := findCommand(name)
	if !ok {
		return fmt.Errorf("unknown command %q; run asm help", name)
	}
	if len(rest) == 1 && (rest[0] == "--help" || rest[0] == "-h") {
		a.ui.help(c.name)
		return nil
	}
	o, err := c.parse(rest)
	if err != nil {
		return err
	}
	return c.run(a, o)
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
	a := newApp(ctx, newTerminal(os.Stdin, os.Stdout, os.Stderr), version)
	return a.finish(a.run(args))
}
