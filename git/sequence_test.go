package git

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mustRule(t *testing.T, tmpl string) Rule {
	t.Helper()
	r, err := CompileRule(tmpl)
	if err != nil {
		t.Fatalf("CompileRule(%q) = %v", tmpl, err)
	}
	return r
}

// withRules injects extra templates through the SequenceRules seam for one test.
func withRules(t *testing.T, tmpls ...string) {
	t.Helper()
	old := SequenceRules
	t.Cleanup(func() { SequenceRules = old })
	SequenceRules = func(Repo) ([]Rule, error) {
		rules := []Rule{ADRRule}
		for _, s := range tmpls {
			rules = append(rules, mustRule(t, s))
		}
		return rules, nil
	}
}

func TestCompileRuleGrammar(t *testing.T) {
	for _, bad := range []string{"", "a/b.sql", "a/{n}{n}.sql", "a/{n}/x.sql", "a/{n:0}.sql", "a/{n:x}.sql", "{n}/x.sql", "a/V{m}.sql"} {
		if _, err := CompileRule(bad); err == nil {
			t.Errorf("CompileRule(%q) compiled, want an error", bad)
		}
	}
}

// The built-in rule classifies as adrParts did (TestADRNumber) plus the prototype's edge cases.
func TestADRRuleParity(t *testing.T) {
	for path, want := range map[string]string{
		"docs/adr/0030-guard.md":               "docs/adr/ 30",
		"apps/mugicrew/docs/adr/0030-guard.md": "apps/mugicrew/docs/adr/ 30",
		"apps/other/docs/adr/0001-x.md":        "apps/other/docs/adr/ 1",
		"docs/adr/README.md":                   " ",
		"docs/adr/0030-guard.txt":              " ",
		"docs/adr/sub/0030-x.md":               " ",
		"apps/mugicrew/docs/adr/sub/0030-x.md": " ",
		"other/0030-guard.md":                  " ",
		"docs/adr/003a-guard.md":               " ",
		"apps/mugicrewdocs/adr/0030-guard.md":  " ",
		"docs/adr/030-x.md":                    " ",
		"docs/adr/00030-x.md":                  " ",
		"docs/adr/1-x.md":                      " ",
		"docs/adr/x/docs/adr/0001-y.md":        "docs/adr/x/docs/adr/ 1",
	} {
		dir, key, _ := seqParts(ADRRule, path)
		if dir+" "+key != want {
			t.Errorf("seqParts(%q) = %q %q, want %q", path, dir, key, want)
		}
	}
}

func TestSeqPartsFlyway(t *testing.T) {
	r := mustRule(t, "db/migration/V{n}__*.sql")
	for path, want := range map[string]string{
		"db/migration/V1__a.sql":     "db/migration/ 1",
		"db/migration/V001__a.sql":   "db/migration/ 1",
		"db/migration/V00__a.sql":    "db/migration/ 0",
		"db/migration/V1.1__a.sql":   " ",
		"db/migration/R__view.sql":   " ",
		"db/migration/sub/V1__a.sql": " ",
	} {
		dir, key, _ := seqParts(r, path)
		if dir+" "+key != want {
			t.Errorf("seqParts(%q) = %q %q, want %q", path, dir, key, want)
		}
	}
	svc := mustRule(t, "services/*/migrations/V{n}__*.sql")
	a, _, _ := seqParts(svc, "services/a/migrations/V1__x.sql")
	b, _, _ := seqParts(svc, "services/b/migrations/V1__x.sql")
	if a == b || a == "" {
		t.Errorf("per-directory spaces: %q vs %q, want distinct non-empty", a, b)
	}
}

