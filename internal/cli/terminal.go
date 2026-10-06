package cli

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// terminal renders everything asm prints. In an interactive terminal an
// answer is framed by one blank line before and after it, its body is
// indented by two columns, and color marks what to read first: headings,
// versions, results and commands to run in the accent, labels and side notes
// muted. Redirected output stays plain, unindented and unframed.
type terminal struct {
	version            string
	out, err           io.Writer
	interactive, color bool
	width              func() int
	opened, blank      bool // the answer has started; the last line was empty
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

var styles = map[string]string{
	"accent":  "38;2;129;140;248", // #818CF8
	"muted":   "90",
	"warning": "38;2;245;201;122", // #F5C97A
	"error":   "38;2;248;113;113", // #F87171
}

func (t *terminal) style(text, style string) string {
	code := styles[style]
	if !t.color || code == "" || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// print writes one finished line and remembers whether it was empty, so the
// framing below never stacks two blank lines.
func (t *terminal) print(w io.Writer, text string) {
	fmt.Fprintln(w, text)
	t.blank = text == ""
}

func (t *terminal) line(text, style string) { t.print(t.out, t.style(text, style)) }

// open starts an interactive answer with a blank line.
func (t *terminal) open() {
	if t.interactive && !t.opened {
		t.opened = true
		if !t.blank {
			t.print(t.out, "")
		}
	}
}

// gap separates two parts of an interactive answer.
func (t *terminal) gap() {
	if t.interactive {
		t.open()
		if !t.blank {
			t.print(t.out, "")
		}
	}
}

// close ends an interactive answer with a blank line before the prompt.
func (t *terminal) close() {
	if t.interactive && t.opened && !t.blank {
		t.print(t.out, "")
	}
}

// wrapText breaks text into lines of at most width runes, at a space when
// there is one and mid-word when there is not, as with a long path.
func wrapText(text string, width int) []string {
	width = max(1, width)
	var lines []string
	runes := []rune(text)
	for len(runes) > width {
		end := width
		for i := width; i > 0; i-- {
			if runes[i] == ' ' {
				end = i
				break
			}
		}
		lines = append(lines, string(runes[:end]))
		runes = []rune(strings.TrimLeft(string(runes[end:]), " "))
	}
	return append(lines, string(runes))
}

func (t *terminal) wrap(text string, indent int, style string) {
	pad := strings.Repeat(" ", indent)
	for _, line := range wrapText(text, t.width()-indent-1) {
		t.line(pad+line, style)
	}
}

// say prints one statement of the answer.
func (t *terminal) say(text, style string) {
	if !t.interactive {
		t.print(t.out, text)
		return
	}
	t.open()
	t.wrap(text, 2, style)
}

// breakText cuts text into pieces of width runes, for values such as paths
// where a break at a space means nothing.
func breakText(text string, width int) []string {
	width = max(1, width)
	runes := []rune(text)
	var lines []string
	for len(runes) > width {
		lines = append(lines, string(runes[:width]))
		runes = runes[width:]
	}
	return append(lines, string(runes))
}

// labeled prints "label text" in which the label and the text are styled
// apart. Continuation lines are indented under the text. Prose wraps at
// spaces; a value such as a path is cut wherever the line ends.
func (t *terminal) labeled(w io.Writer, label, labelStyle, text, textStyle string, value bool) {
	if !t.interactive {
		t.print(w, label+" "+text)
		return
	}
	t.open()
	split := wrapText
	if value {
		split = breakText
	}
	for i, line := range split(label+" "+text, t.width()-5) {
		if i == 0 {
			rest := strings.TrimPrefix(line, label)
			t.print(w, "  "+t.style(label, labelStyle)+t.style(rest, textStyle))
		} else {
			t.print(w, "    "+t.style(line, textStyle))
		}
	}
}

// pair prints a labeled value, such as a path.
func (t *terminal) pair(label, value string) { t.labeled(t.out, label, "muted", value, "", true) }

// hint suggests the command to run next.
func (t *terminal) hint(label, command string) {
	t.labeled(t.out, label, "muted", command, "accent", false)
}

// notice reports progress on stderr, out of the way of the answer itself.
func (t *terminal) notice(text string) {
	if !t.interactive {
		t.print(t.err, text)
		return
	}
	t.open()
	for _, line := range wrapText(text, t.width()-3) {
		t.print(t.err, "  "+t.style(line, "muted"))
	}
}

func (t *terminal) warning(text string) { t.labeled(t.err, "Warning:", "warning", text, "", false) }

// failure reports the error that ended the run.
func (t *terminal) failure(err error) {
	t.gap()
	t.labeled(t.err, "Error:", "error", err.Error(), "", false)
	t.close()
}

func (t *terminal) heading(text string) {
	if t.interactive {
		t.gap()
		t.wrap(text, 2, "accent")
		t.gap()
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
	t.open()
	if t.width()-prefix < 18 {
		for i, row := range rows {
			if i > 0 {
				t.gap()
			}
			t.wrap(strings.Join(row[:columns], " -> "), 2, "accent")
			t.wrap(row[columns], 4, "")
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
		t.print(t.err, label+"...")
		return func() {}
	}
	t.open()
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

var banner = []string{"  ____ __________ ___", " / __ `/ ___/ __ `__ \\", "/ /_/ (__  ) / / / / /", "\\__,_/____/_/ /_/ /_/"}

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
			"Reads AIR_SDKS from ~/.airsdk/airsdkmanager.cfg.",
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
