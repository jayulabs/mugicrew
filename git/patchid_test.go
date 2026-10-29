package git

import (
	"strings"
	"testing"
)

func lines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i))
		b.WriteString("\n")
	}
	return b.String()
}

// patchIDRepo is a scratch repo with a shared file on main and a chart branch cut
// from it with one hunk at the top.
func patchIDRepo(t *testing.T) {
	newRepo(t)
	writeFile(t, "f.txt", lines(12))
	writeFile(t, "bin.dat", "\x00\x01\x02")
	writeFile(t, "0001-a.md", "adr\n")
	mustGit(t, "add", ".")
	mustGit(t, "commit", "-m", "base")
	mustGit(t, "checkout", "-b", "chart/c")
	writeFile(t, "f.txt", strings.Replace(lines(12), "line x\n", "chart edit\n", 1))
	mustGit(t, "commit", "-am", "chart work")
	mustGit(t, "checkout", "main")
}

func mustPatchID(t *testing.T, pin string) string {
	t.Helper()
	id, err := ChartPatchID("", "main", pin)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestChartPatchID(t *testing.T) {
	patchIDRepo(t)
	orig := mustPatchID(t, "chart/c")
	if len(orig) != 40 {
		t.Fatalf("id = %q, want a 40-hex patch-id", orig)
	}

	// a plain rebase over a mainline edit two lines from the hunk keeps the id
	writeFile(t, "f.txt", strings.Replace(lines(12), "line xxx\n", "mainline edit\n", 1))
	mustGit(t, "commit", "-am", "mainline")
	mustGit(t, "checkout", "-b", "chart/rebased", "chart/c")
	mustGit(t, "rebase", "main")
	mustGit(t, "checkout", "main")
	if got := mustPatchID(t, "chart/rebased"); got != orig {
		t.Errorf("rebased id = %s, want %s", got, orig)
	}

	// the same content as two commits keeps the id
	mustGit(t, "checkout", "-b", "chart/split", "main")
	writeFile(t, "f.txt", strings.Replace(strings.Replace(lines(12), "line xxx\n", "mainline edit\n", 1), "line x\n", "chart edit\n", 1))
	mustGit(t, "commit", "-am", "one")
	mustGit(t, "commit", "--allow-empty", "-m", "two")
	mustGit(t, "checkout", "main")
	if got := mustPatchID(t, "chart/split"); got != orig {
		t.Errorf("split id = %s, want %s", got, orig)
	}

	// whitespace-only, binary, rename-only each change the id
	for name, edit := range map[string]func(){
		"ws":     func() { writeFile(t, "f.txt", strings.Replace(lines(12), "line x\n", "chart edit \n", 1)) },
		"bin":    func() { writeFile(t, "bin.dat", "\x00\x01\x03") },
		"rename": func() { mustGit(t, "mv", "0001-a.md", "0002-a.md") },
	} {
		mustGit(t, "checkout", "-b", "chart/"+name, "main")
		edit()
		mustGit(t, "add", ".")
		mustGit(t, "commit", "-m", name)
		mustGit(t, "checkout", "main")
		if got := mustPatchID(t, "chart/"+name); got == "" || got == orig {
			t.Errorf("%s id = %q, want a new id", name, got)
		}
	}
}

// a conflict resolved by keeping the Chart's side drops mainline's edit, so the squash diff changes
func TestChartPatchIDConflictKeepingChartSide(t *testing.T) {
	patchIDRepo(t)
	orig := mustPatchID(t, "chart/c")
	writeFile(t, "f.txt", strings.Replace(lines(12), "line x\n", "mainline edit\n", 1))
	mustGit(t, "commit", "-am", "mainline")
	mustGit(t, "checkout", "-b", "chart/res", "chart/c")
	mustGit(t, "merge", "-X", "ours", "main")
	mustGit(t, "checkout", "main")
	if got := mustPatchID(t, "chart/res"); got == "" || got == orig {
		t.Errorf("resolved id = %q, want a new id", got)
	}
}

func TestChartPatchIDEmptyDiff(t *testing.T) {
	patchIDRepo(t)
	mustGit(t, "branch", "chart/none", "main")
	if id, err := ChartPatchID("", "main", "chart/none"); id != "" || err != nil {
		t.Errorf("empty diff = %q, %v; want no id, no error", id, err)
	}
}

func TestPatchIDPin(t *testing.T) {
	body := CommitPin("abc") + "\n" + PatchIDPin("deadbeef") + "\n\n## Background"
	if got, ok := ParsePatchID(body); !ok || got != "deadbeef" {
		t.Errorf("ParsePatchID = %q, %v", got, ok)
	}
	if got, ok := ParseCommit(body); !ok || got != "abc" {
		t.Errorf("ParseCommit = %q, %v", got, ok)
	}
	if _, ok := ParsePatchID(CommitPin("abc")); ok {
		t.Error("ParsePatchID found a pin in a pin-less body")
	}
}

func TestDecisionPacketStripsPatchID(t *testing.T) {
	for body, want := range map[string]string{
		"Commit: abc\nPatch-id: 123\n\n## Background\n\nwhy": "## Background\n\nwhy",
		"Commit: abc\nPatch-id: 123":                         "",
		"Commit: abc\n\n## Background\n\nwhy":                "## Background\n\nwhy",
		"Commit: abc\n\nPatch-id: 123\nkept":                 "Patch-id: 123\nkept",
	} {
		if got := DecisionPacket(body); got != want {
			t.Errorf("DecisionPacket(%q) = %q, want %q", body, got, want)
		}
	}
}
