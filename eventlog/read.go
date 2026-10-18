package eventlog

// The reader side (Spec decision G): the Logs tab reads the file raw
// through here. Nothing recovers from it (ADR 0033): it is history only.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// Event is one parsed record. Attrs holds every attr for a detail view,
// time/level/msg first then the rest by key.
type Event struct {
	Time                          time.Time
	Level, Msg, Bead, Parent, Pid string
	Short                         string // the short error: err's first ": " segment, else result.subtype (FR42)
	Attrs                         []Attr
	Raw                           map[string]any // the decoded record (numbers as json.Number), for typed readers like stats
}

// Attr is one rendered attr; a group's sub-keys follow its header indented.
type Attr struct{ K, V string }

// Key is an event's identity (time+pid+msg): one process never emits two
// same-named events in the same nanosecond, so a selection keyed by it survives a
// reload or rotation that shifts indices (FR45).
type Key struct {
	time     int64
	pid, msg string
}

func (ev Event) Key() Key { return Key{ev.Time.UnixNano(), ev.Pid, ev.Msg} }

// Sig is the (size, mtime) of .1 and the live file: an unchanged Sig means
// nothing to re-read.
type Sig [2]struct{ size, mod int64 }

// Stat is path's Sig; a missing file reads as zero.
func Stat(path string) Sig {
	var s Sig
	for i, p := range Files(path) {
		if fi, err := os.Stat(p); err == nil {
			s[i].size, s[i].mod = fi.Size(), fi.ModTime().UnixNano()
		}
	}
	return s
}

// Read reads .1 then the live file as one stream in append order (ADR 0028 §4),
// skipping and counting lines that don't parse. A missing file is normal ("no
// events yet"); any other read error is returned as the log unavailable.
// The TUI tails with ReadFrom instead of re-reading on every change.
func Read(path string) (evs []Event, skipped int, err error) {
	for _, p := range Files(path) {
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		e, s := parseLines(b)
		evs, skipped = append(evs, e...), skipped+s
	}
	return evs, skipped, nil
}

// ReadWhere is Read's stream (.1, then the live file up to liveEnd bytes) cut to the
// events keep accepts. A raw line is parsed only if it holds one of needles, so
// needles must cover every line keep could accept (a false hit costs a parse, a
// miss loses an event). Lines that don't parse are dropped uncounted.
func ReadWhere(path string, liveEnd int64, needles []string, keep func(Event) bool) ([]Event, error) {
	var evs []Event
	for i, p := range Files(path) {
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if i == 1 && int64(len(b)) > liveEnd {
			b = b[:liveEnd]
		}
		for _, line := range bytes.Split(b, []byte("\n")) {
			if !slices.ContainsFunc(needles, func(n string) bool { return bytes.Contains(line, []byte(n)) }) {
				continue
			}
			if ev, ok := parse(line); ok && keep(ev) {
				evs = append(evs, ev)
			}
		}
	}
	return evs, nil
}

// Beads is the distinct bead ids the events of the log name, up to liveEnd bytes of the live file.
func Beads(path string, liveEnd int64) (map[string]bool, error) {
	ids := map[string]bool{}
	_, err := ReadWhere(path, liveEnd, []string{`"bead":"`}, func(ev Event) bool {
		if ev.Bead != "" {
			ids[ev.Bead] = true
		}
		return false
	})
	return ids, err
}

func parseLines(b []byte) (evs []Event, skipped int) {
	for _, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if ev, ok := parse(line); ok {
			evs = append(evs, ev)
		} else {
			skipped++
		}
	}
	return evs, skipped
}

// State is where an incremental read left off: the Sig it saw and the byte
// offset into the live file just past its last complete line.
type State struct {
	Sig Sig
	Off int64
}

// ReadFrom is Read, incrementally (Spec decision D): it returns only the events
// appended to the live file since prev, and the new State. full reports a
// replace rather than an extend: no prev, .1 changed (rotation, ADR 0028), or
// the live file shrank below prev.Off (truncation). An unchanged Sig reads
// nothing. A partly written trailing line is held back (not counted malformed)
// until its newline lands (FR26).
func ReadFrom(path string, prev State) (evs []Event, skipped int, next State, full bool, err error) {
	sig := Stat(path)
	if sig == prev.Sig && sig != (Sig{}) {
		return nil, 0, prev, false, nil
	}
	files := Files(path)
	full = prev.Sig == (Sig{}) || sig[0] != prev.Sig[0] || sig[1].size < prev.Off
	if !full && prev.Off > 0 && !endsLine(files[1], prev.Off) {
		full = true // truncated and regrown past the offset: it no longer sits on a line end
	}
	var off int64
	if full {
		if b, err := os.ReadFile(files[0]); err == nil {
			evs, skipped = parseLines(b)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, 0, prev, false, err
		}
	} else {
		off = prev.Off
	}
	b, err := readTail(files[1], off)
	if err != nil {
		return nil, 0, prev, false, err
	}
	b = b[:bytes.LastIndexByte(b, '\n')+1]
	e, s := parseLines(b)
	return append(evs, e...), skipped + s, State{sig, off + int64(len(b))}, full, nil
}

