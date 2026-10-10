package eventlog

// The reader side (Spec decision G): the Logs tab reads the file raw
// through here. Nothing recovers from it (ADR 0033): it is history only.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
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
// ponytail: re-reads and re-decodes both files (up to 2×MaxSize) on every stat
// change, i.e. most ticks while efforts run. Read incrementally from the last
// offset (full re-read only when .1's stat moves) if that ever lags.
func Read(path string) (evs []Event, skipped int, err error) {
	for _, p := range Files(path) {
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, 0, err
		}
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
	}
	return evs, skipped, nil
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
