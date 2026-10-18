package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/transcript"
)

// isTerminal and startPager are seams for the verb tests.
var (
	isTerminal = func(f *os.File) bool { return term.IsTerminal(f.Fd()) }
	// startPager runs cmd (via sh -c) with out as its stdout; the caller writes the
	// view to the returned pipe and calls done, which closes it and waits.
	startPager = func(cmd string, out *os.File) (io.Writer, func() error, error) {
		c := exec.Command("sh", "-c", cmd)
		c.Stdout, c.Stderr = out, os.Stderr
		in, err := c.StdinPipe()
		if err != nil {
			return nil, nil, err
		}
		if err := c.Start(); err != nil {
			return nil, nil, err
		}
		return in, func() error { in.Close(); return c.Wait() }, nil
	}
)

// watchPaneEnv marks the TUI's Watch pane (session.Watch sets it): there every
// non-following output stays up until q, since the pane closes when the verb exits.
const watchPaneEnv = "MUGICREW_WATCH_PANE"

func inWatchPane() bool { return os.Getenv(watchPaneEnv) != "" }

// pagerCmd is $PAGER, else less -FRX (less -RX in the Watch pane: -F would quit on one screen).
func pagerCmd() string {
	if p := os.Getenv("PAGER"); p != "" {
		return p
	}
	if inWatchPane() {
		return "less -RX"
	}
	return "less -FRX"
}

// holdPane waits for q on a raw tty stdin; in the Watch pane only, and a no-op off a tty.
func holdPane() {
	if !inWatchPane() {
		return
	}
	raw, quit, restore := rawStdin()
	defer restore()
	if raw {
		<-quit
	}
}

// runTranscript runs the verb; in the Watch pane a no-sessions answer or an error is
// shown and held until q instead of closing the pane at once.
func runTranscript(argv []string, w io.Writer) error {
	err := transcriptVerb(argv, w)
	if err != nil && inWatchPane() {
		fmt.Fprintln(w, err)
		holdPane()
	}
	return err
}

