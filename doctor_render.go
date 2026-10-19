package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

// fix is a doctor row's remedy (ADR 0302): safe when doctor fix may run argv,
// manual when the human follows steps. For a safe fix, steps is the fallback if
// the command fails.
type fix struct {
	safe  []string // argv doctor fix runs; nil for a manual fix
	steps []string // manual steps, one bullet each
	more  string   // "docs/install.md#<anchor>", or ""
}

// manual is a manual fix of steps with an optional install.md anchor.
func manual(more string, steps ...string) fix {
	if more != "" {
		more = "docs/install.md#" + more
	}
	return fix{steps: steps, more: more}
}

// style is how the report is drawn: colour on or off, and the wrap width.
type style struct {
	color bool
	width int
}

// plainWidth is the wrap width off a terminal.
const plainWidth = 80

// styleFor is colour and the terminal's width when w is a terminal, else plain
// text at 80 columns (ADR 0301 §4).
func styleFor(w io.Writer) style {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return style{width: plainWidth}
	}
	s := style{color: true, width: plainWidth}
	if cols, _, err := term.GetSize(f.Fd()); err == nil && cols > 20 {
		s.width = cols
	}
	return s
}

func (s style) c(code, txt string) string {
	if !s.color {
		return txt
	}
	return "\x1b[" + code + "m" + txt + "\x1b[0m"
}

func (s style) verdict(v status) string {
	switch v {
	case stOK:
		return s.c("32", string(v))
	case stWarn:
		return s.c("33", string(v))
	case stFail:
		return s.c("1;31", string(v))
	}
	return s.c("2", string(v))
}

func (s style) bold(t string) string { return s.c("1", t) }
func (s style) dim(t string) string  { return s.c("2", t) }
func (s style) cmd(t string) string  { return s.c("36", t) }

// wrap breaks text at width, the first line led by first and the rest by rest
// (both plain, so their length is their width).
func (s style) wrap(text, first, rest string) []string {
	var out []string
	cur, lead := first, len([]rune(first))
	n := lead
	for _, w := range strings.Fields(text) {
		if n > lead && n+1+len([]rune(w)) > s.width {
			out = append(out, cur)
			cur, lead = rest, len([]rune(rest))
			n = lead
		}
		if n > lead {
			cur += " "
			n++
		}
		cur += w
		n += len([]rune(w))
	}
	return append(out, cur)
}

// rollCall is a row's one line: "<verdict>  <name>: <detail>".
func (s style) rollCall(r result) string {
	return fmt.Sprintf("%s  %s: %s", s.verdict(r.status), r.name, r.detail)
}

// needsFix is whether r gets a To-fix item: warn and FAIL only.
func needsFix(r result) bool { return r.status == stWarn || r.status == stFail }

// toFixItem is the numbered To-fix item for a warn/FAIL row, no trailing newline.
func (s style) toFixItem(n int, r result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s\n", s.bold(fmt.Sprintf("%d.", n)), s.verdict(r.status), s.bold(r.name))
	for _, l := range s.wrap("Expected: "+r.pre+".", "   ", "   ") {
		fmt.Fprintln(&b, l)
	}
	if r.fix.safe != nil {
		fmt.Fprintln(&b, "   Safe — doctor fix runs:")
		fmt.Fprintf(&b, "     %s\n", s.cmd(strings.Join(r.fix.safe, " ")))
	} else {
		fmt.Fprintln(&b, "   Manual — do it yourself:")
		for _, st := range r.fix.steps {
			for _, l := range s.wrap(st, "     • ", "       ") {
				fmt.Fprintln(&b, l)
			}
		}
	}
	if r.fix.more != "" {
		fmt.Fprintf(&b, "   %s\n", s.dim("More: "+r.fix.more))
	}
	return strings.TrimRight(b.String(), "\n")
}

// safeCount is how many warn/FAIL rows carry a safe fix.
func safeCount(rows []result) int {
	n := 0
	for _, r := range rows {
		if needsFix(r) && r.fix.safe != nil {
			n++
		}
	}
	return n
}

// summary is the report's last line (ADR 0301 §3); nothing is pluralised.
func (s style) summary(rows []result) string {
	var f, w, o int
	for _, r := range rows {
		switch r.status {
		case stFail:
			f++
		case stWarn:
			w++
		case stOK:
			o++
		}
	}
	if f == 0 && w == 0 {
		return s.c("32", fmt.Sprintf("all %d checks ok", o))
	}
	var parts []string
	if f > 0 {
		parts = append(parts, s.c("1;31", fmt.Sprintf("%d FAIL", f)))
	}
	if w > 0 {
		parts = append(parts, s.c("33", fmt.Sprintf("%d warn", w)))
	}
	parts = append(parts, fmt.Sprintf("%d ok", o))
	line := strings.Join(parts, ", ")
	if n := safeCount(rows); n > 0 {
		line += fmt.Sprintf(" · %d safe fix: run %s", n, s.cmd("mugicrew doctor fix"))
	}
	return line
}

// report is the full doctor output: roll call, To fix, summary.
func (s style) report(rows []result) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintln(&b, s.rollCall(r))
	}
	n := 0
	for _, r := range rows {
		if !needsFix(r) {
			continue
		}
		if n == 0 {
			fmt.Fprintf(&b, "\n%s\n", s.bold("To fix"))
		}
		n++
		fmt.Fprintf(&b, "\n%s\n", s.toFixItem(n, r))
	}
	fmt.Fprintf(&b, "\n%s\n", s.summary(rows))
	return b.String()
}
