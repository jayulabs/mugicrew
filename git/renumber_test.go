package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	adrOld  = "docs/adr/0394-feature.md"
	adrNew  = "docs/adr/0395-feature.md"
	adrMain = "docs/adr/0394-other.md"
)

func commitAll(t *testing.T, msg string) string {
	t.Helper()
	mustGit(t, "add", "-A")
	mustGit(t, "commit", "-m", msg)
	return mustGit(t, "rev-parse", "HEAD")
}

func edit(t *testing.T, name string, fn func(string) string) {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	out := fn(string(b))
	if out == string(b) {
		t.Fatalf("edit of %s changed nothing", name)
	}
	writeFile(t, name, out)
}

func replaceOnce(old, new string) func(string) string {
	return func(s string) string { return strings.Replace(s, old, new, 1) }
}

func writeAll(t *testing.T, files map[string]string) {
	t.Helper()
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, name, body)
	}
}

// lap is a repo holding a branch that adds ADR 0394 (and edits files the mainline
// also edits), the approved pin, and a mainline that moved on and took 0394.
type lap struct {
	pin, mainline string
	conflict      bool
}

const (
	lapGlossary = "# Glossary\n\nalpha\nbeta\ngamma\n"
	lapBdGo     = "package bd\n\nfunc a() {\n\tone()\n}\n\nfunc b() {\n\ttwo()\n}\n"
)

func newLap(t *testing.T, conflict bool) lap {
	t.Helper()
	newRepo(t)
	writeAll(t, map[string]string{
		"GLOSSARY.md":        lapGlossary,
		"bd.go":              lapBdGo,
		"untouched.md":       "nobody edits this\n",
		"docs/adr/0001-x.md": "# ADR 0001\n",
	})
	commitAll(t, "base")
	mustGit(t, "checkout", "-q", "-b", "feature")
	writeAll(t, map[string]string{
		adrOld:       "# ADR 0394\n\nSee ADR 0001 and ADR 0394 itself.\n",
		"feature.go": "package bd\n\n// ADR 0394\nconst limit = 394\n\nfunc f() {\n\tx()\n\ty()\n}\n",
	})
	edit(t, "GLOSSARY.md", replaceOnce("beta\n", "beta\nsee ADR 0394\n"))
	edit(t, "bd.go", replaceOnce("\ttwo()\n", "\ttwo()\n\tthree() // ADR 0394\n"))
	pin := commitAll(t, "feature")
	mustGit(t, "checkout", "-q", "main")
	writeAll(t, map[string]string{adrMain: "# ADR 0394\n\nmainline's own.\n"})
	edit(t, "GLOSSARY.md", replaceOnce("# Glossary\n", "# Glossary\n\nnew top entry\nanother\n")) // shifts @@
	edit(t, "bd.go", replaceOnce("\tone()\n", "\tone()\n\tuno()\n"))
	if conflict {
		edit(t, "GLOSSARY.md", replaceOnce("beta\n", "beta\nmainline also edits here\n"))
	}
	return lap{pin: pin, mainline: commitAll(t, "mainline moves on"), conflict: conflict}
}

// head builds a new head of the lap: the pin replayed onto the mainline, renamed
// 0394 → 0395, then mut for the variation under test.
func (l lap) head(t *testing.T, mut func()) string {
	t.Helper()
	return l.headFrom(t, l.mainline, l.pin, "0394", "0395", mut)
}

func (l lap) headFrom(t *testing.T, onto, pin, from, to string, mut func()) string {
	t.Helper()
	mustGit(t, "checkout", "-q", "--detach", onto)
	if _, err := run("cherry-pick", "--no-commit", pin); err != nil {
		mustGit(t, "checkout", "--theirs", "--", "GLOSSARY.md") // a hand resolution
		mustGit(t, "add", "GLOSSARY.md")
	}
	oldPath := strings.ReplaceAll(adrOld, "0394", from)
	newPath := strings.ReplaceAll(adrNew, "0395", to)
	mustGit(t, "mv", oldPath, newPath)
	for _, f := range []string{newPath, "feature.go", "GLOSSARY.md", "bd.go"} {
		edit(t, f, func(s string) string { return strings.ReplaceAll(s, from, to) })
	}
	if mut != nil {
		mut()
	}
	sha := commitAll(t, "head")
	mustGit(t, "checkout", "-q", "main")
	return sha
}

func verify(t *testing.T, pin, head, mainline string) RenumberVerdict {
	t.Helper()
	v, err := Repo("").VerifyRenumber([]Rule{ADRRule}, pin, head, mainline)
	if err != nil {
		t.Fatalf("VerifyRenumber: %v", err)
	}
	return v
}

func wantReject(t *testing.T, v RenumberVerdict, point, file string) {
	t.Helper()
	if v.Accept || v.Point != point || (file != "" && v.File != file) {
		t.Errorf("verdict = %+v, want reject %s at %q", v, point, file)
	}
}

// A renumber with base drift in files both sides edit (an @@ shift) is accepted.
func TestVerifyRenumberAcceptsADriftedLap(t *testing.T) {
	l := newLap(t, false)
	h := l.head(t, nil)
	v := verify(t, l.pin, h, "main")
	if !v.Accept || len(v.Renames) != 1 {
		t.Fatalf("verdict = %+v, want accept with one rename", v)
	}
	if got := v.Renames[0]; got != (Renamed{Path: adrNew, OldPath: adrOld, Old: "0394", New: "0395"}) {
		t.Errorf("rename = %+v", got)
	}
}

