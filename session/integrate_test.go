package session

import (
	"errors"
	"fmt"
	"io"
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
	integrateGit = func(_ git.Repo, container, id, subject, trailer string) (string, int, error) {
		*args = []string{container, id, subject, trailer}
		return sha, 0, err
	}
	unpause = func(id, cause string) error { *unpaused = append(*unpaused, id+" "+cause); return nil }
	return args, unpaused
}

var pausedBuild = bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp", Title: "Add it", Labels: []string{"context:app", "paused:" + bd.PausedIntegrateConflict}}

// The verb's exit code says what happened (FR10): 0 integrated or nothing ahead,
// 2 a conflict (rebase, held), 3 landed, 1 transient (spent retries included). Only
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
		{"landed", "", &git.IntegrateConflict{Reason: "landed", Container: "checkpoint/ws-cp"}, 3, false},
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
			if want := []string{"checkpoint/ws-cp", "mc-b", "feat(app): Add it [mc-b]", "Build: mc-b"}; strings.Join(*args, "|") != strings.Join(want, "|") {
				t.Errorf("git.Integrate args = %q, want %q", *args, want)
			}
			if lifted := len(*unpaused) == 1 && (*unpaused)[0] == "mc-b "+bd.PausedIntegrateConflict; lifted != c.lifted {
				t.Errorf("unpaused = %q, want lifted %v", *unpaused, c.lifted)
			}
		})
	}
}

// The subject's scope is the bead's own context, else its container's, else none;
// a bead with its own context triggers no container lookup, and a failed lookup
// still integrates, unscoped.
func TestIntegrateSubjectScopeFallsBackToTheContainersContext(t *testing.T) {
	cases := []struct {
		name      string
		labels    []string
		container bd.Bead
		showErr   error
		want      string
		looked    bool
	}{
		{"own context wins", []string{"context:own"}, bd.Bead{Labels: []string{"context:box"}}, nil, "feat(own): Add it [mc-b]", false},
		{"container context", nil, bd.Bead{Labels: []string{"context:box"}}, nil, "feat(box): Add it [mc-b]", true},
		{"neither", nil, bd.Bead{}, nil, "feat: Add it [mc-b]", true},
		{"lookup fails", nil, bd.Bead{}, errors.New("bd down"), "feat: Add it [mc-b]", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args, _ := stubIntegrate(t, "sq1", nil)
			origShow := showBead
			t.Cleanup(func() { showBead = origShow })
			looked := ""
			showBead = func(id string) (bd.Bead, error) { looked = id; return c.container, c.showErr }
			b := bd.Bead{ID: "mc-b", Kind: contract.KindBuild, Parent: "ws-cp", Title: "Add it", Labels: c.labels}
			if code := Integrate(b, "agent", io.Discard); code != 0 {
				t.Fatalf("exit = %d, want 0", code)
			}
			if got := (*args)[2]; got != c.want {
				t.Errorf("subject = %q, want %q", got, c.want)
			}
			if (looked != "") != c.looked || (c.looked && looked != "ws-cp") {
				t.Errorf("container lookup = %q, want looked %v of ws-cp", looked, c.looked)
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
		{bd.Bead{ID: "ws-cr", Kind: contract.KindCritique, Parent: "ws-cp", Description: "Base: b\nTip: t3p"}, []string{"checkpoint/ws-cp", "t3p"}},
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
	integrateReplace = func(_ git.Repo, container, pinned string) (string, int, error) {
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

// stubGitlabFetch sets land_strategy and scripts the Forge main fetch, recording call order.
func stubGitlabFetch(t *testing.T, strategy string, fetchErr error) (calls *[]string) {
	t.Helper()
	origName, origFetch, origReplace := git.LandStrategyName, fetchMainline, integrateReplace
	t.Cleanup(func() { git.LandStrategyName, fetchMainline, integrateReplace = origName, origFetch, origReplace })
	calls = new([]string)
	git.LandStrategyName = func() (string, error) { return strategy, nil }
	fetchMainline = func(git.Repo) (string, error) { *calls = append(*calls, "fetch"); return "", fetchErr }
	integrateReplace = func(git.Repo, string, string) (string, int, error) {
		*calls = append(*calls, "replace")
		return "", 0, &git.IntegrateConflict{Reason: git.ConflictLanded, Container: "checkpoint/ws-cp"}
	}
	return calls
}

var replaceBead = bd.Bead{ID: "mc-c", Kind: contract.KindConflict, Parent: "ws-cp", Description: "Base: b\nTip: t1p"}

// Under gitlab a replace integrate fetches Forge main before its landed check (exit 3
// once the fetched main carries the trailer); a failed fetch publishes nothing
// (exit 1); other strategies never fetch.
func TestIntegrateReplaceFetchesForgeMainUnderGitlab(t *testing.T) {
	var out strings.Builder
	calls := stubGitlabFetch(t, git.LandStrategyGitlab, nil)
	if code := Integrate(replaceBead, "agent", &out); code != ExitLanded || strings.Join(*calls, ",") != "fetch,replace" {
		t.Errorf("gitlab: exit %d, calls %q; want 3 after fetch,replace", code, *calls)
	}
	calls = stubGitlabFetch(t, git.LandStrategyGitlab, errors.New("forge down"))
	if code := Integrate(replaceBead, "agent", &out); code != ExitTransient || strings.Join(*calls, ",") != "fetch" {
		t.Errorf("failed fetch: exit %d, calls %q; want 1 and no replace", code, *calls)
	}
	calls = stubGitlabFetch(t, git.LandStrategyLocal, nil)
	Integrate(replaceBead, "agent", &out)
	if strings.Join(*calls, ",") != "replace" {
		t.Errorf("local: calls %q, want no fetch", *calls)
	}
}

// A replace refused off its pinned tip is its own exit (5), not a transient 1.
func TestIntegrateReplaceOffPinIsItsOwnExit(t *testing.T) {
	stubReplace(t, "", fmt.Errorf("%w: checkpoint/ws-cp was rewritten", git.ErrOffPin))
	if code := Integrate(replaceBead, "agent", io.Discard); code != ExitOffPin {
		t.Errorf("exit %d, want %d", code, ExitOffPin)
	}
}

// ADR 0328: the exit block every integrate-running prompt embeds names each Exit*
// constant's number beside its instruction.
func TestIntegrateExitsBlockNamesEachExitCode(t *testing.T) {
	got := contract.IntegrateExits()
	for _, want := range []string{
		fmt.Sprintf("Exit %d: integrated, or nothing ahead.", ExitIntegrated),
		fmt.Sprintf("Exit %d with a rebase stopped in your tree: resolve", ExitConflict),
		fmt.Sprintf("Exit %d with no rebase stopped: the container is held.", ExitConflict),
		fmt.Sprintf("Exit %d: the container already landed.", ExitLanded),
		fmt.Sprintf("Exit %d or %d: the container was rewritten past your cut or is off its pin. Never repair or rebase branches by hand.", ExitNoCutPoint, ExitOffPin),
		fmt.Sprintf("Any exit but %d or a resolved %d (%d is transient): note the output on your bead, leave it open and stop.", ExitIntegrated, ExitConflict, ExitTransient),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("IntegrateExits missing %q:\n%s", want, got)
		}
	}
}
