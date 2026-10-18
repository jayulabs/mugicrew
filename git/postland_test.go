package git

import (
	"os"
	"reflect"
	"testing"
)

// head is the current commit's sha.
func head(t *testing.T) string { t.Helper(); return mustGit(t, "rev-parse", "HEAD") }

// moved is PostLandClashes(landed) as "added vs others" strings, for comparing.
func moved(t *testing.T, landed string) []string {
	t.Helper()
	cs, err := PostLandClashes(landed)
	if err != nil {
		t.Fatalf("PostLandClashes(%s): %v", landed, err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Added+" vs "+c.Others[0])
	}
	return out
}

func wantMoved(t *testing.T, landed string, want ...string) {
	t.Helper()
	if got := moved(t, landed); !reflect.DeepEqual(got, want) {
		t.Errorf("PostLandClashes = %v, want %v", got, want)
	}
}

const (
	adrX = "docs/adr/0002-x.md"
	adrY = "docs/adr/0002-y.md"
)

// The later side of a Forge-merged clash is the one reported, by first-parent
// ancestry on mainline (ADR 0225), whatever shape the land took.
func TestPostLandClashesOrdersByFirstParentAncestry(t *testing.T) {
	t.Run("ff: later add moves, earlier reports nothing", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-base.md")
		addADR(t, "0002-x.md")
		x := head(t)
		addADR(t, "0002-y.md")
		y := head(t)
		wantMoved(t, y, adrY+" vs "+adrX)
		wantMoved(t, x)
		if cs, _ := PostLandClashes(y); cs[0].Mainline != y {
			t.Errorf("Mainline = %s, want %s", cs[0].Mainline, y)
		}
	})

	// two branches cut from the same base, each adding ADR 2
	twoBranches := func(t *testing.T) (x, y string) {
		newRepo(t)
		addADR(t, "0001-base.md")
		mustGit(t, "checkout", "-b", "bx")
		addADR(t, "0002-x.md")
		x = head(t)
		mustGit(t, "checkout", "-b", "by", "main")
		addADR(t, "0002-y.md")
		y = head(t)
		mustGit(t, "checkout", "main")
		mustGit(t, "merge", "--ff-only", "bx")
		return x, y
	}
	t.Run("rebase_merge: semi-linear merge of a rebased commit", func(t *testing.T) {
		x, _ := twoBranches(t)
		mustGit(t, "checkout", "by")
		mustGit(t, "rebase", "main")
		yr := head(t)
		mustGit(t, "checkout", "main")
		mustGit(t, "merge", "--no-ff", "-m", "merge by", "by")
		wantMoved(t, yr, adrY+" vs "+adrX)
		wantMoved(t, x)
	})
	t.Run("merge on a stale base", func(t *testing.T) {
		x, y := twoBranches(t)
		mustGit(t, "merge", "--no-ff", "-m", "merge by", "by")
		wantMoved(t, y, adrY+" vs "+adrX)
		wantMoved(t, x)
	})
	t.Run("merge: the earlier side merged second still does not move", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-base.md")
		mustGit(t, "checkout", "-b", "bx")
		addADR(t, "0002-x.md")
		x := head(t)
		mustGit(t, "checkout", "-b", "by", "main")
		addADR(t, "0002-y.md")
		y := head(t)
		mustGit(t, "checkout", "main")
		mustGit(t, "merge", "--no-ff", "-m", "merge by", "by")
		mustGit(t, "merge", "--no-ff", "-m", "merge bx", "bx")
		wantMoved(t, x, adrX+" vs "+adrY)
		wantMoved(t, y)
	})
	t.Run("git mv onto a taken number counts as an add", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-old.md")
		addADR(t, "0002-x.md")
		mustGit(t, "mv", "docs/adr/0001-old.md", adrY)
		mustGit(t, "commit", "-m", "renumber")
		wantMoved(t, head(t), adrY+" vs "+adrX)
	})
	t.Run("deleted then re-added other reads its current add", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0002-x.md")
		addADR(t, "0002-y.md")
		y := head(t)
		mustGit(t, "rm", adrX)
		mustGit(t, "commit", "-m", "rm x")
		addADR(t, "0002-x.md")
		wantMoved(t, y) // x's newest add is after y's: y is earlier
	})
	t.Run("both adds in one first-parent commit are skipped", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-base.md")
		writeFile(t, adrX, "x\n")
		writeFile(t, adrY, "y\n")
		mustGit(t, "add", "-A")
		mustGit(t, "commit", "-m", "both")
		wantMoved(t, head(t))
	})
	t.Run("a duplicate the landed commit did not add is never reported", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0002-x.md")
		addADR(t, "0002-y.md")
		addADR(t, "0003-z.md")
		wantMoved(t, head(t))
	})
	t.Run("a non-ADR sequence rule is detected the same way", func(t *testing.T) {
		withRules(t, "db/migration/V{n}__*.sql")
		newRepo(t)
		commitOn(t, "", "db/migration/V1__a.sql")
		commitOn(t, "", "db/migration/V001__b.sql")
		wantMoved(t, head(t), "db/migration/V001__b.sql vs db/migration/V1__a.sql")
	})
}

