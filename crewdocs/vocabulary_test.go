package crewdocs

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// vocabulary.md is a verbatim subset of ../GLOSSARY.md (ADR 0202): its entries
// are the term list, and each must equal the glossary's entry after stripEntry.
// Admit a term only if (a) crew docs use it in its mugicrew sense, (b) nothing defines it inline, (c) it is misreadable.

var (
	adrCite   = regexp.MustCompile(` \(ADR \d{4}(?:, ADR \d{4})*\)`)
	adrResid  = regexp.MustCompile(`ADR \d{4}`)
	entryHead = regexp.MustCompile(`^- \*\*([^*]+)\*\* — `)
)

// entries splits md into its "- **Term** — …" list items (with indented
// continuation lines), keyed by term, in file order.
func entries(md string) (terms []string, byTerm map[string]string) {
	byTerm = map[string]string{}
	var cur string
	var lines []string
	flush := func() {
		if cur != "" {
			if _, dup := byTerm[cur]; !dup {
				terms = append(terms, cur)
				byTerm[cur] = strings.Join(lines, "\n")
			}
		}
		cur, lines = "", nil
	}
	for _, l := range strings.Split(md, "\n") {
		if m := entryHead.FindStringSubmatch(l); m != nil {
			flush()
			cur, lines = m[1], []string{l}
		} else if cur != "" && strings.HasPrefix(l, "  ") {
			lines = append(lines, l)
		} else {
			flush()
		}
	}
	flush()
	return
}

// stripEntry applies the strip rule: drop (ADR NNNN, …) parentheticals with
// their leading space, and any _History_ line with its continuation lines (those
// indented deeper than 2 spaces). A residual ADR citation is an error.
func stripEntry(e string) (string, bool) {
	var out []string
	skip := false
	for _, l := range strings.Split(e, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "_History_") {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(l, "   ") {
			continue
		}
		skip = false
		out = append(out, l)
	}
	s := adrCite.ReplaceAllString(strings.Join(out, "\n"), "")
	return s, !adrResid.MatchString(s)
}

func TestStripEntry(t *testing.T) {
	in := "- **X** — a thing (ADR 0102, ADR 0147).\n  more text.\n  _History_: was Y (ADR 0001),\n    renamed later.\n  _Avoid:_ z."
	want := "- **X** — a thing.\n  more text.\n  _Avoid:_ z."
	got, ok := stripEntry(in)
	if !ok || got != want {
		t.Fatalf("got %q ok=%v, want %q", got, ok, want)
	}
	if _, ok := stripEntry("- **X** — see ADR 0102 here."); ok {
		t.Fatal("residual ADR citation not rejected")
	}
}

// checkVocabulary compares vocab's entries to glossary's, returning failures.
func checkVocabulary(vocab, glossary string) (errs []string) {
	terms, have := entries(vocab)
	_, src := entries(glossary)
	for _, term := range terms {
		g, ok := src[term]
		if !ok {
			errs = append(errs, term+": no GLOSSARY.md entry")
			continue
		}
		want, clean := stripEntry(g)
		if !clean {
			errs = append(errs, term+": stripped entry still cites an ADR; reword GLOSSARY.md:\n"+want)
		} else if have[term] != want {
			errs = append(errs, term+": differs from GLOSSARY.md; expected:\n"+want)
		}
	}
	return
}

func TestCheckVocabulary(t *testing.T) {
	gl := "- **A** — alpha (ADR 0001).\n  _Avoid:_ x.\n- **B** — beta ADR 0002.\n"
	if errs := checkVocabulary("- **A** — alpha.\n  _Avoid:_ x.\n", gl); len(errs) != 0 {
		t.Fatalf("match failed: %v", errs)
	}
	for name, vocab := range map[string]string{
		"off by a word": "- **A** — alpha!\n  _Avoid:_ x.\n",
		"absent term":   "- **C** — gamma.\n",
		"residual ADR":  "- **B** — beta ADR 0002.\n",
	} {
		if errs := checkVocabulary(vocab, gl); len(errs) != 1 {
			t.Errorf("%s: got %v, want one failure", name, errs)
		}
	}
}

func TestVocabularyMatchesGlossary(t *testing.T) {
	g, err := os.ReadFile("../GLOSSARY.md")
	if errors.Is(err, fs.ErrNotExist) {
		t.Skip("GLOSSARY.md is excluded from the nix build source")
	} else if err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadFile(docs, "vocabulary.md")
	if err != nil {
		t.Fatal(err)
	}
	vocab := string(b)
	if terms, _ := entries(vocab); len(terms) == 0 {
		t.Fatal("vocabulary.md has no parseable entries")
	}
	for _, e := range checkVocabulary(vocab, string(g)) {
		t.Error(e)
	}
}
