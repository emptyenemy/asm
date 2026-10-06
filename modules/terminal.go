package modules

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type terminal struct {
	version            string
	out, err           io.Writer
	interactive, color bool
	width              func() int
}

func newTerminal(out, err *os.File) *terminal {
	interactive := terminalWidth(out) > 0 && terminalWidth(err) > 0 && os.Getenv("TERM") != "dumb"
	_, noColor := os.LookupEnv("NO_COLOR")
	color := interactive && !noColor && enableColor(out, err)
	return &terminal{out: out, err: err, interactive: interactive, color: color, width: func() int {
		if n := terminalWidth(out); n > 0 {
			return n
		}
		return 80
	}}
}

func (t *terminal) style(text, style string) string {
	if !t.color || style == "" {
		return text
	}
	code := "38;2;129;140;248"
	if style == "muted" {
		code = "90"
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (t *terminal) line(text, style string) { fmt.Fprintln(t.out, t.style(text, style)) }
func (t *terminal) notice(text string)      { fmt.Fprintln(t.err, t.style(text, "muted")) }
func (t *terminal) warning(text string)     { fmt.Fprintln(t.err, "Warning: "+text) }

func (t *terminal) wrap(text string, indent int, style string) {
	width := max(1, t.width()-indent-1)
	runes := []rune(text)
	for len(runes) > width {
		end := width
		for i := width; i > 0; i-- {
			if runes[i] == ' ' {
				end = i
				break
			}
		}
		t.line(strings.Repeat(" ", indent)+string(runes[:end]), style)
		runes = []rune(strings.TrimLeft(string(runes[end:]), " "))
	}
	t.line(strings.Repeat(" ", indent)+string(runes), style)
}

func (t *terminal) heading(text string) {
	if t.interactive {
		t.line("", "")
		t.wrap(text, 2, "accent")
		t.line("", "")
	}
}

func (t *terminal) rows(headers []string, rows [][]string) {
	columns := len(headers) - 1
	widths := make([]int, columns)
	prefix := 2 + columns*2
	for i := range widths {
		widths[i] = len(headers[i])
		for _, row := range rows {
			widths[i] = max(widths[i], len(row[i]))
		}
		prefix += widths[i]
	}
	if !t.interactive {
		if columns > 1 {
			for i := 0; i < columns; i++ {
				fmt.Fprintf(t.out, "%-14s ", headers[i])
			}
			fmt.Fprintln(t.out, headers[columns])
		}
		for _, row := range rows {
			for i := 0; i < columns; i++ {
				fmt.Fprintf(t.out, "%-14s ", row[i])
			}
			fmt.Fprintln(t.out, row[columns])
		}
		return
	}
	if t.width()-prefix < 18 {
		for _, row := range rows {
			t.wrap(strings.Join(row[:columns], " -> "), 2, "accent")
			t.wrap(row[columns], 4, "muted")
			t.line("", "")
		}
		return
	}
	header := "  "
	for i := 0; i < columns; i++ {
		header += fmt.Sprintf("%-*s  ", widths[i], headers[i])
	}
	t.line(header+headers[columns], "muted")
	for _, row := range rows {
		line := "  "
		for i := 0; i < columns; i++ {
			line += t.style(fmt.Sprintf("%-*s", widths[i], row[i]), "accent") + "  "
		}
		path := []rune(row[columns])
		available := t.width() - prefix - 1
		for len(path) > available {
			t.line(line+string(path[:available]), "")
			path = path[available:]
			line = strings.Repeat(" ", prefix)
		}
		t.line(line+string(path), "")
	}
}

type transfer struct {
	received, total atomic.Int64
	base            atomic.Int64 // bytes already on disk when the download began
	started         time.Time
}

func (p *transfer) Write(data []byte) (int, error) {
	p.received.Add(int64(len(data)))
	return len(data), nil
}

func formatBytes(bytes float64) string {
	units := []string{"B", "KB", "MB", "GB"}
	unit := 0
	for bytes >= 1000 && unit < 3 {
		bytes /= 1000
		unit++
	}
	bytes = math.Round(bytes*100) / 100
	if bytes == math.Trunc(bytes) {
		return fmt.Sprintf("%.0f %s", bytes, units[unit])
	}
	return fmt.Sprintf("%.2f %s", bytes, units[unit])
}

func (t *terminal) activity(label string, progress *transfer) func() {
	if !t.interactive {
		fmt.Fprintln(t.err, label+"...")
		return func() {}
	}
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		started, frame, oldWidth := time.Now(), 0, 0
		draw := func() {
			width := max(1, t.width()-1)
			text := fmt.Sprintf("  %c  %s  %ds", "|/-\\"[frame%4], label, int(time.Since(started).Seconds()))
			frame++
			if progress != nil {
				received, total := progress.received.Load(), progress.total.Load()
				stats := formatBytes(float64(received))
				if total > 0 {
					stats += " / " + formatBytes(float64(total))
				}
				seconds := time.Since(progress.started).Seconds()
				if seconds > 0 {
					stats += "  " + formatBytes(float64(max(0, received-progress.base.Load()))/seconds) + "/s"
				}
				percent := "  --"
				if total > 0 {
					percent = fmt.Sprintf("%3d%%", min(100, int(float64(received)*100/float64(total))))
				}
				if width >= 59 {
					barWidth := max(8, min(28, width-len(stats)-13))
					bar := ""
					if total > 0 {
						filled := min(barWidth, int(float64(received)/float64(total)*float64(barWidth)))
						bar = strings.Repeat("=", filled)
						if filled < barWidth {
							bar += ">"
							filled++
						}
						bar += strings.Repeat(".", barWidth-filled)
					} else {
						position := frame % barWidth
						bar = strings.Repeat(".", position) + ">" + strings.Repeat(".", barWidth-position-1)
					}
					text = "  [" + bar + "] " + percent + "  " + stats
				} else {
					text = fmt.Sprintf("  %c  %s  %s", "|/-\\"[frame%4], percent, formatBytes(float64(received)))
				}
			}
			runes := []rune(text)
			if len(runes) > width {
				runes = runes[:width]
			}
			text = string(runes)
			padding := strings.Repeat(" ", max(0, min(oldWidth, width)-len(runes)))
			fmt.Fprint(t.err, "\r"+t.style(text, "accent")+padding)
			oldWidth = len(runes)
		}
		draw()
		for {
			select {
			case <-ticker.C:
				draw()
			case <-done:
				fmt.Fprint(t.err, "\r"+strings.Repeat(" ", min(oldWidth, max(1, t.width()-1)))+"\r")
				return
			}
		}
	}()
	return func() { close(done); <-stopped }
}

func (t *terminal) help(topic string) {
	t.line("", "")
	if topic == "" {
		if t.width() >= 26 {
			for _, line := range []string{"  ____ __________ ___", " / __ `/ ___/ __ `__ \\", "/ /_/ (__  ) / / / / /", "\\__,_/____/_/ /_/ /_/"} {
				t.line("  "+line, "accent")
			}
		} else {
			t.line("  asm", "accent")
		}
		t.line("", "")
		t.wrap("AIR SDK Manager  "+t.version, 2, "muted")
		t.line("", "")
		t.wrap("Usage: asm <command> [options]", 2, "")
		t.line("", "")
		entries := [][]string{
			{"list, ls", "List installed SDKs and their paths."},
			{"search [VERSION]", "Find available stable releases."},
			{"install [VERSION]", "Install a branch, exact build, or latest."},
			{"update [VERSION]", "Check updates; a version applies them."},
			{"uninstall [VERSION]", "Remove one installed SDK. Alias: remove."},
			{"clean", "Remove what an interrupted operation left behind."},
			{"help [COMMAND]", "Show general or command help."},
			{"--version, -v", "Print the asm version."},
		}
		for _, entry := range entries {
			if t.width() >= 70 {
				t.line("  "+t.style(fmt.Sprintf("%-20s", entry[0]), "accent")+entry[1], "")
			} else {
				t.wrap(entry[0], 2, "accent")
				t.wrap(entry[1], 4, "muted")
			}
		}
		t.line("", "")
		t.wrap("Start: asm search 51.4", 2, "accent")
		t.wrap("Help:  asm <command> --help", 2, "muted")
		t.line("", "")
		return
	}
	usage := map[string]string{
		"list":      "asm list",
		"search":    "asm search [VERSION]",
		"install":   "asm install [VERSION] [--accept-license]",
		"update":    "asm update [VERSION] [--all] [--check] [--accept-license]",
		"uninstall": "asm uninstall [VERSION]",
		"clean":     "asm clean [--check]",
	}
	t.wrap("Usage: "+usage[topic], 2, "accent")
	t.line("", "")
	lines := map[string][]string{
		"uninstall": {
			"Alias: asm remove [VERSION]",
			"VERSION: an exact build or a prefix matching one installed SDK.",
			"Ambiguous versions are rejected; use a full version from asm list.",
			"Deletes that SDK directory from AIR_SDKS without keeping a backup.",
			"Does not change AIR SDK Manager settings or PATH.",
		},
		"list": {
			"Alias: asm ls",
			"Reads AIR_SDKS from ~/.airsdk/airsdkmanager.cfg.",
			"Lists installed SDK versions and paths, newest first.",
		},
		"search": {
			"VERSION is optional: a branch such as 51.4 or an exact build.",
			"Lists announced stable releases, newest first.",
			"Falls back to the announcement archive and the manager catalog.",
		},
		"install": {
			"VERSION: a branch such as 51.4, an exact build, or latest.",
			"Installs into AIR_SDKS; an installed build is kept.",
			"Falls back to the mirror if the official API fails.",
			"An interrupted download resumes on the next attempt.",
			"--accept-license  Accept the AIR SDK license for this operation.",
		},
		"update": {
			"No arguments      Show installed updates and a newer SDK branch.",
			"VERSION           Update matching installed SDKs, such as 51.3.",
			"--all             Update all installed SDKs with a newer build.",
			"--check           Preview only, including with VERSION or --all.",
			"--accept-license  Accept the AIR SDK license for this operation.",
			"Updates preserve each SDK path and three-component version.",
			"Install new branches separately with asm install.",
		},
		"clean": {
			"Removes incomplete downloads and temporary directories left by an interrupted install or update.",
			"Restores an SDK that an interrupted update saved for rollback and left out of place.",
			"Removes empty version directories that would block installing that version again.",
			"Does not touch installed SDKs, settings, or unrelated files.",
			"--check  List what would be removed without changing anything.",
		},
	}
	for _, line := range lines[topic] {
		t.wrap(line, 2, "")
	}
	t.line("", "")
}
