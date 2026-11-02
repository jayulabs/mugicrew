package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/release"
)

// stubRelease stubs the release seams: Classify gives in, RunCaptured records argv
// and answers by its first word.
func stubRelease(t *testing.T, in release.Install, npmCode int, doctorOut string) *[][]string {
	t.Helper()
	oc, or := release.Classify, release.RunCaptured
	t.Cleanup(func() { release.Classify, release.RunCaptured = oc, or })
	release.Classify = func() release.Install { return in }
	var calls [][]string
	release.RunCaptured = func(argv []string) (string, int) {
		calls = append(calls, argv)
		if argv[0] == "npm" {
			return "", npmCode
		}
		return doctorOut, 0
	}
	upgradePoll = 0
	return &calls
}

var npmGlobal = release.Install{Mode: release.NpmGlobal, Prefix: "/p", Exe: "/p/bin/x"}

// drive runs cmd and every command its messages return, until none is left.
func drive(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				m = drive(t, m, c)
			}
			return m
		}
		m, cmd = send(t, m, msg)
	}
	return m
}

func upgradeModel(t *testing.T, st looplock.Status) Model {
	t.Helper()
	m := sized(t, 100, 30)
	m.loop, m.releaseLatest = st, "2.0.0"
	return m
}

func TestUpgradeOptionNeedsANotice(t *testing.T) {
	m := sized(t, 100, 30)
	m.loop = loopOff
	if slices.ContainsFunc(m.loopOpts(), func(o loopOpt) bool { return o.key == "u" }) {
		t.Error("u offered with no notice held")
	}
	m.releaseLatest = "2.0.0"
	if !slices.Contains(m.loopOpts(), loopOpt{"u", "upgrade"}) {
		t.Errorf("no u with a notice: %v", m.loopOpts())
	}
}

func TestUpgradeDialogText(t *testing.T) {
	for _, c := range []struct {
		st   looplock.Status
		stop bool
	}{{loopOff, false}, {loopFromSource, true}} {
		m := upgradeModel(t, c.st)
		m, _ = send(t, m, runeKey("o"))
		m, _ = send(t, m, runeKey("u"))
		v := ansi.Strip(m.View())
		if !strings.Contains(v, "Upgrade mugicrew to v2.0.0?") || strings.Contains(v, "stopped first") != c.stop {
			t.Errorf("%s: dialog %q", c.st.State, v)
		}
	}
}

func TestUpgradeConfirmStopsLoopWaitsThenUpgrades(t *testing.T) {
	calls := stubRelease(t, npmGlobal, 0, "all 5 checks ok\n")
	verbs := fakeLoopVerb(t)
	reads := 0
	orig := readLoop
	t.Cleanup(func() { readLoop = orig })
	readLoop = func() (looplock.Status, error) {
		reads++
		if reads < 3 { // the stop is still draining on the first reads
			return loopFromSource, nil
		}
		return loopOff, nil
	}
	m := upgradeModel(t, loopFromSource)
	m, _ = send(t, m, runeKey("o"))
	m, _ = send(t, m, runeKey("u"))
	m, cmd := send(t, m, runeKey("y"))
	m = drive(t, m, cmd)
	if len(*verbs) != 1 || !slices.Equal((*verbs)[0], []string{"stop"}) {
		t.Errorf("loop verbs %v, want one plain stop", *verbs)
	}
	if reads < 3 {
		t.Errorf("upgraded after %d reads, before the lock cleared", reads)
	}
	want := [][]string{{"npm", "i", "-g", "--prefix", "/p", "@jayulabs/mugicrew@2.0.0"}, {"/p/bin/x", "doctor"}}
	if len(*calls) != 2 || !slices.Equal((*calls)[0], want[0]) || !slices.Equal((*calls)[1], want[1]) {
		t.Errorf("ran %v, want %v", *calls, want)
	}
	if !strings.Contains(m.status, "upgraded to v2.0.0: quit and restart mugicrew") || !strings.Contains(m.status, "doctor: clean") {
		t.Errorf("status %q", m.status)
	}
}