func TestRenumberPath(t *testing.T) {
	fly, adr := mustRule(t, "db/migration/V{n}__*.sql"), ADRRule
	for _, c := range []struct {
		r    Rule
		p    string
		n    int
		want string
	}{
		{adr, "docs/adr/0012-x.md", 13, "docs/adr/0013-x.md"},
		{adr, "docs/adr/9999-x.md", 10000, ""},
		{fly, "db/migration/V9__a.sql", 10, "db/migration/V10__a.sql"},
		{fly, "db/migration/V001__a.sql", 2, "db/migration/V002__a.sql"},
		{fly, "db/migration/V1__a.sql", 3, "db/migration/V3__a.sql"},
		{fly, "db/migration/V1__v2.sql", 5, "db/migration/V5__v2.sql"},
		{fly, "db/migration/R__a.sql", 5, ""},
	} {
		if got := renumberPath(c.r, c.p, c.n); got != c.want {
			t.Errorf("renumberPath(%q, %d) = %q, want %q", c.p, c.n, got, c.want)
		}
	}
}

func commitOn(t *testing.T, branch, file string) {
	t.Helper()
	if branch != "" {
		mustGit(t, "checkout", branch)
	}
	commitFile(t, file, "x\n")
}

// Land runs an injected Flyway rule beside the built-in one.
func TestLandSequenceRules(t *testing.T) {
	withRules(t, "db/migration/V{n}__*.sql")
	const m = "db/migration/"
	land := func() error { _, err := Land("chart/x", TrailerFor("chart", "x"), LandLocal); return err }
	t.Run("V1 clashes V001", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		commitOn(t, "", m+"V1__a.sql")
		mustGit(t, "checkout", Mainline())
		commitOn(t, "", m+"V001__b.sql")
		var c *NumberClash
		err := land()
		if !errors.As(err, &c) || len(c.Clashes) != 1 ||
			c.Clashes[0] != m+"V1__a.sql (rule db/migration/V{n}__*.sql) (vs mainline "+m+"V001__b.sql)" {
			t.Fatalf("Land = %v, want one rendered clash", err)
		}
		if !strings.Contains(err.Error(), "numbered-file clash") {
			t.Errorf("error %q lacks %q", err, "numbered-file clash")
		}
	})
	t.Run("V1.1 is ignored", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		commitOn(t, "", m+"V1.1__a.sql")
		mustGit(t, "checkout", Mainline())
		commitOn(t, "", m+"V1.1__b.sql")
		if err := land(); err != nil {
			t.Fatalf("Land = %v, want landed", err)
		}
	})
	t.Run("a mainline-only duplicate does not block", func(t *testing.T) {
		newRepo(t)
		commitOn(t, "", m+"V1__a.sql")
		commitOn(t, "", m+"V01__b.sql")
		mustGit(t, "checkout", "-b", "chart/x")
		commitOn(t, "", m+"V2__c.sql")
		if err := land(); err != nil {
			t.Fatalf("Land = %v, want landed", err)
		}
	})
	t.Run("a path matching two rules is a plain error", func(t *testing.T) {
		withRules(t, "db/migration/V{n}__*.sql", "db/*/V{n}__a.sql")
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		commitOn(t, "", m+"V1__a.sql")
		err := land()
		var c *NumberClash
		if err == nil || errors.As(err, &c) || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("Land = %v, want a plain overlap error", err)
		}
	})
}

func TestCheckResolvedSequenceRules(t *testing.T) {
	withRules(t, "db/migration/V{n}__*.sql")
	const m = "db/migration/"
	scene := func(t *testing.T) (base, tip string) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "chart/x")
		commitFile(t, m+"V1__chart.sql", "x\n")
		tip = mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", Mainline())
		commitFile(t, m+"V1__main.sql", "x\n")
		base = mustGit(t, "rev-parse", "HEAD")
		mustGit(t, "checkout", "chart/x")
		mustGit(t, "rebase", base)
		return
	}
	t.Run("assertion 2 on an injected sequence", func(t *testing.T) {
		base, tip := scene(t)
		wantCheckFailure(t, CheckResolved(base, tip), 2, m+"V1__chart.sql (rule db/migration/V{n}__*.sql) (vs mainline "+m+"V1__main.sql)")
	})
	t.Run("assertion 3 exempts a renumbered migration, flags a stray edit", func(t *testing.T) {
		base, tip := scene(t)
		mustGit(t, "mv", m+"V1__chart.sql", m+"V2__chart.sql")
		mustGit(t, "commit", "-m", "renumber")
		if err := CheckResolved(base, tip); err != nil {
			t.Fatalf("CheckResolved = %v, want a renumber to pass", err)
		}
		commitFile(t, m+"V9__stray.sql", "x\n")
		wantCheckFailure(t, CheckResolved(base, tip), 3, m+"V9__stray.sql")
	})
}

