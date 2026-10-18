package transcript

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/mdrender"
)

// Opts is how Render lays out: Width cells (ignored when !Wrap), SGR on or off,
// wrapping on or off.
type Opts struct {
	Width       int
	Color, Wrap bool
}

// sgr wraps s in an attribute and its own off code, so nested styles survive.
func (o Opts) sgr(on, off, s string) string {
	if !o.Color {
		return s
	}
	return "\x1b[" + on + "m" + s + "\x1b[" + off + "m"
}
func (o Opts) fg(c, s string) string { return o.sgr("38;5;"+c, "39", s) }
func (o Opts) bold(s string) string  { return o.sgr("1", "22", s) }
func (o Opts) dim(s string) string   { return o.fg("244", s) }
func (o Opts) red(s string) string   { return o.fg("203", s) }

// band fills s to the full width on the 236 background.
func (o Opts) band(s string) string {
	if !o.Color {
		return s
	}
	if o.Wrap {
		s += strings.Repeat(" ", max(0, o.Width-ansi.StringWidth(s)))
	}
	return "\x1b[48;5;236m" + s + "\x1b[49m"
}

// wrapLines is s's logical lines, each wrapped to w cells when wrapping is on.
func (o Opts) wrapLines(s string, w int) []string {
	if !o.Wrap {
		return strings.Split(s, "\n")
	}
	return strings.Split(ansi.Wrap(s, max(w, 10), ""), "\n")
}