// runTranscript is `mugicrew transcript [--no-follow] [--session <prefix> | --last]
// [--no-pager] <bead>` (ADR 0251): every session the bead ran, oldest first, as one
// digest. SGR and wrapping are off unless w is a tty (and NO_COLOR is unset for SGR).
// An unended last session is followed (ADR 0251) unless --no-follow; a finished view on
// a tty pages through $PAGER unless --no-pager.
func transcriptVerb(argv []string, w io.Writer) error {
	fs := flag.NewFlagSet("transcript", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var noFollow, noPager, lastOnly bool
	var prefix string
	fs.BoolVar(&noFollow, "no-follow", false, "")
	fs.BoolVar(&noPager, "no-pager", false, "")
	fs.BoolVar(&lastOnly, "last", false, "")
	fs.StringVar(&prefix, "session", "", "")
	const usage = "usage: mugicrew transcript [--no-follow] [--session <prefix> | --last] [--no-pager] <bead>"
	if fs.Parse(argv) != nil || fs.NArg() != 1 || (lastOnly && prefix != "") {
		return errors.New(usage)
	}
	bead := fs.Arg(0)
	inheritGraph()
	if _, err := showBead(bead); err != nil {
		if bd.IsNotFound(err) {
			return fmt.Errorf("mugicrew: no bead %s", bead)
		}
		return err
	}
	logPath, err := eventlog.Resolve()
	if err != nil {
		return err
	}
	ss, err := transcript.Sessions(logPath, bead)
	if err != nil {
		return err
	}
	if prefix != "" {
		var m []*transcript.Session
		for _, s := range ss {
			if strings.HasPrefix(s.ID, prefix) {
				m = append(m, s)
			}
		}
		if len(m) != 1 {
			what := "unknown"
			if len(m) > 1 {
				what = "ambiguous"
			}
			return fmt.Errorf("mugicrew: %s session %q for %s", what, prefix, bead)
		}
		ss = m
	}
	if lastOnly && len(ss) > 0 {
		ss = ss[len(ss)-1:]
	}
	if len(ss) == 0 {
		fmt.Fprintf(w, "no sessions recorded for %s\n", bead)
		holdPane()
		return nil
	}
	o := transcript.Opts{}
	if f, ok := w.(*os.File); ok && isTerminal(f) {
		// Colour and width are read from the real stdout before the pager pipe opens.
		o.Wrap = true
		o.Color = os.Getenv("NO_COLOR") == ""
		o.Width, _, _ = term.GetSize(f.Fd())
		o.Width = max(o.Width, 40)
	}
	last := ss[len(ss)-1]
	follow := !noFollow && last.Outcome == "unended"
	if f, ok := w.(*os.File); ok && o.Wrap && !noPager && !follow {
		pw, done, err := startPager(pagerCmd(), f)
		if err != nil {
			return err
		}
		defer func() { done() }() // a pager quit early (q) closes the pipe; that is not an error
		w = pw
	}
	for i, s := range ss {
		if i > 0 {
			fmt.Fprint(w, "\n\n")
		}
		if follow && s == last {
			return followTranscript(logPath, s, o, w)
		}
		items, found := claudecode.Items(s.ID)
		fmt.Fprintln(w, strings.Join(transcript.Render(s, items, found, o), "\n"))
	}
	return nil
}

// followTranscript follows s, the last shown session and unended, until the event log
// records its end. On a tty stdin it then prints "run ended" and waits for q.
func followTranscript(logPath string, s *transcript.Session, o transcript.Opts, w io.Writer) error {
	f, isFile := w.(*os.File)
	raw, quit, restore := rawStdin()
	defer restore()
	out := w
	if raw && isFile && isTerminal(f) {
		out = crlf{w} // the raw tty won't turn \n into \r\n; a pipe or file must keep \n
	}
	ended := sessionEnder(logPath, s.ID)
	fo := transcript.FollowOpts{Opts: o, Redraw: o.Wrap, Poll: followPoll, Stop: ended, Quit: func() bool {
		select {
		case <-quit:
			return true
		default:
			return false
		}
	}}
	if isFile && o.Wrap {
		fo.Width = func() int { w, _, _ := term.GetSize(f.Fd()); return w }
	}
	if err := transcript.Follow(s, fo, out); err != nil {
		return err
	}
	select {
	case <-quit:
	default:
		if raw {
			fmt.Fprintln(out, "run ended")
			<-quit
		}
	}
	return nil
}

// followPoll is the follow's poll, claude Follow's interval.
var followPoll = 250 * time.Millisecond

// sessionEnder returns a poll reporting whether the event log at path last records
// session id as over: session.ended/failed or hitl.exited/failed, not followed by a
// hitl.resumed. After the first poll it reads only the live file's appended tail; a
// shrunk file (rotated) is re-read whole, with .1, so an end that moved there is
// not missed.
func sessionEnder(path, id string) func() bool {
	var off int64
	over := false
	note := func(msg, sess string) {
		if sess != id {
			return
		}
		switch msg {
		case "session.ended", "session.failed", "hitl.exited", "hitl.failed":
			over = true
		case "hitl.resumed":
			over = false
		}
	}
	return func() bool {
		fi, err := os.Stat(path)
		if err != nil {
			return over
		}
		if fi.Size() < off {
			off, over = 0, false
			evs, _, _ := eventlog.Read(path)
			for _, ev := range evs {
				s, _ := ev.Raw["session"].(string)
				note(ev.Msg, s)
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return over
		}
		defer f.Close()
		f.Seek(off, io.SeekStart)
		b, _ := io.ReadAll(f)
		b = b[:bytes.LastIndexByte(b, '\n')+1] // whole lines only; a torn tail waits
		off += int64(len(b))
		for _, l := range bytes.Split(b, []byte("\n")) {
			var ev struct{ Msg, Session string }
			if json.Unmarshal(l, &ev) == nil {
				note(ev.Msg, ev.Session)
			}
		}
		return over
	}
}

// crlf turns \n into \r\n: the pane's tty is raw so q and Ctrl-C arrive unbuffered.
type crlf struct{ w io.Writer }

func (c crlf) Write(p []byte) (int, error) {
	_, err := c.w.Write([]byte(strings.ReplaceAll(string(p), "\n", "\r\n")))
	return len(p), err
}

// rawStdin puts a tty stdin in raw mode (q and Ctrl-C arrive unbuffered) and
// returns whether it did, a channel closed when q or Ctrl-C is read, and the
// restore. With stdin not a tty it returns false, a never-closed channel, and a no-op.
func rawStdin() (bool, chan struct{}, func()) {
	quit := make(chan struct{})
	st, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return false, quit, func() {}
	}
	go func() {
		b := make([]byte, 1)
		for {
			if _, err := os.Stdin.Read(b); err != nil || b[0] == 'q' || b[0] == 3 {
				close(quit)
				return
			}
		}
	}()
	return true, quit, func() { term.Restore(os.Stdin.Fd(), st) }
}