// endsLine reports whether the byte before off in path is a newline. Offsets only
// advance past one, so a miss means the file was rewritten under us.
// ponytail: a rewrite that happens to leave a newline there is still missed.
func endsLine(path string, off int64) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var c [1]byte
	_, err = f.ReadAt(c[:], off-1)
	return err == nil && c[0] == '\n'
}

// readTail is path's bytes from off; a missing file is empty.
func readTail(path string, off int64) ([]byte, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.NewSectionReader(f, off, 1<<62))
}

func parse(line []byte) (Event, bool) {
	d := json.NewDecoder(bytes.NewReader(line))
	d.UseNumber() // dur_ms stays 1234567, not 1.234567e+06
	var raw map[string]any
	if err := d.Decode(&raw); err != nil {
		return Event{}, false
	}
	str := func(k string) string { return attrString(raw[k]) }
	ev := Event{Level: str("level"), Msg: str("msg"), Bead: str("bead"), Parent: str("parent"), Pid: str("pid"), Raw: raw}
	ev.Time, _ = time.Parse(time.RFC3339Nano, str("time"))
	// Short error: err's first ": " segment (first line), else result.subtype.
	ev.Short, _, _ = strings.Cut(str("err"), ": ")
	ev.Short, _, _ = strings.Cut(ev.Short, "\n")
	if res, ok := raw["result"].(map[string]any); ok && ev.Short == "" {
		ev.Short = attrString(res["subtype"])
	}
	for _, k := range append([]string{"time", "level", "msg"}, sortedKeys(raw, "time", "level", "msg")...) {
		v, ok := raw[k]
		if !ok {
			continue
		}
		// A group (session.failed's result) reads as a header plus indented sub-keys.
		if g, isGroup := v.(map[string]any); isGroup {
			ev.Attrs = append(ev.Attrs, Attr{k, ""})
			for _, sk := range sortedKeys(g) {
				ev.Attrs = append(ev.Attrs, Attr{"  " + sk, attrString(g[sk])})
			}
			continue
		}
		ev.Attrs = append(ev.Attrs, Attr{k, attrString(v)})
	}
	return ev, true
}