func durs(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func took(d time.Duration) string {
	d = max(d, 0)
	if d < 10*time.Second {
		if d.Round(100*time.Millisecond) < 10*time.Second {
			return fmt.Sprintf("%.1fs", d.Seconds())
		}
		d = 10 * time.Second // 9.95s+ would print 10.0s
	}
	return durs(d)
}

// Header is a session's one-line header (ADR 0250 §1, ADR 0251 §4).
func Header(s *Session, o Opts) string {
	dur, out := s.Dur, fmt.Sprintf("ended (exit %d)", s.Exit)
	switch s.Outcome {
	case "unended":
		dur, out = 0, o.fg("180", "no end recorded")
	case "failed":
		out = o.red(fmt.Sprintf("failed (exit %d)", s.Exit))
	}
	what := strings.Join(strings.Fields(s.Kind+" "+s.Crewmate), " ")
	ds := durs(max(dur, 0))
	if s.Outcome == "unended" {
		ds = "—"
	}
	h := fmt.Sprintf(" %s  %s  %s  %s", s.Start.Local().Format("Jan 2 15:04"), what, out, ds)
	id := s.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return o.sgr("7", "27", " "+s.Mode+" ") + o.bold(h) + "  " + o.dim(id)
}

// Render is one session's lines: its header, then items' digest, or the event
// log's fallback when the transcript is gone (items nil). An unended session says
// only that it has none (the follow view has its own waiting line).
func Render(s *Session, items []claudecode.Item, found bool, o Opts) []string {
	out := []string{Header(s, o)}
	switch {
	case found:
		return append(out, body(items, o)...)
	case s.Outcome == "unended":
		return append(out, "", o.dim("no transcript (not yet written, or pruned) · no end recorded"))
	}
	return append(out, "", o.dim("transcript pruned (claude keeps ~30 days) · from the event log:"), fallback(s, o))
}

func fallback(s *Session, o Opts) string {
	if s.Outcome != "failed" {
		return o.dim("(nothing kept: no result or stderr is recorded for this end)")
	}
	out := []string{o.bold("result")}
	if s.Result == "" {
		out = append(out, "  "+o.dim("(empty)"))
	} else {
		for _, l := range o.wrapLines(s.Result, o.Width-2) {
			out = append(out, "  "+l)
		}
	}
	if s.Stderr != "" {
		out = append(out, o.bold("stderr tail"))
		ls := strings.Split(strings.TrimRight(s.Stderr, "\n"), "\n")
		if len(ls) > 12 {
			out = append(out, "  "+o.dim(fmt.Sprintf("… %d earlier lines", len(ls)-12)))
			ls = ls[len(ls)-12:]
		}
		for _, l := range ls {
			out = append(out, "  "+o.red(o.cut(l, o.Width-2)))
		}
	}
	return strings.Join(out, "\n")
}

func (o Opts) cut(s string, w int) string {
	if o.Wrap && w > 1 {
		return ansi.Truncate(s, w, "…")
	}
	return s
}

// body is the digest of items (ADR 0250 §2–§7): a blank line wherever the block
// kind changes, runs of tool calls paired with their results by tool_use_id.
func body(items []claudecode.Item, o Opts) []string {
	settled, open, _ := bodyParts(items, "header", o)
	return append(settled, open...)
}

// bodyParts is body split at the open run: the trailing tool calls not yet flushed
// by a later block, which a following view redraws as their results arrive. It
// starts in section (the kind of block before items) and returns the section it
// ended in, so a caller can render items in batches.
func bodyParts(items []claudecode.Item, section string, o Opts) (settled, open []string, end string) {
	var out []string
	var calls []claudecode.Item
	results := map[string]claudecode.Item{}
	enter := func(sec string) {
		if sec != section {
			out = append(out, "")
			section = sec
		}
	}
	flush := func() {
		for _, c := range calls {
			out = append(out, toolRow(c, results, o)...)
		}
		calls, results = nil, map[string]claudecode.Item{}
	}
	for _, it := range items {
		switch it.Kind {
		case "prompt", "human", "interrupt":
			flush()
			enter("human")
			label, rest := o.dim(fmt.Sprintf("▍launch prompt · %d lines", len(strings.Split(it.Text, "\n")))), []string(nil)
			if it.Kind != "prompt" {
				ls := o.wrapLines(it.Text, o.Width-12)
				label, rest = o.fg("212", o.bold("▍"+ls[0])), ls[1:]
			}
			ts := it.TS.Format("15:04:05")
			pad := 2
			if o.Wrap {
				pad = max(1, o.Width-ansi.StringWidth(label)-len(ts))
			}
			out = append(out, label+strings.Repeat(" ", pad)+o.dim(ts))
			for _, l := range rest {
				out = append(out, o.fg("212", o.bold("▍"+l)))
			}
		case "text":
			flush()
			enter("claude")
			w := o.Width - 2
			if !o.Wrap {
				w = 10000 // never reflows
			}
			for _, l := range strings.Split(strings.Trim(mdrender.Render(it.Text, w), "\n"), "\n") {
				out = append(out, " "+l)
			}
		case "tool":
			enter("tool")
			calls = append(calls, it)
		case "result":
			for _, c := range calls { // a result whose call was flushed has no row
				if c.ID == it.ID {
					enter("tool")
					results[it.ID] = it
				}
			}
		case "continued":
			flush()
			enter("continued")
			id := it.Text
			out = append(out, o.dim("↪ continued in "+id[:min(len(id), 8)]+" (bg)"))
		case "apierr":
			flush()
			enter("claude")
			out = append(out, "  "+o.red("✗ API error: "+it.Text))
		}
	}
	n := len(out)
	flush()
	if !o.Color {
		for i, l := range out {
			out[i] = ansi.Strip(l)
		}
	}
	return out[:n:n], out[n:], section
}

// toolRow is one call in full on the band, its status right-aligned on the first
// line (inline when not wrapping) and, if it failed, its first 4 error lines.
func toolRow(c claudecode.Item, results map[string]claudecode.Item, o Opts) []string {
	r, done := results[c.ID]
	st := o.dim("· no result")
	if done && r.IsErr {
		st = o.red("✗ " + took(r.TS.Sub(c.TS)))
	} else if done {
		st = o.fg("114", "✓ "+took(r.TS.Sub(c.TS)))
	}
	in := strings.Split(c.Full, "\n")
	head := " " + o.fg("111", o.bold(c.Name)) + " "
	var first []string
	if o.Wrap {
		first = o.wrapLines(in[0], o.Width-ansi.StringWidth(head)-ansi.StringWidth(st)-3)
	} else {
		first = []string{in[0]}
	}
	l0 := head + first[0]
	var out []string
	if o.Wrap {
		out = append(out, o.band(l0+strings.Repeat(" ", max(1, o.Width-ansi.StringWidth(l0)-ansi.StringWidth(st)-1))+st+" "))
	} else {
		out = append(out, o.band(l0+" "+st))
	}
	for _, l := range append(first[1:], in[1:]...) {
		for _, x := range o.wrapLines(l, o.Width-4) {
			out = append(out, o.band("   "+x))
		}
	}
	if done && r.IsErr {
		el := strings.Split(r.Text, "\n")
		for _, l := range el[:min(len(el), 4)] {
			out = append(out, o.band("   "+o.red(o.cut(l, o.Width-4))))
		}
	}
	return out
}
