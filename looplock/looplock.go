// Package looplock is the single-loop lock (ADR 0053 §1): every dispatch loop
// takes flock(LOCK_EX|LOCK_NB) on <BEADS_DIR>/mugicrew-loop.lock before it
// polls, so at most one loop runs per Bead Graph (ADR 0089). The lock is the only
// single-loop authority. The file body (pid, state, and the pinned build's rev
// and store path, ADR 0060 §5) is valid only while
// the lock is held, so readers probe the lock first; dispatch
// never reads it (ADR 0031). No stale-lock cleanup: the kernel drops a flock on
// any death, and the fd is close-on-exec so efforts never inherit it; only a
// restart hands it on, clearing that flag just before it execs (ADR 0062 §5).
// MainlineBuild builds the pinned binary a loop runs (ADR 0060 §1).
package looplock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/git"
)

// Loop states (ADR 0050). Off is never written: it is what an unheld lock reads.
const (
	Off      = "off"
	On       = "on"
	Draining = "draining"
	Limited  = "limited" // polling, claude dispatch held until a Plan usage reset
)

// Status is a loop's state as read through the lock: Pid is set only when held.
// Rev and StorePath name the pinned build the holder runs (ADR 0060 §5); an
// installed loop has only StorePath, its own executable (ADR 0295 §3); both are
// empty for a holder that has not written its body. Watch marks a watched loop (ADR 0090
// §4), and Source the git common dir it watches (§7). Inflight is a draining loop's
// in-flight bead titles. RestartTo is the rev a restart drain will exec onto, and
// FailedRev the main rev whose build failed (ADR 0063 §1–2); both empty otherwise.
// A limited loop is live like an on one: it carries the in-flight titles too, and
// ResetsAt (when the hold ends, before the resume's minute of slack) with LimitSource
// (message, usage or fallback); a draining write drops both.
type Status struct {
	State     string   `json:"state"`
	Pid       int      `json:"pid"`
	Rev       string   `json:"rev,omitempty"`        // main's full SHA the loop was built from
	StorePath string   `json:"store_path,omitempty"` // os.Executable() of the loop
	Watch     bool     `json:"watch,omitempty"`      // --watch: follows the source repo's lands
	Source    string   `json:"source,omitempty"`     // the watched source repo's git common dir
	Inflight  []string `json:"inflight,omitempty"`
	RestartTo string   `json:"restart_to,omitempty"`
	FailedRev string   `json:"failed_rev,omitempty"`

	ResetsAt    time.Time `json:"resets_at,omitzero"`
	LimitSource string    `json:"limit_source,omitempty"`
}

// StopDrain reports a drain a stop began, which a start can cancel; a restart
// drain (RestartTo set) is not one.
func (s Status) StopDrain() bool { return s.State == Draining && s.RestartTo == "" }

// Section groups a status Line; Lines put a blank row between sections (ADR 0295 §1).
type Section string

// The closed set of sections, in print order.
const (
	SecState    Section = "state"
	SecBuild    Section = "build"
	SecInflight Section = "in flight"
	SecLimit    Section = "limit"
	SecStale    Section = "stale"
	SecWatch    Section = "watch"
)

// Line is one labelled status field.
type Line struct {
	Section      Section
	Label, Value string
}

// Lines is the `loop status` layout as structured lines (ADR 0295 §2–3), the single
// formatter behind Text and the TUI loop menu (ADR 0288 §4). Off, or a lock held with
// pid 0 (body not yet written), is just the state line; empty sections are omitted.
func (st Status) Lines() []Line {
	if st.State == Off || st.Pid == 0 {
		return []Line{{SecState, "state", st.State}}
	}
	state := st.State
	if st.FailedRev != "" {
		state += " (stale)"
	}
	ls := []Line{{SecState, "state", state}}
	if st.RestartTo != "" {
		ls = append(ls, Line{SecState, "restarting onto", "main " + Short(st.RestartTo)})
	}
	ls = append(ls, Line{SecState, "pid", fmt.Sprint(st.Pid)})
	if st.Rev != "" {
		ls = append(ls, Line{SecBuild, "main", Short(st.Rev)})
	}
	if st.StorePath != "" {
		ls = append(ls, Line{SecBuild, "store", st.StorePath})
	}
	ls = append(ls, Line{SecInflight, "in flight", fmt.Sprint(len(st.Inflight))})
	if st.State == Limited {
		src := st.LimitSource
		if src == "fallback" {
			src += " (1h, no reset time found)"
		}
		ls = append(ls, Line{SecLimit, "held by usage limit until", st.ResetLabel()}, Line{SecLimit, "reset time from", src})
	}
	if st.FailedRev != "" {
		ls = append(ls, Line{SecStale, "stale", fmt.Sprintf("build of main %s failed (see build.failed in events)", Short(st.FailedRev))})
	}
	if st.Watch {
		ls = append(ls, Line{SecWatch, "watching", strings.TrimSuffix(st.Source, "/.git") + " " + git.MainlineOf(st.Source)})
	}
	return ls
}

