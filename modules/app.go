package modules

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type sdkVersion [4]int

func versionParts(text string) ([]int, error) {
	parts := strings.Split(text, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return nil, errors.New("expected a version such as 51.4 or 51.4.1.1")
	}
	result := make([]int, len(parts))
	for i, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return nil, errors.New("expected a version such as 51.4 or 51.4.1.1")
		}
		n, err := strconv.ParseUint(part, 10, 31)
		if err != nil {
			return nil, errors.New("version component is too large")
		}
		result[i] = int(n)
	}
	return result, nil
}

func parseVersion(text string) (sdkVersion, error) {
	parts, err := versionParts(text)
	if err != nil {
		return sdkVersion{}, err
	}
	if len(parts) != 4 {
		return sdkVersion{}, errors.New("expected a full four-component SDK version")
	}
	return sdkVersion{parts[0], parts[1], parts[2], parts[3]}, nil
}

func (v sdkVersion) String() string {
	if v[3] < 0 {
		return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	}
	return fmt.Sprintf("%d.%d.%d.%d", v[0], v[1], v[2], v[3])
}

func (v sdkVersion) newer(other sdkVersion) bool {
	for i := range v {
		if v[i] != other[i] {
			return v[i] > other[i]
		}
	}
	return false
}

func (v sdkVersion) matches(parts []int) bool {
	for i, part := range parts {
		if v[i] != part {
			return false
		}
	}
	return true
}

func (v sdkVersion) branch(other sdkVersion) bool {
	return v[0] == other[0] && v[1] == other[1] && v[2] == other[2]
}

type installedSDK struct {
	Version sdkVersion
	Path    string
}
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
	newsURL, mirrorURL string
}

func newApp(ctx context.Context, ui *terminal, version string) *app {
	ui.version = version
	home, _ := os.UserHomeDir()
	return &app{version: version, ctx: ctx, ui: ui, configFile: filepath.Join(home, ".airsdk", "airsdkmanager.cfg"),
		os: runtime.GOOS, arch: runtime.GOARCH, newsURL: "https://airsdk.dev/news/archive", mirrorURL: "https://shockpkg.github.io/packages/api/1/packages.json"}
}

func (a *app) loadSettings() error {
	if a.settings != nil {
		return nil
	}
	data, err := os.ReadFile(a.configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("AIR SDK Manager settings not found: %s", a.configFile)
		}
		return err
	}
	a.settings = make(map[string]string)
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			a.settings[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), "\"")
		}
	}
	return nil
}

func (a *app) root() (string, error) {
	if err := a.loadSettings(); err != nil {
		return "", err
	}
	root := a.settings["AIR_SDKS"]
	if root == "" {
		return "", fmt.Errorf("AIR_SDKS is not set in %s", a.configFile)
	}
	return filepath.Abs(root)
}

func readSDK(path string) (installedSDK, error) {
	data, err := os.ReadFile(filepath.Join(path, "air-sdk-description.xml"))
	if err != nil {
		return installedSDK{}, err
	}
	var description struct {
		XMLName xml.Name `xml:"air-sdk-description"`
		Version string   `xml:"version"`
		Build   string   `xml:"build"`
	}
	if err = xml.Unmarshal(data, &description); err != nil {
		return installedSDK{}, err
	}
	parts, err := versionParts(strings.TrimSpace(description.Version))
	if err != nil || len(parts) < 3 {
		return installedSDK{}, errors.New("invalid SDK description version")
	}
	v := sdkVersion{parts[0], parts[1], parts[2], -1}
	if len(parts) == 4 {
		v[3] = parts[3]
	} else if strings.TrimSpace(description.Build) != "" {
		build, err := versionParts(strings.TrimSpace(description.Build))
		if err != nil || len(build) != 1 {
			return installedSDK{}, errors.New("invalid SDK description build")
		}
		v[3] = build[0]
	}
	return installedSDK{v, path}, nil
}

func (a *app) installed() ([]installedSDK, error) {
	root, err := a.root()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("cannot read SDK directory %s: %w", root, err)
	}
	var sdks []installedSDK
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".asm-") {
			continue
		}
		if !entry.IsDir() {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			info, err := os.Stat(filepath.Join(root, entry.Name()))
			if err != nil || !info.IsDir() {
				continue
			}
		}
		sdk, err := readSDK(filepath.Join(root, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			a.ui.warning(fmt.Sprintf("Cannot read SDK description in %s: %v", filepath.Join(root, entry.Name()), err))
			continue
		}
		sdks = append(sdks, sdk)
	}
	sort.Slice(sdks, func(i, j int) bool { return sdks[i].Version.newer(sdks[j].Version) })
	return sdks, nil
}

func parseOptions(command string, args []string) (options, error) {
	var o options
	for _, arg := range args {
		switch arg {
		case "--accept-license":
			o.license = true
		case "--all":
			if command != "update" {
				return o, fmt.Errorf("unknown option %s", arg)
			}
			o.all = true
		case "--check":
			if command != "update" {
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
			return errors.New("usage: asm uninstall VERSION; run asm help uninstall")
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
			a.ui.line("No local AIR SDK versions found.", "")
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
			a.ui.line("No matching AIR SDK versions found.", "")
			return nil
		}
		for _, v := range matches {
			prefix := ""
			if a.ui.interactive {
				prefix = "  "
			}
			a.ui.line(prefix+v.String(), "accent")
		}
		if a.ui.interactive {
			a.ui.line("", "")
			a.ui.wrap("Install: asm install "+matches[0].String(), 2, "muted")
		}
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
	return command == "list" || command == "search" || command == "install" || command == "update" || command == "uninstall"
}

var newsLinks = regexp.MustCompile(`(?is)<a\b[^>]*\bhref="/news/\d{4}/\d{2}/\d{2}/[^"\s]+"[^>]*>(.*?)</a>`)
var htmlTags = regexp.MustCompile(`<[^>]*>`)
var newsNumber = regexp.MustCompile(`(?i)\bRelease\s+(\d+\.\d+\.\d+\.\d+)\b`)
var previewTitle = regexp.MustCompile(`(?i)\b(beta|alpha|preview|pre[ -]?release)\b`)

// Run executes the CLI with the supplied arguments and build version.
func Run(args []string, version string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	return newApp(ctx, newTerminal(os.Stdout, os.Stderr), version).run(args)
}
