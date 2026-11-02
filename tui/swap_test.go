package tui

import (
	"testing"

	"jayulabs.org/mugicrew/selfexe"
)

func swapModel(t *testing.T, changed *bool, probe string) Model {
	t.Helper()
	oldS, oldP := swapped, swapProbe
	t.Cleanup(func() { swapped, swapProbe = oldS, oldP })
	swapped = func(string, selfexe.Identity) bool { return *changed }
	swapProbe = func(string) string { return probe }
	m := sized(t, 100, 30)
	m.swapPath, m.swapWatch, m.swapFrom = "/bin/mugicrew", true, "1.0.0"
	return m
}

// refresh is one tick's swap check: the probe cmd it returns, run and folded in.
func refresh(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.checkSwap()
	if cmd == nil {
		return m
	}
	m, _ = send(t, m, cmd())
	return m
}

func TestSwapLineShowsOnceWithProbedVersion(t *testing.T) {
	changed := true
	m := refresh(t, swapModel(t, &changed, "1.1.0"))
	want := "loop upgraded v1.0.0→v1.1.0; restart mugicrew to run it here"
	if m.status != want {
		t.Fatalf("status = %q, want %q", m.status, want)
	}
	m.status = ""
	if m = refresh(t, m); m.status != "" {
		t.Errorf("line repeated: %q", m.status)
	}
}

func TestSwapUnchangedShowsNothing(t *testing.T) {
	changed := false
	if m := refresh(t, swapModel(t, &changed, "1.1.0")); m.status != "" {
		t.Errorf("status = %q", m.status)
	}
}

// A half-extracted binary answers no release: the line waits for a later tick's
// probe that does, then shows once.
func TestSwapLineWaitsForAStrictRelease(t *testing.T) {
	changed := true
	m := swapModel(t, &changed, "")
	m = refresh(t, m)
	if m.status != "" || !m.swapWatch {
		t.Fatalf("status %q, watch %v after a failed probe", m.status, m.swapWatch)
	}
	swapProbe = func(string) string { return "1.1.0" }
	m = refresh(t, m)
	if want := "loop upgraded v1.0.0→v1.1.0; restart mugicrew to run it here"; m.status != want {
		t.Fatalf("status = %q, want %q", m.status, want)
	}
}
