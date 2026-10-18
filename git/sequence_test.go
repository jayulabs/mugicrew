package git

import (
	"errors"
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
