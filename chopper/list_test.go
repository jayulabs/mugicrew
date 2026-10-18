package chopper

import (
	"bytes"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/looplock"
	"jayulabs.org/mugicrew/session"
)

// `mugicrew recover` with no bead is a pure view (ADR 0033): one row per
// candidate (in_progress, paused abandoned or recovering, an effort tree with no
// live holder, a mid-rebase tree) with its verdict, leftovers and way out, then
// the detached temp trees no bead links to under their own heading. It logs
// nothing and writes nothing.
func TestListShowsStrandedBeads(t *testing.T) {
	evs := eventlogtest.Capture(t)
	beads := map[string]bd.Bead{
		"mc-dead":  {ID: "mc-dead", Kind: contract.KindBuild, Parent: "mc-cp", Status: "in_progress", Labels: []string{"paused:abandoned"}},
		"mc-live":  {ID: "mc-live", Kind: contract.KindBuild, Parent: "mc-cp", Status: "in_progress"},
		"mc-amb":   {ID: "mc-amb", Kind: contract.KindSpec, Status: "open", Labels: []string{"paused:abandoned"}},
		"mc-hitl":  {ID: "mc-hitl", Kind: contract.KindSurgery, Parent: "mc-cp", Status: "in_progress"},
		"mc-reb":   {ID: "mc-reb", Kind: contract.KindBuild, Parent: "mc-cp", Status: "open", Labels: []string{"paused:recovering", "paused:failing"}},
		"mc-busy":  {ID: "mc-busy", Kind: contract.KindBuild, Parent: "mc-cp", Status: "open"},
		"mc-nest":  {ID: "mc-nest", Kind: contract.KindBuild, Parent: "mc-cp", Status: "in_progress"},
		"mc-human": {ID: "mc-human", Status: "in_progress"}, // no kind: not mugicrew's to recover
	}
	trees := map[string]string{ // branch → tree
		"effort/mc-dead": "/tmp/mugicrew-wt-1/wt",
		"effort/mc-busy": "/tmp/mugicrew-wt-3/wt",
		"effort/mc-nest": "/tmp/mugicrew-wt-4/wt",
	}
	var writes []string
	write := func(name string) { writes = append(writes, name) }
	set(t, &inProgress, func() ([]bd.Bead, error) {
		return []bd.Bead{beads["mc-dead"], beads["mc-live"], beads["mc-hitl"],
			beads["mc-human"], beads["mc-nest"]}, nil
	})
	set(t, &pausedList, func() ([]bd.Bead, error) {
		return []bd.Bead{beads["mc-dead"], beads["mc-amb"], beads["mc-reb"],
			{ID: "mc-man", Kind: contract.KindBuild, Status: "open", Labels: []string{"paused:manual"}}}, nil
	})
	set(t, &worktrees, func() ([]git.Worktree, error) {
		return []git.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/tmp/mugicrew-wt-1/wt", Branch: "effort/mc-dead"},
			{Path: "/tmp/mugicrew-wt-2/wt"}, // detached: stopped mid-rebase on effort/mc-reb
			{Path: "/tmp/mugicrew-wt-3/wt", Branch: "effort/mc-busy"},
			{Path: "/tmp/mugicrew-wt-land/wt"}, // a detached land tree
			{Path: "/tmp/mugicrew-wt-4/wt"},    // a detached host: a declared child holds effort/mc-nest in it
		}, nil
	})
	set(t, &rebasing, func() (map[string]string, error) {
		return map[string]string{"/tmp/mugicrew-wt-2/wt": "effort/mc-reb"}, nil
	})
	set(t, &nestedHosts, func() (map[string]string, error) {
		return map[string]string{"/tmp/mugicrew-wt-4/wt": "effort/mc-nest"}, nil
	})
	set(t, &show, func(id string) (bd.Bead, error) { return beads[id], nil })
	set(t, &holderAcross, func(branch string, _ map[string]string) (git.Holder, git.Repo, error) {
		if branch == "effort/mc-reb" {
			return git.Holder{Path: "/tmp/mugicrew-wt-2/wt", Rebasing: true}, "", nil
		}
		return git.Holder{Path: trees[branch]}, "", nil
	})
	set(t, &dirtyFiles, func(p string) (int, error) {
		if p == "/tmp/mugicrew-wt-1/wt" {
			return 3, nil
		}
		return 0, nil
	})
	set(t, &drafts, func(id string) ([]string, error) {
		if id == "mc-dead" {
			return []string{"mc-d1", "mc-d2"}, nil
		}
		return nil, nil
	})
	set(t, &sessions, func() (map[string]session.Info, error) {
		return map[string]session.Info{"mc-hitl": {Session: "mc-hitl", State: session.Exited}}, nil
	})
	set(t, &probe, func(id string) []int {
		if id == "mc-live" {
			return []int{7}
		}
		return nil
	})
	set(t, &inTree, func(tree string, _ []int) ([]int, error) {
		if tree == "/tmp/mugicrew-wt-3/wt" {
			return []int{9}, nil
		}
		return nil, nil
	})
	set(t, &loopState, func() (string, error) { return looplock.Off, nil })
	set(t, &tuiAlive, func(string) (bool, error) { return false, nil })
	set(t, &lineage, func(pid int) []int { return []int{pid} })
	set(t, &pause, func(string, string) error { write("pause"); return nil })
	set(t, &unpause, func(string, string) error { write("unpause"); return nil })
	set(t, &abortRebase, func(string) error { write("abort rebase"); return nil })
	set(t, &dropTree, func(string) error { write("drop"); return nil })
	set(t, &deleteDrafts, func(string) ([]string, error) { write("delete drafts"); return nil, nil })
	set(t, &unclaim, func(string) error { write("unclaim"); return nil })

	var out bytes.Buffer
	if err := List(&out); err != nil {
		t.Fatalf("List: %v", err)
	}
	s := out.String()
	row := func(id string) string {
		for l := range strings.Lines(s) {
			if strings.HasPrefix(l, id+" ") {
				return l
			}
		}
		t.Errorf("no row for %s:\n%s", id, s)
		return ""
	}
	for id, want := range map[string][]string{
		"mc-dead": {"dead", "claimed", "/tmp/mugicrew-wt-1/wt (3 dirty)", "2 Drafts", "paused abandoned", "mugicrew recover mc-dead --discard"},
		"mc-live": {"alive", "claimed", "mugicrew abort mc-live"},
		"mc-amb":  {"ambiguous", "--force"},
		"mc-hitl": {"refused", "drop it from the TUI"},
		"mc-nest": {"ambiguous", "/tmp/mugicrew-wt-4/wt"},
		"mc-reb":  {"dead", "/tmp/mugicrew-wt-2/wt", "mid-rebase", "re-run mugicrew recover mc-reb"},
	} {
		r := row(id)
		for _, w := range want {
			if !strings.Contains(r, w) {
				t.Errorf("%s row missing %q: %q", id, w, r)
			}
		}
	}
	for _, not := range []string{"mc-busy", "mc-man", "mc-human", "/repo\n"} {
		if strings.Contains(s, not) {
			t.Errorf("listing shows %q, which is not stranded:\n%s", not, s)
		}
	}
	head := strings.Index(s, "UNOWNED")
	if head < 0 || !strings.Contains(s[head:], "/tmp/mugicrew-wt-land/wt") || strings.Contains(s[head:], "mugicrew-wt-4") || strings.Contains(s[:head], "mugicrew-wt-land") {
		t.Errorf("the land tree must sit under its own UNOWNED heading:\n%s", s)
	}
	if len(writes) > 0 || len(*evs) > 0 {
		t.Errorf("the listing wrote %v and logged %v; want neither", writes, eventlogtest.Names(*evs))
	}
}

// Nothing stranded says so.
func TestListEmpty(t *testing.T) {
	set(t, &inProgress, func() ([]bd.Bead, error) { return nil, nil })
	set(t, &pausedList, func() ([]bd.Bead, error) { return nil, nil })
	set(t, &worktrees, func() ([]git.Worktree, error) { return []git.Worktree{{Path: "/repo", Branch: "main"}}, nil })
	set(t, &rebasing, func() (map[string]string, error) { return nil, nil })
	var out bytes.Buffer
	if err := List(&out); err != nil || out.String() != "nothing stranded\n" {
		t.Errorf("List = %q, %v; want nothing stranded", out.String(), err)
	}
}
