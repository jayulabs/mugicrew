package transcript

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"jayulabs.org/mugicrew/claudecode"
)

// FollowOpts is how Follow runs: the render Opts (Width is re-read from Width each
// poll when set), Redraw to erase and reprint the open run on a tty, Stop (the
// session's end was recorded; it does not end a follow that went into a
// continued-in destination) and Quit (the human quit; it always does) asked each
// poll, and Poll between polls.
type FollowOpts struct {
	Opts
	Redraw bool
	Width  func() int
	Stop   func() bool
	Quit   func() bool
	Poll   time.Duration
}

// Follow prints s's header, then its transcript digest as claude appends to it,
// until Stop. Settled items are rendered once, at the width then, and never reprinted. The open run of tool calls is the
// tail: with Redraw each poll that changes it, or the width, erases its rows and
// reprints it; piped it prints only when the follow ends (pending as · no result).
func Follow(s *Session, f FollowOpts, out io.Writer) error {
	o := f.Opts
	fmt.Fprintln(out, Header(s, o))
	var (
		tail          = claudecode.NewTail(s.ID)
		items         []claudecode.Item
		lastW         int // width the open run was drawn at
		section       = "header"
		open          []string // open run as last printed (Redraw)
		waited, final bool
	)
	for !final {
		stop := f.Stop() // asked before reading, so lines written before stop are drained
		quit := f.Quit != nil && f.Quit()
		if f.Width != nil {
			o.Width = max(f.Width(), 40)
		}
		its := tail.Poll()
		if !tail.Started() && !waited {
			fmt.Fprintln(out, o.dim("waiting for transcript…")) // the body opens with its own blank line
			waited = true
		}
		items = append(items, its...) // only unsettled items: the open run and what's new
		fresh := len(its) > 0
		final = quit || (stop && !tail.Handed()) || tail.Ended()
		if fresh || (final && !f.Redraw) || (f.Redraw && o.Width != lastW) {
			settled, op, sec := bodyParts(items, section, o)
			if f.Redraw && len(open) > 0 {
				rows := 0 // a pane that narrowed since wraps the old lines onto more rows
				for _, l := range open {
					rows += max(1, (ansi.StringWidth(l)+o.Width-1)/o.Width)
				}
				fmt.Fprintf(out, "\x1b[%dA\r\x1b[J", rows)
				open = nil
			}
			if len(settled) > 0 {
				fmt.Fprintln(out, strings.Join(settled, "\n"))
			}
			section = sec
			items = items[len(items)-openRun(items):] // printed lines are never re-rendered
			if (f.Redraw || final) && len(op) > 0 {
				fmt.Fprintln(out, strings.Join(op, "\n"))
				open = op
			}
			lastW = o.Width
		}
		if !final {
			time.Sleep(f.Poll)
		}
	}
	return nil
}

// openRun is how many trailing items belong to the open run of tool calls.
func openRun(items []claudecode.Item) int {
	n := 0
	for n < len(items) && (items[len(items)-1-n].Kind == "tool" || items[len(items)-1-n].Kind == "result") {
		n++
	}
	return n
}