// Text is the plain `loop status` text: one `label: value` per line, a blank line
// between sections (ADR 0295), shared with the TUI loop menu through Lines.
func (st Status) Text() string {
	var b strings.Builder
	var prev Section
	for i, l := range st.Lines() {
		if i > 0 && l.Section != prev {
			b.WriteByte('\n')
		}
		prev = l.Section
		b.WriteString(l.Label + ": " + l.Value + "\n")
	}
	return b.String()
}

// ResetLabel is a limited loop's reset time as the status surfaces show it: local
// 15:04, or Jan 2 15:04 when not today, with no zone.
func (s Status) ResetLabel() string {
	t := s.ResetsAt.Local()
	if t.Format(time.DateOnly) != time.Now().Format(time.DateOnly) {
		return t.Format("Jan 2 15:04")
	}
	return t.Format("15:04")
}

// HeldError is Acquire's refusal: another process holds the lock. `loop start`
// cancels a stop drain before it gets here (ADR 0236); a restart drain is refused.
type HeldError struct{ Status Status }

func (e *HeldError) Error() string {
	if e.Status.State == Draining {
		return fmt.Sprintf("loop is draining (pid %d)", e.Status.Pid)
	}
	return fmt.Sprintf("loop is already on (pid %d)", e.Status.Pid)
}

// Lock is a held single-loop lock; it remembers its holder's store path and
// watched source so every body rewrite keeps them.
type Lock struct {
	f                 *os.File
	storePath, source string
}

// Resolve is the lock path for the Bead Graph main exported as BEADS_DIR (Spec
// FR10): inside the graph dir, so every Workspace entry point (or
// redirect) that resolves one graph contends on one file, whatever path names it.
// bd's own .gitignore already ignores *.lock there.
// While a pre-ADR-0089 loop holds the old <common>/mugicrew/loop.lock, that lock
// wins: status, start and stop see the live loop, and its restart
// onto this image adopts the fd it handed on. The next stop and start moves the
// loop onto the graph's lock.
func Resolve(common string) (string, error) {
	return ResolveIn(common, os.Getenv("BEADS_DIR"))
}

// ResolveIn is Resolve for a graph dir the caller already holds (dir "" is no graph).
func ResolveIn(common, dir string) (string, error) {
	legacy := filepath.Join(common, "mugicrew", "loop.lock")
	if st, err := Read(legacy); err == nil && st.State != Off {
		return legacy, nil
	}
	if dir == "" {
		return "", errors.New("no Bead Graph: BEADS_DIR is unset")
	}
	return filepath.Join(dir, "mugicrew-loop.lock"), nil
}

// Acquire takes the lock at path and records this process as on, running the
// build rev/storePath names, watching source (empty: unwatched). A held lock is
// a *HeldError carrying the holder's status; any other failure is a plain error.
func Acquire(path, rev, storePath, source string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644) // os opens O_CLOEXEC
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		defer f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &HeldError{parse(f)}
		}
		return nil, err
	}
	lk := &Lock{f, storePath, source}
	if err := lk.Write(Status{State: On, Rev: rev}); err != nil {
		f.Close()
		return nil, err
	}
	return lk, nil
}

