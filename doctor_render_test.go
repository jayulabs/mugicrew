package main

import (
	"strings"
	"testing"
)

// canned rows modelled on the prototype's scenarios (mc-7u7g6.3).
func okRows(names ...string) []result {
	var rs []result
	for _, n := range names {
		rs = append(rs, quiet(stOK, n, "fine"))
	}
	return rs
}

var (
	trackedRow = trackedBeadsResult([]string{".beads/issues.jsonl", ".beads/config.yaml"})
	wrongJira  = problem(stFail, "acli site", "acli is on other.atlassian.net, [jira] site is acme.atlassian.net",
		"acli's active site is [jira] site (acme.atlassian.net)",
		fix{safe: []string{"acli", "jira", "auth", "switch", "--site", "acme.atlassian.net"}, steps: []string{"run `acli jira auth login`"}, more: "docs/install.md#acli"})
	browserRow = problem(stWarn, "browser", "BROWSER unset", "BROWSER names a browser on PATH",
		manual("", "set BROWSER", "until then the prototype hand-over prints the target only"))
)

func TestRenderCleanRun(t *testing.T) {
	got := plainStyle.report(okRows("bd", "git", "tmux"))
	want := `ok  bd: fine
ok  git: fine
ok  tmux: fine

all 3 checks ok
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderBrokenScenario(t *testing.T) {
	rows := append(okRows("bd"), trackedRow, wrongJira, browserRow)
	got := plainStyle.report(rows)
	want := `ok  bd: fine
FAIL  beads tracked: .beads/issues.jsonl, .beads/config.yaml
FAIL  acli site: acli is on other.atlassian.net, [jira] site is acme.atlassian.net
warn  browser: BROWSER unset

To fix

1. FAIL beads tracked
   Expected: git's index tracks no path under .beads/.
   Manual — do it yourself:
     • on a branch run ` + "`git rm -r --cached .beads`" + `
     • merge it through the Workspace's usual MR or land flow
     • re-run ` + "`mugicrew init`" + `
     • every other clone re-runs ` + "`mugicrew init`" + ` after it pulls (the pull
       deletes their .beads files)
     • optional, in the same MR: the bd block in CLAUDE.md/AGENTS.md and bd's
       root .gitignore entries

2. FAIL acli site
   Expected: acli's active site is [jira] site (acme.atlassian.net).
   Safe — doctor fix runs:
     acli jira auth switch --site acme.atlassian.net
   More: docs/install.md#acli

3. warn browser
   Expected: BROWSER names a browser on PATH.
   Manual — do it yourself:
     • set BROWSER
     • until then the prototype hand-over prints the target only

2 FAIL, 1 warn, 1 ok · 1 safe fix: run mugicrew doctor fix
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderFreshMachine(t *testing.T) {
	dolt := problem(stFail, "dolt", "not on PATH", "dolt is on PATH", manual("dolt", "need ≥ 2.3.0: install pinned 2.5.0 tarball, see docs/install.md#dolt"))
	tmux := problem(stFail, "tmux", "3.4 is below 3.5", "tmux 3.5 or later is on PATH", manual("core", "install apt install tmux"))
	graph := problem(stFail, "bead graph", "not initialized or not resolvable", "a Bead Graph resolves for this clone", manual("", "run `mugicrew init`"))
	got := plainStyle.report([]result{dolt, quiet(stOK, "git", "2.43.0"), tmux, graph, quiet(stSkip, "bd store", "mode unknown")})
	want := `FAIL  dolt: not on PATH
ok  git: 2.43.0
FAIL  tmux: 3.4 is below 3.5
FAIL  bead graph: not initialized or not resolvable
skip  bd store: mode unknown

To fix

1. FAIL dolt
   Expected: dolt is on PATH.
   Manual — do it yourself:
     • need ≥ 2.3.0: install pinned 2.5.0 tarball, see docs/install.md#dolt
   More: docs/install.md#dolt

2. FAIL tmux
   Expected: tmux 3.5 or later is on PATH.
   Manual — do it yourself:
     • install apt install tmux
   More: docs/install.md#core

3. FAIL bead graph
   Expected: a Bead Graph resolves for this clone.
   Manual — do it yourself:
     • run ` + "`mugicrew init`" + `

3 FAIL, 1 ok
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderSummaryVariants(t *testing.T) {
	for _, c := range []struct {
		name string
		rows []result
		want string
	}{
		{"clean", okRows("a", "b"), "all 2 checks ok"},
		{"clean with a skip", append(okRows("a"), quiet(stSkip, "s", "x")), "all 1 checks ok"},
		{"warn only", append(okRows("a"), browserRow), "1 warn, 1 ok"},
		{"fail only", append(okRows("a"), trackedRow), "1 FAIL, 1 ok"},
		{"one safe fix", append(okRows("a"), wrongJira), "1 FAIL, 1 ok · 1 safe fix: run mugicrew doctor fix"},
		{"two safe fix, no pluralising", []result{wrongJira, wrongJira, browserRow}, "2 FAIL, 1 warn, 0 ok · 2 safe fix: run mugicrew doctor fix"},
	} {
		if got := plainStyle.summary(c.rows); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRenderWrapsWithHangingIndent(t *testing.T) {
	long := problem(stFail, "x", "d", strings.Repeat("alpha ", 20)+"omega",
		manual("", strings.Repeat("beta ", 30)+"zeta"))
	got := style{width: 40}.toFixItem(1, long)
	want := `1. FAIL x
   Expected: alpha alpha alpha alpha
   alpha alpha alpha alpha alpha alpha
   alpha alpha alpha alpha alpha alpha
   alpha alpha alpha alpha omega.
   Manual — do it yourself:
     • beta beta beta beta beta beta
       beta beta beta beta beta beta
       beta beta beta beta beta beta
       beta beta beta beta beta beta
       beta beta beta beta beta beta
       zeta`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderColour(t *testing.T) {
	rows := []result{quiet(stOK, "a", "x"), browserRow, trackedRow, wrongJira}
	on := style{color: true, width: plainWidth}.report(rows)
	for _, code := range []string{"\x1b[32mok", "\x1b[33mwarn", "\x1b[1;31mFAIL", "\x1b[36macli jira auth switch", "\x1b[1mTo fix", "\x1b[2mMore: docs/install.md#acli"} {
		if !strings.Contains(on, code) {
			t.Errorf("colour output lacks %q:\n%q", code, on)
		}
	}
	if off := plainStyle.report(rows); strings.Contains(off, "\x1b") {
		t.Errorf("plain output has escape codes: %q", off)
	}
	var b strings.Builder
	if s := styleFor(&b); s.color || s.width != plainWidth {
		t.Errorf("piped style = %+v", s)
	}
}
