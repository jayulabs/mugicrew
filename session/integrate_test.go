package session

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// stubIntegrate scripts git.Integrate's result and records the unpauses.
func stubIntegrate(t *testing.T, sha string, err error) (args *[]string, unpaused *[]string) {
	t.Helper()
	origGit, origUnpause := integrateGit, unpause
	t.Cleanup(func() { integrateGit, unpause = origGit, origUnpause })
	args, unpaused = new([]string), new([]string)
	integrateGit = func(container, id, trailer string) (string, int, error) {
		*args = []string{container, id, trailer}
		return sha, 0, err
	}
	unpause = func(id, cause string) error { *unpaused = append(*unpaused, id+" "+cause); return nil }
	return args, unpaused
}

var pausedBuild = bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp", Labels: []string{"paused:" + bd.PausedIntegrateConflict}}

// The verb's exit code says what happened (FR10): 0 integrated or nothing ahead,
// 2 a conflict (rebase, held, landed), 1 transient (spent retries included). Only
// a 0 lifts paused:integrate-conflict (FR37).
func TestIntegrateExitCodesAndPauseLift(t *testing.T) {
	cases := []struct {
		name   string
		sha    string
		err    error
		code   int
		lifted bool
	}{
		{"integrated", "sq1", nil, 0, true},
		{"nothing ahead", "", nil, 0, true},
		{"rebase conflict", "", &git.IntegrateConflict{Reason: "rebase", Container: "checkpoint/ws-cp"}, 2, false},
		{"held", "", &git.IntegrateConflict{Reason: "held", Container: "checkpoint/ws-cp", Holder: "/h"}, 2, false},
		{"landed", "", &git.IntegrateConflict{Reason: "landed", Container: "checkpoint/ws-cp"}, 2, false},
		{"retries spent", "", git.ErrContended, 1, false},
		{"transient", "", errors.New("boom"), 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args, unpaused := stubIntegrate(t, c.sha, c.err)
			var out strings.Builder
			if code := Integrate(pausedBuild, "agent", &out); code != c.code {
				t.Errorf("exit = %d, want %d (output %q)", code, c.code, out.String())
			}
			if want := []string{"checkpoint/ws-cp", "mc-b", "Build: mc-b"}; strings.Join(*args, "|") != strings.Join(want, "|") {
				t.Errorf("git.Integrate args = %q, want %q", *args, want)
			}
			if lifted := len(*unpaused) == 1 && (*unpaused)[0] == "mc-b "+bd.PausedIntegrateConflict; lifted != c.lifted {
				t.Errorf("unpaused = %q, want lifted %v", *unpaused, c.lifted)
			}
		})
	}
}

// A rebase conflict tells the agent both ways out (FR11).
func TestIntegrateRebaseConflictSaysResolveOrAbort(t *testing.T) {
	stubIntegrate(t, "", &git.IntegrateConflict{Reason: "rebase", Container: "checkpoint/ws-cp"})
	var out strings.Builder
	Integrate(pausedBuild, "agent", &out)
	for _, want := range []string{"git rebase --continue", "mugicrew integrate", "git rebase --abort", "leave it open"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("conflict output %q lacks %q", out.String(), want)
		}
	}
}

// The verb never closes the bead; by hand it says so (FR38). With nothing ahead it
// says so only when it lifted paused:integrate-conflict, the bead's last hold.
func TestIntegrateByHumanPrintsTheCloseHint(t *testing.T) {
	stubIntegrate(t, "sq1", nil)
	var out strings.Builder
	Integrate(pausedBuild, "human", &out)
	if !strings.Contains(out.String(), "integrated; bead still open: bd close mc-b") {
		t.Errorf("human output = %q, want the close hint", out.String())
	}
	stubIntegrate(t, "", nil)
	out.Reset()
	Integrate(pausedBuild, "human", &out)
	if got := out.String(); !strings.Contains(got, "nothing to integrate") || !strings.Contains(got, "bd close mc-b") {
		t.Errorf("human output, nothing ahead, pause lifted = %q, want nothing-to-integrate and the close hint", got)
	}
	unpausedBuild := pausedBuild
	unpausedBuild.Labels = nil
	out.Reset()
	Integrate(unpausedBuild, "human", &out)
	if got := out.String(); strings.Contains(got, "bd close") || !strings.Contains(got, "nothing to integrate") {
		t.Errorf("human output, nothing ahead = %q, want only the nothing-to-integrate line", got)
	}
}

// A history-rewriting leaf (conflict, chart-conflict) integrates in replace mode:
// a replace-CAS expecting the pinned Tip its body carries, onto its parent container
// (Spec D3, FR 13).
func TestIntegrateReplaceModeExpectsThePinnedTip(t *testing.T) {
	for _, c := range []struct {
		bead bd.Bead
		want []string
	}{
		{bd.Bead{ID: "mc-c", Kind: contract.KindConflict, Parent: "ws-cp", Description: "Base: b\nTip: t1p"}, []string{"checkpoint/ws-cp", "t1p"}},
		{bd.Bead{ID: "ws-cc", Kind: contract.KindChartConflict, Parent: "ws-ch", Description: "Base: b\nTip: t2p"}, []string{"chart/ws-ch", "t2p"}},
	} {
		squash, _ := stubIntegrate(t, "sq1", nil)
		replace := stubReplace(t, "rw1", nil)
		var out strings.Builder
		if code := Integrate(c.bead, "agent", &out); code != 0 || *squash != nil {
			t.Errorf("%s: exit %d, squash args %q; want 0 and no squash", c.bead.Kind, code, *squash)
		}
		if strings.Join(*replace, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: git.IntegrateReplace args = %q, want %q", c.bead.Kind, *replace, c.want)
		}
	}
}

// stubReplace scripts git.IntegrateReplace's result and records its args.
func stubReplace(t *testing.T, sha string, err error) (args *[]string) {
	t.Helper()
	orig := integrateReplace
	t.Cleanup(func() { integrateReplace = orig })
	args = new([]string)
	integrateReplace = func(container, pinned string) (string, int, error) {
		*args = []string{container, pinned}
		return sha, 0, err
	}
	return args
}

// A replace leaf whose body carries no Tip: pin, and a kind with no container, are
// refused loudly before any git.
func TestIntegrateRefusesUnpinnedReplaceAndContainerless(t *testing.T) {
	for _, b := range []bd.Bead{
		{ID: "mc-c", Kind: contract.KindConflict, Parent: "ws-cp", Description: "Base: b"},
		{ID: "mc-s", Kind: contract.KindSignal},
	} {
		args, _ := stubIntegrate(t, "sq1", nil)
		replace := stubReplace(t, "rw1", nil)
		var out strings.Builder
		if code := Integrate(b, "agent", &out); code != 1 || *args != nil || *replace != nil || out.Len() == 0 {
			t.Errorf("%s: exit %d, git args %q %q, output %q; want a loud 1 with no git", b.Kind, code, *args, *replace, out.String())
		}
	}
}