// Adopt takes over the lock an exec handed this image as fd (ADR 0062 §5), in
// place of Acquire, and records this process as on, running the build
// rev/storePath names, watching source (empty: unwatched). It refuses unless fd
// is open on path and itself holds the flock, so the lock never lapsed. The fd goes back to close-on-exec.
func Adopt(fd int, path, rev, storePath, source string) (*Lock, error) {
	var got, want syscall.Stat_t
	if err := syscall.Fstat(fd, &got); err != nil {
		return nil, fmt.Errorf("lock fd %d: %w", fd, err)
	}
	if err := syscall.Stat(path, &want); err != nil {
		return nil, err
	}
	if got.Dev != want.Dev || got.Ino != want.Ino {
		return nil, fmt.Errorf("lock fd %d is not open on %s", fd, path)
	}
	// Held under this pid (an exec keeps it, and the old image wrote it) and
	// re-lockable through fd: fd is the holder. Never flock fd first, or an
	// unlocked fd would quietly take a lapsed lock.
	if st, err := Read(path); err != nil {
		return nil, err
	} else if st.State == Off || st.Pid != os.Getpid() {
		return nil, fmt.Errorf("lock fd %d does not hold %s", fd, path)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("lock fd %d does not hold %s: %w", fd, path, err)
	}
	syscall.CloseOnExec(fd)
	lk := &Lock{os.NewFile(uintptr(fd), path), storePath, source}
	if err := lk.Write(Status{State: On, Rev: rev}); err != nil {
		lk.Close()
		return nil, err
	}
	return lk, nil
}

// InheritFd clears close-on-exec on the held lock's fd and returns its number,
// for the exec that hands the lock to a new image (Adopt). Call it right before
// the exec: any child started after it inherits the lock.
func (l *Lock) InheritFd() (int, error) {
	fd := l.f.Fd()
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFD, 0); errno != 0 {
		return 0, errno
	}
	return int(fd), nil
}

// Write rewrites the held lock's body to st (state, rev, in-flight titles,
// restart_to, failed_rev) with this process's pid, store path and watched source:
// what loop status and the TUI show.
func (l *Lock) Write(st Status) error {
	st.Pid, st.StorePath, st.Watch, st.Source = os.Getpid(), l.storePath, l.source != "", l.source
	body, _ := json.Marshal(st)
	fi, err := l.f.Stat()
	if err != nil {
		return err
	}
	// Space-pad over every old byte, never trim: a reader never sees an empty body
	// or a torn tail (JSON allows trailing whitespace).
	// ponytail: the file only grows, to the longest body this lock has held.
	if pad := int(fi.Size()) - len(body); pad > 0 {
		body = append(body, strings.Repeat(" ", pad)...)
	}
	_, err = l.f.WriteAt(body, 0)
	return err
}

// Close releases the lock. The file stays: its body is void once unheld.
func (l *Lock) Close() error { return l.f.Close() }

// Read probes the lock at path: acquirable (or no file) is off whatever the file
// says, and the probe lets go at once; held is the holder's status from the file.
// ponytail: the probe briefly holds the lock, so a loop starting in that instant
// is refused (as if on); flock has no non-taking test. Retry Acquire if it bites.
func Read(path string) (Status, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{State: Off}, nil
	}
	if err != nil {
		return Status{}, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return Status{State: Off}, nil // Close drops the probe's lock
	case errors.Is(err, syscall.EWOULDBLOCK):
		return parse(f), nil
	}
	return Status{}, err
}

// Pid is the pid the lock's body names, read without probing the lock, so it
// never races an Acquire. The body is void once unheld: a match proves the lock
// only for a live process that could have written it (a `loop start` child).
func Pid(path string) int {
	var st Status
	body, _ := os.ReadFile(path)
	json.Unmarshal(body, &st)
	return st.Pid
}

// parse reads a held lock's body. A holder that has locked but not yet written
// reads as on with no pid.
func parse(f *os.File) Status {
	st := Status{State: On}
	body, _ := io.ReadAll(io.NewSectionReader(f, 0, 1<<20))
	json.Unmarshal(body, &st)
	return st
}

// Short is r cut to seven characters, a rev as displayed.
func Short(r string) string { return r[:min(7, len(r))] }