func TestUpgradeDoesNotForceADrainingLoop(t *testing.T) {
	stubRelease(t, npmGlobal, 0, "")
	verbs := fakeLoopVerb(t)
	reads := 0
	orig := readLoop
	t.Cleanup(func() { readLoop = orig })
	readLoop = func() (looplock.Status, error) {
		if reads++; reads < 2 {
			return loopDraining, nil
		}
		return loopOff, nil
	}
	m := upgradeModel(t, loopDraining)
	m, _ = send(t, m, runeKey("o"))
	m, _ = send(t, m, runeKey("u"))
	m, cmd := send(t, m, runeKey("y"))
	drive(t, m, cmd)
	if len(*verbs) != 0 {
		t.Errorf("loop verbs %v, want none for a stop drain", *verbs)
	}
}

func TestUpgradeLoopOffRunsNoLoopVerb(t *testing.T) {
	stubRelease(t, npmGlobal, 0, "1. warn x\n\nTo fix\n\n1. warn x\n2. FAIL y\n")
	verbs := fakeLoopVerb(t)
	m := upgradeModel(t, loopOff)
	m, _ = send(t, m, runeKey("o"))
	m, _ = send(t, m, runeKey("u"))
	m, cmd := send(t, m, runeKey("y"))
	m = drive(t, m, cmd)
	if len(*verbs) != 0 {
		t.Errorf("loop verbs %v", *verbs)
	}
	if !strings.Contains(m.status, "2 to fix") {
		t.Errorf("status %q lacks the doctor summary", m.status)
	}
}

func TestUpgradePrintPlanTouchesNothing(t *testing.T) {
	calls := stubRelease(t, release.Install{Mode: release.ProjectLocal, Root: "/proj", PM: "npm"}, 0, "")
	verbs := fakeLoopVerb(t)
	m := upgradeModel(t, loopFromSource)
	m, _ = send(t, m, runeKey("o"))
	m, _ = send(t, m, runeKey("u"))
	m, cmd := send(t, m, runeKey("y"))
	m = drive(t, m, cmd)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "npm i @jayulabs/mugicrew@2.0.0") || !strings.Contains(v, "project-local") {
		t.Errorf("print plan not shown: %q", v)
	}
	if len(*verbs) != 0 || len(*calls) != 0 {
		t.Errorf("print plan ran loop %v, release %v", *verbs, *calls)
	}
}

func TestUpgradeNpmFailureReportsExitStatus(t *testing.T) {
	stubRelease(t, npmGlobal, 243, "")
	fakeLoopVerb(t)
	m := upgradeModel(t, loopOff)
	m, _ = send(t, m, runeKey("o"))
	m, _ = send(t, m, runeKey("u"))
	m, cmd := send(t, m, runeKey("y"))
	m = drive(t, m, cmd)
	if !strings.Contains(m.status, "npm exited 243") {
		t.Errorf("status %q", m.status)
	}
}

func TestUpgradeStopFailureLeavesNpmUnrun(t *testing.T) {
	calls := stubRelease(t, npmGlobal, 0, "")
	fakeLoopVerbOut(t, "", errors.New("boom"))
	orig := readLoop
	t.Cleanup(func() { readLoop = orig })
	readLoop = func() (looplock.Status, error) { return loopFromSource, nil }
	m := upgradeModel(t, loopFromSource)
	m, _ = send(t, m, runeKey("o"))
	m, _ = send(t, m, runeKey("u"))
	m, cmd := send(t, m, runeKey("y"))
	m = drive(t, m, cmd)
	if len(*calls) != 0 || !strings.Contains(m.status, "boom") {
		t.Errorf("ran %v, status %q", *calls, m.status)
	}
}

func TestReleaseSegmentClickOpensUpgrade(t *testing.T) {
	m := headerFixture(t, 120)
	m.releaseLatest = "2.0.0"
	for _, sp := range m.headerSpans() {
		if sp.kind != headerUpgrade {
			continue
		}
		m = headerPress(t, m, sp.x0, 0, tea.MouseButtonLeft)
		if len(m.modals) != 1 || m.modals[0].title != "Upgrade mugicrew?" {
			t.Errorf("click opened %+v", m.modals)
		}
		return
	}
	t.Fatal("no upgrade span on the header")
}
