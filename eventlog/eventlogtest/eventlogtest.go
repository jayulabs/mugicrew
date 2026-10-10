// Package eventlogtest captures the events code under test emits through slog's
// default logger — the one event seam (Spec decision A).
package eventlogtest

import (
	"context"
	"log/slog"
	"testing"
)

// Event is one captured record. Int attrs read back as int, the rest as given.
type Event struct {
	Level slog.Level
	Name  string
	Attrs map[string]any
}

// Discard silences slog's default logger, for a TestMain: tests that don't capture
// must not append to the real repo's event log.
func Discard() { slog.SetDefault(slog.New(slog.DiscardHandler)) }

// Capture swaps slog's default logger for a recorder (every level) until the test
// ends.
func Capture(t testing.TB) *[]Event {
	evs := &[]Event{}
	orig := slog.Default()
	slog.SetDefault(slog.New(recorder{evs: evs}))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return evs
}

// Names are evs' event names in order.
func Names(evs []Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Name)
	}
	return out
}

// recorder keeps every record; attrs from .With ride ahead of the record's own.
type recorder struct {
	evs   *[]Event
	attrs []slog.Attr
}

func (recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r recorder) Handle(_ context.Context, rec slog.Record) error {
	m := map[string]any{}
	add := func(a slog.Attr) bool {
		v := a.Value.Any()
		if i, ok := v.(int64); ok {
			v = int(i)
		}
		m[a.Key] = v
		return true
	}
	for _, a := range r.attrs {
		add(a)
	}
	rec.Attrs(add)
	*r.evs = append(*r.evs, Event{rec.Level, rec.Message, m})
	return nil
}

func (r recorder) WithAttrs(as []slog.Attr) slog.Handler {
	return recorder{r.evs, append(r.attrs[:len(r.attrs):len(r.attrs)], as...)}
}

// ponytail: no call site groups; a group's attrs land flat, unprefixed.
func (r recorder) WithGroup(string) slog.Handler { return r }
