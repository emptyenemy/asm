package cli

import (
	"fmt"
	"strings"
)

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
		var entries [][2]string
		for _, c := range commands {
			name, summary := c.name, c.summary
			if c.version != noVersion {
				name += " [VERSION]"
			}
			if c.alias != "" {
				summary += " Alias: " + c.alias + "."
			}
			entries = append(entries, [2]string{name, summary})
		}
		t.columns(append(entries,
			[2]string{"help [COMMAND]", "Show general or command help."},
			[2]string{"--version, -v", "Print the asm version."}))
		t.gap()
		t.hint("Start:", "asm search 51.4")
		t.hint("Help: ", "asm <command> --help")
		t.gap()
		return
	}
	c, _ := findCommand(topic)
	t.labeled(t.out, "Usage:", "muted", strings.TrimSpace("asm "+c.name+" "+c.usage), "accent", false)
	if c.alias != "" {
		alias := "asm " + c.alias
		if c.version != noVersion {
			alias += " [VERSION]"
		}
		t.labeled(t.out, "Alias:", "muted", alias, "accent", false)
	}
	t.gap()
	for _, line := range c.lines {
		t.wrap(line, 2, "")
	}
	if len(c.options) > 0 {
		t.gap()
		t.columns(c.options)
	}
	t.gap()
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