// attrString renders one decoded JSON value.
func attrString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// sortedKeys is m's keys, sorted, less skip.
func sortedKeys(m map[string]any, skip ...string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		if !slices.Contains(skip, k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// scanParsed counts the lines scanFile parsed, for the prefilter test.
var scanParsed atomic.Int64

const scanChunk = 256 << 10

// Scan walks the whole log newest first (the live file, then .1), a chunk at a time.
// Each line must contain every prefilter string (already lowercased) in its
// lowercased form before it is parsed, so the caller's prefilter must be one match
// cannot accept without; match then picks events. batch gets each chunk's matches,
// newest first, as that chunk is read; it returns false to stop the scan. Lines that
// don't parse are skipped, and a partly written last live line is held back.
func Scan(path string, prefilter []string, match func(Event) bool, batch func([]Event) bool) error {
	return ScanTally(path, prefilter, match, batch, Base{}, nil)
}

// Tally counts events a scan has walked: Lines the parsed ones base accepts, Debug
// those among them at DEBUG level.
type Tally struct{ Lines, Debug int }

// Base is what ScanTally tallies: the events Match accepts. Pre (lowercased) is a
// raw-line prefilter Match cannot accept without, so lines missing it are never
// parsed. Raw says Pre alone decides Match, so a line holding Pre counts unparsed.
// A zero Base (nil Match) counts nothing.
type Base struct {
	Pre   []string
	Raw   bool
	Match func(Event) bool
}

// ScanTally is Scan that also counts into t every event base accepts, prefiltered
// out or not; only the lines base needs parsed are (none, when base.Raw). batch may
// read t.
func ScanTally(path string, prefilter []string, match func(Event) bool, batch func([]Event) bool, base Base, t *Tally) error {
	files := Files(path)
	for _, p := range []string{files[1], files[0]} {
		if stop, err := scanFile(p, prefilter, match, batch, base, t); err != nil || stop {
			return err
		}
	}
	return nil
}

func scanFile(path string, prefilter []string, match func(Event) bool, batch func([]Event) bool, base Base, t *Tally) (stop bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	pos, err := lineEnd(f, fi.Size())
	if err != nil {
		return false, err
	}
	var carry []byte // the front of the line the previous (newer) chunk began inside
	for pos > 0 {
		n := min(scanChunk, pos)
		pos -= n
		buf := make([]byte, int(n)+len(carry))
		if _, err := f.ReadAt(buf[:n], pos); err != nil {
			return false, err
		}
		copy(buf[n:], carry)
		carry = nil
		if pos > 0 { // the chunk's first line is a fragment of the older chunk's last
			i := bytes.IndexByte(buf, '\n')
			if i < 0 {
				carry = buf
				continue
			}
			carry, buf = buf[:i:i], buf[i+1:]
		}
		lines := bytes.Split(buf, []byte("\n"))
		var out []Event
		for i := len(lines) - 1; i >= 0; i-- {
			line := lines[i]
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			pass := hasAll(line, prefilter)
			if t != nil && base.Match != nil && base.Raw && hasAll(line, base.Pre) {
				t.Lines++
				if bytes.Contains(line, []byte(`"level":"DEBUG"`)) {
					t.Debug++
				}
			}
			if !pass && (base.Match == nil || base.Raw || !hasAll(line, base.Pre)) {
				continue
			}
			scanParsed.Add(1)
			ev, ok := parse(line)
			if !ok {
				continue
			}
			if t != nil && base.Match != nil && !base.Raw && base.Match(ev) {
				t.Lines++
				if ev.Level == "DEBUG" {
					t.Debug++
				}
			}
			if pass && match(ev) {
				out = append(out, ev)
			}
		}
		if len(out) > 0 && !batch(out) {
			return true, nil
		}
	}
	return false, nil
}

// hasAll reports whether line, lowercased, holds every string in subs.
func hasAll(line []byte, subs []string) bool {
	if len(subs) == 0 {
		return true
	}
	low := bytes.ToLower(line)
	for _, s := range subs {
		if !bytes.Contains(low, []byte(s)) {
			return false
		}
	}
	return true
}

// lineEnd is the offset just past the last newline in the first size bytes of f
// (0 when there is none): a partly written last line is not read.
func lineEnd(f *os.File, size int64) (int64, error) {
	for end := size; end > 0; {
		n := min(scanChunk, end)
		end -= n
		buf := make([]byte, n)
		if _, err := f.ReadAt(buf, end); err != nil {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			return end + int64(i) + 1, nil
		}
	}
	return 0, nil
}

// Cursor is where the oldest loaded event starts: File 0 = .1, 1 = live. Done
// means nothing older exists (start of .1, or .1 missing).
type Cursor struct {
	File int
	Off  int64
	Done bool
}

// ReadNewest is the newest n events, oldest first, read backward in chunks from the
// end of the live file and on into .1 (ADR 0231 §1). It returns the head State that
// ReadFrom extends (a partly written last live line is held back, FR26) and the
// Cursor ReadOlder continues from. skipped counts unparsed lines it read past.
func ReadNewest(path string, n int) (evs []Event, skipped int, st State, cur Cursor, err error) {
	st.Sig = Stat(path)
	files := Files(path)
	evs, skipped, st.Off, cur, err = readBack(files[1], 1, -1, n)
	if err != nil {
		return nil, 0, State{}, Cursor{}, err
	}
	if len(evs) < n || cur.Off == 0 {
		older, sk, _, c, err := readBack(files[0], 0, -1, n-len(evs))
		if err != nil {
			return nil, 0, State{}, Cursor{}, err
		}
		evs, skipped, cur = append(older, evs...), skipped+sk, c
	}
	return evs, skipped, st, cur, nil
}

// ReadOlder is the next n events older than cur, oldest first, with the cursor past
// them and the Sig that cursor belongs to. sig is the Sig cur was taken at: if .1
// changed since (a rotation, ADR 0028), a live-file cursor is the same offset in
// .1 now, and a .1 cursor is Done (that file is gone).
// ponytail: two rotations between page reads remap a live cursor into a different .1
// and re-read rows; one Sig per page cannot tell, add a file generation to Sig if it bites.
func ReadOlder(path string, cur Cursor, sig Sig, n int) (evs []Event, skipped int, next Cursor, now Sig, err error) {
	now = Stat(path)
	if cur.Done {
		return nil, 0, cur, now, nil
	}
	if now[0] != sig[0] {
		if cur.File == 0 {
			return nil, 0, Cursor{Done: true}, now, nil
		}
		cur.File = 0
	}
	files := Files(path)
	next = cur
	if cur.File == 1 && cur.Off > 0 {
		evs, skipped, _, next, err = readBack(files[1], 1, cur.Off, n)
		if err != nil || (len(evs) == n && next.Off > 0) {
			return evs, skipped, next, now, err
		}
	}
	if cur.File == 1 || cur.Off > 0 { // into .1: from its end, or from where the cursor sits in it
		end := int64(-1)
		if cur.File == 0 {
			end = cur.Off
		}
		older, sk, _, c, err := readBack(files[0], 0, end, n-len(evs))
		return append(older, evs...), skipped + sk, c, now, err
	}
	return evs, skipped, Cursor{Done: true}, now, nil
}

// readBack reads up to n events backward from end in path (a line boundary; -1 = the
// last complete line), oldest first. head is end resolved; cur is where the oldest begins. A missing file reads as nothing older.
func readBack(path string, file int, end int64, n int) (evs []Event, skipped int, head int64, cur Cursor, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, 0, Cursor{Done: true}, nil
	}
	if err != nil {
		return nil, 0, 0, Cursor{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, 0, Cursor{}, err
	}
	if end < 0 {
		if end, err = lineEnd(f, fi.Size()); err != nil {
			return nil, 0, 0, Cursor{}, err
		}
	}
	end = min(end, fi.Size()) // ponytail: a truncated file leaves a stale cursor mid-line; rare, a rescan fixes it
	head = end
	start, pos := end, end
	var carry []byte // the front of the line the newer chunk began inside
	for pos > 0 && len(evs) < n {
		sz := min(scanChunk, pos)
		pos -= sz
		buf := make([]byte, int(sz)+len(carry))
		if _, err := f.ReadAt(buf[:sz], pos); err != nil {
			return nil, 0, 0, Cursor{}, err
		}
		copy(buf[sz:], carry)
		carry = nil
		base := pos
		if pos > 0 { // the chunk's first line is a fragment of the older chunk's last
			i := bytes.IndexByte(buf, '\n')
			if i < 0 {
				carry = buf
				continue
			}
			carry, buf, base = buf[:i:i], buf[i+1:], pos+int64(i)+1
		}
		start = base
		lines := bytes.Split(buf, []byte("\n"))
		offs := make([]int64, len(lines))
		for i := 1; i < len(lines); i++ {
			offs[i] = offs[i-1] + int64(len(lines[i-1])) + 1
		}
		for i := len(lines) - 1; i >= 0 && len(evs) < n; i-- {
			if len(bytes.TrimSpace(lines[i])) == 0 {
				continue
			}
			if ev, ok := parse(lines[i]); ok {
				evs = append(evs, ev)
			} else {
				skipped++
			}
			start = base + offs[i]
		}
	}
	slices.Reverse(evs)
	return evs, skipped, head, Cursor{File: file, Off: start, Done: file == 0 && start == 0}, nil
}

// ReadRotated is ReadFrom across a rotation: the events appended since prev, which
// now sit in .1 past prev.Off, then the new live file whole. ok is false when .1 does
// not continue prev (no rotation, or the file was replaced): the caller re-reads.
func ReadRotated(path string, prev State) (evs []Event, skipped int, next State, ok bool, err error) {
	sig := Stat(path)
	files := Files(path)
	if prev.Sig == (Sig{}) || sig[0] == prev.Sig[0] || sig[0].size < prev.Off || (prev.Off > 0 && !endsLine(files[0], prev.Off)) {
		return nil, 0, prev, false, nil
	}
	old, err := readTail(files[0], prev.Off)
	if err != nil {
		return nil, 0, prev, false, err
	}
	b, err := readTail(files[1], 0)
	if err != nil {
		return nil, 0, prev, false, err
	}
	b = b[:bytes.LastIndexByte(b, '\n')+1]
	evs, skipped = parseLines(old)
	e, s := parseLines(b)
	return append(evs, e...), skipped + s, State{sig, int64(len(b))}, true, nil
}
