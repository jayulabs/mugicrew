package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The explainer site is not a build input of the binary: the flake's src
// exclusion must list it so site edits keep the Nix outPath.
func TestFlakeExcludesSite(t *testing.T) {
	if _, err := os.Stat("site"); err != nil {
		t.Skip("no site/ in this tree")
	}
	b, err := os.ReadFile("flake.nix")
	if err != nil {
		t.Skip("no flake.nix in this tree")
	}
	if !strings.Contains(string(b), "(pkgs.lib.fileset.maybeMissing ./site)") {
		t.Error("flake.nix src exclusion does not list ./site as maybeMissing")
	}
}

// usageVerbs are the verb phrases `mugicrew --help` lists: the leading words
// after "mugicrew" up to the first argument, and, where one line folds several
// verbs, each alternative ("chart append|drop", "loop [--watch | start | stop]",
// "initiative adopt <head> … | release <head>").
func usageVerbs(usage string) []string {
	altWord := regexp.MustCompile(`\| ([a-z][a-z-]*)\b`)
	bare := regexp.MustCompile(`^[a-z][a-z|-]*$`)
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, line := range strings.Split(usage, "\n") {
		rest, ok := strings.CutPrefix(line, "  mugicrew ")
		if !ok {
			continue
		}
		if strings.HasPrefix(rest, " ") { // the bare-TUI lines: only a description follows
			continue
		}
		syn, _, _ := strings.Cut(rest, "  ") // two spaces open the description column
		var words []string
		for _, tok := range strings.Fields(syn) {
			if !bare.MatchString(tok) {
				break
			}
			words = append(words, tok)
		}
		if len(words) == 0 {
			continue
		}
		last := words[len(words)-1]
		prefix := strings.Join(words[:len(words)-1], " ")
		for _, alt := range strings.Split(last, "|") {
			add(strings.TrimSpace(prefix + " " + alt))
		}
		for _, m := range altWord.FindAllStringSubmatch(syn, -1) {
			add(words[0] + " " + m[1])
		}
	}
	return out
}

func TestUsageVerbsParsesPhrases(t *testing.T) {
	got := usageVerbs("  mugicrew chart append|drop <id>\n  mugicrew latch mr <ref>\n  mugicrew initiative adopt <h> <i> | release <h>\n  mugicrew loop [--watch | start | stop]\n  mugicrew -C <dir> ...\n  mugicrew ps [--json]")
	want := []string{"chart append", "chart drop", "latch mr", "initiative adopt", "initiative release", "loop", "loop start", "loop stop", "ps"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("usageVerbs = %v, want %v", got, want)
	}
}

// The CLI verbs page at /learn groups every verb `mugicrew --help` prints; a
// verb added to the usage constant without a place on the page fails here.
func TestLearnVerbsPageListsEveryUsageVerb(t *testing.T) {
	if _, err := os.Stat("site"); err != nil {
		t.Skip("no site/ in this tree")
	}
	b, err := os.ReadFile("site/learn/cli-verbs.mdx")
	if err != nil {
		t.Fatalf("the CLI verbs page is missing: %v", err)
	}
	page := string(b)
	verbs := usageVerbs(usage)
	if len(verbs) < 30 {
		t.Fatalf("parsed only %d verbs from usage", len(verbs))
	}
	for _, v := range verbs {
		// The verb phrase inside backticks, so "init" is not satisfied by "initiative".
		if !regexp.MustCompile("`(?:mugicrew )?" + regexp.QuoteMeta(v) + "[ `]").MatchString(page) {
			t.Errorf("site/learn/cli-verbs.mdx lacks the usage verb %q", v)
		}
	}
}