func TestSeqNext(t *testing.T) {
	rules := []Rule{ADRRule, mustRule(t, "db/migration/V{n}__*.sql")}
	main := []string{"docs/adr/0394-a.md", "docs/adr/0395-b.md", "apps/x/docs/adr/0900-z.md", "db/migration/V7__x.sql", "README"}
	for _, tc := range []struct {
		name, path string
		head       []string
		others     []branchAdds
		want, err  string
	}{
		{"mainline max only", "docs/adr/0381-me.md", nil, nil, "0396", ""},
		{"nested dir has its own space", "apps/x/docs/adr/0001-me.md", nil, nil, "0901", ""},
		{"open branches of each kind reach above", "docs/adr/0381-me.md", nil, []branchAdds{
			{"refs/heads/checkpoint/a", []string{"docs/adr/0397-c.md"}},
			{"refs/heads/chart/b", []string{"docs/adr/0399-d.md"}},
			{"refs/heads/effort/c", []string{"docs/adr/0398-e.md", "apps/x/docs/adr/0950-o.md"}},
		}, "0400", ""},
		{"same slug on another lap is skipped", "docs/adr/0381-me.md", []string{"docs/adr/0381-me.md"}, []branchAdds{
			{"refs/heads/effort/c", []string{"docs/adr/0420-me.md"}},
			{"refs/heads/chart/b", []string{"docs/adr/0397-other.md"}},
		}, "0398", ""},
		{"head's own higher same-directory add is cleared", "docs/adr/0381-me.md", []string{"docs/adr/0381-me.md", "docs/adr/0410-sib.md"}, []branchAdds{
			{"refs/heads/chart/b", []string{"docs/adr/0397-other.md"}},
		}, "0411", ""},
		{"p itself as head's max is excluded", "docs/adr/0450-me.md", []string{"docs/adr/0450-me.md"}, nil, "0396", ""},
		{"head's adds in another directory or rule are ignored", "docs/adr/0381-me.md", []string{"docs/adr/0381-me.md", "apps/x/docs/adr/0990-s.md", "db/migration/V99__s.sql"}, nil, "0396", ""},
		{"same slug in another directory is not the same file", "docs/adr/0381-me.md", []string{"docs/adr/0381-me.md"}, []branchAdds{
			{"refs/heads/effort/c", []string{"apps/x/docs/adr/0420-me.md"}},
		}, "0396", ""},
		{"configured V{n} rule keeps own width", "db/migration/V03__me.sql", nil, []branchAdds{
			{"refs/heads/chart/b", []string{"db/migration/V12__o.sql", "docs/adr/0999-q.md"}},
		}, "13", ""},
		{"no rule", "docs/notes.md", nil, nil, "", "matches no sequence rule"},
		{"width overflow", "docs/adr/0381-me.md", nil, []branchAdds{
			{"refs/heads/chart/b", []string{"docs/adr/9999-big.md"}},
		}, "", "outgrows"},
	} {
		got, err := seqNext(rules, tc.path, main, tc.head, tc.others)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: = %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestSeqNextTwoRulesIsAnError(t *testing.T) {
	rules := []Rule{ADRRule, mustRule(t, "docs/adr/{n:4}-*.md")}
	if _, err := seqNext(rules, "docs/adr/0001-x.md", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("err = %v, want an overlap error", err)
	}
}

// End to end in a real repo: mainline holds 0002, an open checkpoint branch adds
// 0005, the current effort branch adds 0003 (its own, ignored); the verb prints 0006.
func TestSequenceNextRealGit(t *testing.T) {
	newRepo(t)
	add := func(name string) {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, name, "x\n")
		mustGit(t, "add", ".")
		mustGit(t, "commit", "-m", name)
	}
	add("docs/adr/0002-main.md")
	mustGit(t, "checkout", "-b", "checkpoint/mc-a")
	add("docs/adr/0005-theirs.md")
	mustGit(t, "checkout", "-b", "effort/mc-a.1", "main")
	add("docs/adr/0003-mine.md")
	got, err := Repo("").SequenceNext("docs/adr/0003-mine.md")
	if err != nil || got != "0006" {
		t.Errorf("SequenceNext = %q, %v; want 0006", got, err)
	}
	// The branch's own other add clears too: 0008 beats the other branch's 0005.
	add("docs/adr/0008-sibling.md")
	got, err = Repo("").SequenceNext("docs/adr/0003-mine.md")
	if err != nil || got != "0009" {
		t.Errorf("SequenceNext with own sibling = %q, %v; want 0009", got, err)
	}
}

func TestSeqClashesIntraBranch(t *testing.T) {
	rules := []Rule{ADRRule, mustRule(t, "db/migration/V{n}__*.sql")}
	const a = " (rule **/docs/adr/{n:4}-*.md) (vs this branch's "
	for _, tc := range []struct {
		name          string
		added, main   []string
		want          []string
		wantErrSubstr string
	}{
		{"two adds, one line for the later", []string{"docs/adr/0005-a.md", "docs/adr/0005-b.md"}, nil,
			[]string{"docs/adr/0005-b.md" + a + "docs/adr/0005-a.md)"}, ""},
		{"three adds, two lines each listing earlier siblings", []string{"docs/adr/0005-a.md", "docs/adr/0005-b.md", "docs/adr/0005-c.md"}, nil,
			[]string{"docs/adr/0005-b.md" + a + "docs/adr/0005-a.md)", "docs/adr/0005-c.md" + a + "docs/adr/0005-a.md, docs/adr/0005-b.md)"}, ""},
		{"introduction order, not path order, picks the keeper", []string{"docs/adr/0005-z.md", "docs/adr/0005-a.md"}, nil,
			[]string{"docs/adr/0005-a.md" + a + "docs/adr/0005-z.md)"}, ""},
		{"same number in different directories", []string{"docs/adr/0005-a.md", "apps/x/docs/adr/0005-b.md"}, nil, nil, ""},
		{"same number under different rules", []string{"docs/adr/0005-a.md", "db/migration/V5__b.sql"}, nil, nil, ""},
		{"equal by value across widths is one slot", []string{"db/migration/V5__a.sql", "db/migration/V05__b.sql"}, nil,
			[]string{"db/migration/V05__b.sql (rule db/migration/V{n}__*.sql) (vs this branch's db/migration/V5__a.sql)"}, ""},
		{"mainline number wins: only mainline lines, siblings included", []string{"docs/adr/0005-a.md", "docs/adr/0005-b.md"}, []string{"docs/adr/0005-m.md"},
			[]string{
				"docs/adr/0005-a.md (rule **/docs/adr/{n:4}-*.md) (vs mainline docs/adr/0005-m.md)",
				"docs/adr/0005-b.md (rule **/docs/adr/{n:4}-*.md) (vs mainline docs/adr/0005-m.md)",
			}, ""},
		{"same-path hit is skipped", []string{"docs/adr/0005-a.md"}, []string{"docs/adr/0005-a.md"}, nil, ""},
		{"same path on mainline holds the slot for a sibling", []string{"docs/adr/0005-a.md", "docs/adr/0005-b.md"}, []string{"docs/adr/0005-a.md"},
			[]string{"docs/adr/0005-b.md (rule **/docs/adr/{n:4}-*.md) (vs mainline docs/adr/0005-a.md)"}, ""},
	} {
		got, err := seqClashes(rules, tc.added, tc.main)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: = %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	overlap := []Rule{ADRRule, mustRule(t, "docs/adr/{n:4}-*.md")}
	if _, err := seqClashes(overlap, []string{"docs/adr/0005-a.md", "docs/adr/0005-b.md"}, nil); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("overlap err = %v, want an overlap error", err)
	}
}

// The introducing commit along the branch's first-parent chain decides the keeper;
// one commit (or one merge) bringing both falls back to path order.
func TestNumberClashesIntroductionOrder(t *testing.T) {
	line := func(mover, keeper string) string {
		return "docs/adr/0002-" + mover + ".md (rule **/docs/adr/{n:4}-*.md) (vs this branch's docs/adr/0002-" + keeper + ".md)"
	}
	check := func(t *testing.T, want string) {
		t.Helper()
		base := mustGit(t, "rev-parse", Mainline())
		got, err := numberClashes("HEAD", base)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("numberClashes = %q, %v; want [%q]", got, err, want)
		}
	}
	t.Run("the later commit's file moves even when its path sorts first", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "effort/x")
		addADR(t, "0002-z.md")
		addADR(t, "0002-a.md")
		check(t, line("a", "z"))
	})
	t.Run("one commit adding both: path order", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "effort/x")
		os.MkdirAll("docs/adr", 0o755)
		writeFile(t, "docs/adr/0002-z.md", "z\n")
		writeFile(t, "docs/adr/0002-a.md", "a\n")
		mustGit(t, "add", ".")
		mustGit(t, "commit", "-m", "both")
		check(t, line("z", "a"))
	})
	t.Run("an Integrate-style merge bringing both in: path order", func(t *testing.T) {
		newRepo(t)
		mustGit(t, "checkout", "-b", "checkpoint/x")
		commitFile(t, "keep.txt", "k\n")
		mustGit(t, "checkout", "-b", "effort/y")
		addADR(t, "0002-z.md")
		addADR(t, "0002-a.md")
		mustGit(t, "checkout", "checkpoint/x")
		commitFile(t, "other.txt", "o\n")
		mustGit(t, "merge", "--no-ff", "-m", "integrate", "effort/y")
		check(t, line("z", "a"))
	})
}

