package cli

import "fmt"

// help prints the general help, or the help of one command. It is framed and
// indented in redirected output too, since it is read rather than parsed.
func (t *terminal) help(topic string) {
	interactive := t.interactive
	t.interactive = true
	defer func() { t.interactive = interactive }()
	t.open()
	if topic == "" {
		if t.width() >= 26 {
			for _, line := range banner {
				t.line("  "+line, "accent")
			}
		} else {
			t.line("  asm", "accent")
		}
		t.gap()
		t.labeled(t.out, "AIR SDK Manager", "muted", t.version, "accent", false)
		t.gap()
		t.labeled(t.out, "Usage:", "muted", "asm <command> [options]", "", false)
		t.gap()
		t.columns([][2]string{
			{"list, ls", "List installed SDKs and their paths."},
			{"search [VERSION]", "Find available stable releases."},
			{"install [VERSION]", "Install a branch, exact build, or latest."},
			{"update [VERSION]", "Check updates; a version applies them."},
			{"uninstall [VERSION]", "Remove one installed SDK. Alias: remove."},
			{"clean", "Remove what an interrupted operation left behind."},
			{"help [COMMAND]", "Show general or command help."},
			{"--version, -v", "Print the asm version."},
		})
		t.gap()
		t.hint("Start:", "asm search 51.4")
		t.hint("Help: ", "asm <command> --help")
		t.gap()
		return
	}
	help := helpTopics[topic]
	t.labeled(t.out, "Usage:", "muted", help.usage, "accent", false)
	if help.alias != "" {
		t.labeled(t.out, "Alias:", "muted", help.alias, "accent", false)
	}
	t.gap()
	for _, line := range help.lines {
		t.wrap(line, 2, "")
	}
	if len(help.options) > 0 {
		t.gap()
		t.columns(help.options)
	}
	t.gap()
}

type helpTopic struct {
	usage, alias string
	lines        []string
	options      [][2]string
}

var helpTopics = map[string]helpTopic{
	"list": {
		usage: "asm list", alias: "asm ls",
		lines: []string{
			"Lists installed SDK versions and paths, newest first.",
			"Reads AIR_SDKS from ~/.airsdk/airsdkmanager.cfg; the first run sets it to ~/sdks/air.",
		},
	},
	"search": {
		usage: "asm search [VERSION]",
		lines: []string{
			"Lists announced stable releases, newest first.",
			"VERSION is optional: a branch such as 51.4 or an exact build.",
			"Falls back to the announcement archive and the manager catalog.",
		},
	},
	"install": {
		usage: "asm install [VERSION] [--accept-license]",
		lines: []string{
			"VERSION: a branch such as 51.4, an exact build, or latest.",
			"Installs into AIR_SDKS; an installed build is kept.",
			"Falls back to the mirror if the official API fails.",
			"An interrupted download resumes on the next attempt.",
			"Asks once to accept the AIR SDK license and saves the answer.",
		},
		options: [][2]string{{"--accept-license", "Accept the AIR SDK license for this operation."}},
	},
	"update": {
		usage: "asm update [VERSION] [--all] [--check] [--accept-license]",
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
	},
	"uninstall": {
		usage: "asm uninstall [VERSION]", alias: "asm remove [VERSION]",
		lines: []string{
			"VERSION: an exact build or a prefix matching one installed SDK.",
			"Ambiguous versions are rejected; use a full version from asm list.",
			"Deletes that SDK directory from AIR_SDKS without keeping a backup.",
			"Does not change AIR SDK Manager settings or PATH.",
		},
	},
	"clean": {
		usage: "asm clean [--check]",
		lines: []string{
			"Removes incomplete downloads and temporary directories left by an interrupted install or update.",
			"Restores an SDK that an interrupted update saved for rollback and left out of place.",
			"Removes empty version directories that would block installing that version again.",
			"Does not touch installed SDKs, settings, or unrelated files.",
		},
		options: [][2]string{{"--check", "List what would be removed without changing anything."}},
	},
}

var banner = []string{"  ____ __________ ___", " / __ `/ ___/ __ `__ \\", "/ /_/ (__  ) / / / / /", "\\__,_/____/_/ /_/ /_/"}

// columns prints name and description pairs as two aligned columns, names in
// the accent. A terminal too narrow for that gets each name on its own line.
func (t *terminal) columns(entries [][2]string) {
	width := 0
	for _, entry := range entries {
		width = max(width, len(entry[0]))
	}
	indent := 2 + width + 3
	if t.width()-indent < 28 {
		for _, entry := range entries {
			t.wrap(entry[0], 2, "accent")
			t.wrap(entry[1], 4, "")
		}
		return
	}
	for _, entry := range entries {
		for i, line := range wrapText(entry[1], t.width()-indent-1) {
			name := ""
			if i == 0 {
				name = entry[0]
			}
			t.line("  "+t.style(fmt.Sprintf("%-*s", width+3, name), "accent")+line, "")
		}
	}
}