// Two laps in a row both carry: the first new head is the second lap's pin.
func TestVerifyRenumberAcceptsATwoStepChain(t *testing.T) {
	l := newLap(t, false)
	h1 := l.head(t, nil)
	mustGit(t, "checkout", "-q", "main")
	writeAll(t, map[string]string{"docs/adr/0395-yet-another.md": "# ADR 0395\n\nanother lander.\n"})
	edit(t, "bd.go", replaceOnce("\tone()\n", "\tone()\n\tdos()\n"))
	main2 := commitAll(t, "another container landed 0395")
	h2 := l.headFrom(t, main2, h1, "0395", "0396", nil)
	v := verify(t, h1, h2, "main")
	if !v.Accept {
		t.Fatalf("verdict = %+v, want accept", v)
	}
}

func TestVerifyRenumberRejects(t *testing.T) {
	cases := []struct {
		name, point, file string
		mut               func(t *testing.T)
	}{
		{"content edit in a branch file", PointCompare, "feature.go", func(t *testing.T) {
			edit(t, "feature.go", replaceOnce("\tx()\n", "\tx()\n\tsmuggled()\n"))
		}},
		{"edit in an untouched shared file", PointOwnLines, "untouched.md", func(t *testing.T) {
			edit(t, "untouched.md", replaceOnce("nobody", "somebody"))
		}},
		{"edit in a both-edited shared file", PointOwnLines, "bd.go", func(t *testing.T) {
			edit(t, "bd.go", replaceOnce("\tuno()\n", "\tuno2()\n"))
		}},
		{"changed reference to another number", PointCompare, adrNew, func(t *testing.T) {
			edit(t, adrNew, replaceOnce("ADR 0001", "ADR 0002"))
		}},
		{"reference retargeted at the new number", PointCompare, adrNew, func(t *testing.T) {
			edit(t, adrNew, replaceOnce("ADR 0001", "ADR 0395"))
		}},
		{"slug change", PointPair, adrOld, func(t *testing.T) {
			mustGit(t, "mv", adrNew, "docs/adr/0395-feature-renamed.md")
		}},
		{"directory change", PointPair, adrOld, func(t *testing.T) {
			writeAll(t, map[string]string{"sub/docs/adr/x": ""})
			mustGit(t, "mv", adrNew, "sub/docs/adr/0395-feature.md")
		}},
		{"added line moved within its function", PointCompare, "feature.go", func(t *testing.T) {
			edit(t, "feature.go", replaceOnce("\tx()\n\ty()\n", "\ty()\n\tx()\n"))
		}},
		{"sweep rewriting mainline's file of the old number", PointOwnLines, adrMain, func(t *testing.T) {
			edit(t, adrMain, replaceOnce("# ADR 0394", "# ADR 0395"))
		}},
		{"missed title", PointSweep, adrNew, func(t *testing.T) {
			edit(t, adrNew, replaceOnce("# ADR 0395", "# ADR 0394"))
		}},
		{"unpadded 394 → 395", PointCompare, "feature.go", func(t *testing.T) {
			edit(t, "feature.go", replaceOnce("limit = 394", "limit = 395"))
		}},
		{"a dropped numbered file", PointPair, adrOld, func(t *testing.T) {
			mustGit(t, "rm", "-qf", adrNew)
		}},
		{"an added numbered file", PointPair, "docs/adr/0396-extra.md", func(t *testing.T) {
			writeAll(t, map[string]string{"docs/adr/0396-extra.md": "# ADR 0396\n"})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := newLap(t, false)
			h := l.head(t, func() { c.mut(t) })
			wantReject(t, verify(t, l.pin, h, "main"), c.point, c.file)
		})
	}
}

// A head that pairs with no renames is a rebase, not a renumber lap.
func TestVerifyRenumberRejectsAHeadWithNoRenames(t *testing.T) {
	l := newLap(t, false)
	mustGit(t, "checkout", "-q", "--detach", "main")
	mustGit(t, "cherry-pick", "--no-commit", l.pin)
	h := commitAll(t, "rebased only")
	wantReject(t, verify(t, l.pin, h, "main"), PointPair, "")
}

// A hand-resolved rebase (mainline edits the line the branch edits) does not
// replay clean, so it is a reject however tidy the resolution.
func TestVerifyRenumberRejectsAReplayConflict(t *testing.T) {
	l := newLap(t, true)
	h := l.head(t, nil)
	wantReject(t, verify(t, l.pin, h, "main"), PointReplay, "GLOSSARY.md")
}

// A git failure is an error for the caller to retry, never a verdict.
func TestVerifyRenumberGitFailureIsAnError(t *testing.T) {
	l := newLap(t, false)
	h := l.head(t, nil)
	v, err := Repo("").VerifyRenumber([]Rule{ADRRule}, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", h, "main")
	if err == nil {
		t.Fatalf("verdict = %+v, want an error", v)
	}
	if v.Accept || v.Point != "" {
		t.Errorf("an error carried a verdict: %+v", v)
	}
}