func TestLandProbeAndCheckResolvedIntraBranchClash(t *testing.T) {
	const want = "docs/adr/0002-b.md (rule **/docs/adr/{n:4}-*.md) (vs this branch's docs/adr/0002-a.md)"
	scene := func(t *testing.T) {
		newRepo(t)
		addADR(t, "0001-base.md")
		mustGit(t, "checkout", "-b", "chart/x")
		addADR(t, "0002-a.md")
		addADR(t, "0002-b.md")
		mustGit(t, "checkout", Mainline())
	}
	t.Run("Land refuses", func(t *testing.T) {
		scene(t)
		var c *NumberClash
		_, err := Land("chart/x", TrailerFor("chart", "x"), LandLocal)
		if !errors.As(err, &c) || !slices.Equal(c.Clashes, []string{want}) {
			t.Fatalf("Land = %v, want one intra-branch clash", err)
		}
	})
	t.Run("ProbeConflict reports it", func(t *testing.T) {
		scene(t)
		base, err := ResolveProbeBase()
		if err != nil {
			t.Fatal(err)
		}
		var c *NumberClash
		if err := ProbeConflict("chart/x", base); !errors.As(err, &c) || !slices.Equal(c.Clashes, []string{want}) {
			t.Fatalf("ProbeConflict = %v, want one intra-branch clash", err)
		}
	})
	t.Run("CheckResolved fails assertion 2", func(t *testing.T) {
		scene(t)
		base := mustGit(t, "rev-parse", Mainline())
		mustGit(t, "checkout", "chart/x")
		wantCheckFailure(t, CheckResolved(base, mustGit(t, "rev-parse", "HEAD")), 2, want)
	})
}