// The other side's container is named from its land trailer when one is found,
// on the commit itself or, for a merge, in the c^1..c range; "" otherwise.
func TestPostLandClashesNamesOtherContainer(t *testing.T) {
	landedBy := func(t *testing.T, y string) string {
		t.Helper()
		cs, err := PostLandClashes(y)
		if err != nil || len(cs) != 1 {
			t.Fatalf("PostLandClashes = %v, %v", cs, err)
		}
		if cs[0].Commits[0] == "" {
			t.Error("Commits not kept")
		}
		return cs[0].LandedBy[0]
	}
	t.Run("trailer on the other side's commit", func(t *testing.T) {
		newRepo(t)
		_ = os.MkdirAll("docs/adr", 0o755)
		writeFile(t, adrX, "x\n")
		mustGit(t, "add", ".")
		mustGit(t, "commit", "-m", "land x\n\n"+TrailerFor("checkpoint", "mc-x")+"\nGraph: g")
		addADR(t, "0002-y.md")
		if got := landedBy(t, head(t)); got != "mc-x" {
			t.Errorf("LandedBy = %q, want mc-x", got)
		}
	})
	t.Run("trailer inside a merge's range", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-base.md")
		mustGit(t, "checkout", "-b", "bx")
		writeFile(t, adrX, "x\n")
		mustGit(t, "add", ".")
		mustGit(t, "commit", "-m", "land x\n\n"+TrailerFor("chart", "mc-cx"))
		mustGit(t, "checkout", "main")
		mustGit(t, "merge", "--no-ff", "-m", "merge bx", "bx")
		addADR(t, "0002-y.md")
		if got := landedBy(t, head(t)); got != "mc-cx" {
			t.Errorf("LandedBy = %q, want mc-cx", got)
		}
	})
	t.Run("no trailer", func(t *testing.T) {
		newRepo(t)
		addADR(t, "0002-x.md")
		addADR(t, "0002-y.md")
		if got := landedBy(t, head(t)); got != "" {
			t.Errorf("LandedBy = %q, want empty", got)
		}
	})
}

// A clash whose added path a later renumber already moved off the tip is settled:
// it is not reported again.
func TestPostLandClashesSkipsAddedPathAbsentFromTip(t *testing.T) {
	newRepo(t)
	addADR(t, "0001-base.md")
	addADR(t, "0002-x.md")
	addADR(t, "0002-y.md")
	y := head(t)
	mustGit(t, "mv", adrY, "docs/adr/0003-y.md")
	mustGit(t, "commit", "-m", "renumber y")
	wantMoved(t, y)
}
