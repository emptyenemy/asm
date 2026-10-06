package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
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
