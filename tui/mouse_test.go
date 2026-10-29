package tui

import (
	"errors"
	"log/slog"
	"testing"

	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

func TestTmuxMouseFailureIsACatalogueEvent(t *testing.T) {
	orig := setTUIMouse
	t.Cleanup(func() { setTUIMouse = orig })
	setTUIMouse = func(bool) error { return errors.New("tmux gone") }
	evs := eventlogtest.Capture(t)
	tmuxMouse(true)()
	if len(*evs) != 1 {
		t.Fatalf("events = %v", *evs)
	}
	ev := (*evs)[0]
	if ev.Name != "tmux.mouse.failed" || ev.Level != slog.LevelWarn || ev.Attrs["on"] != true || ev.Attrs["err"] != "tmux gone" {
		t.Errorf("event = %+v", ev)
	}
}
