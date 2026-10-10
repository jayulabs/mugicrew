package bd

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// Spec D6: a strip emits exactly one title.stripped event with from/to
// on the default logger; an unprefixed title emits nothing.
func TestStripTitleEvent(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if got := stripTitle("Critique: Foo", "id", "mc-1"); got != "Foo" {
		t.Errorf("got %q", got)
	}
	if got := stripTitle("Foo bar", "id", "mc-2"); got != "Foo bar" {
		t.Errorf("got %q", got)
	}
	out := buf.String()
	if strings.Count(out, "title.stripped") != 1 || !strings.Contains(out, `from="Critique: Foo"`) ||
		!strings.Contains(out, "to=Foo") || !strings.Contains(out, "id=mc-1") || strings.Contains(out, "mc-2") {
		t.Errorf("log = %q", out)
	}
}
