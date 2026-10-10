package contract

import (
	"regexp"
	"strings"
	"testing"
)

// The tail kinds' string values ARE the kind:<value> wire labels the slicing fork
// lays and the tail crewmates route on — a rename here silently breaks
// that contract, so it is pinned. KindBuild in particular is what bd.BuildChildren
// reads back from slicing.md's kind:build.
func TestTailKindWireValues(t *testing.T) {
	for kind, want := range map[Kind]string{
		KindBuild:    "build",
		KindReview:   "review",
		KindCritique: "critique",
		KindConflict: "conflict",
		KindApproval: "approval",
	} {
		if string(kind) != want {
			t.Errorf("tail kind = %q, want wire label %q", string(kind), want)
		}
	}
}

func TestNewSessionIDIsUUIDv4(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := NewSessionID(), NewSessionID()
	if !re.MatchString(a) || a == b {
		t.Errorf("NewSessionID = %q, %q; want distinct UUIDv4s", a, b)
	}
}

// The one Draft instruction every adopting prompt embeds (Spec D11) renders with
// the creator's bead id and carries each rule a prompt must follow.
func TestDraftInstruction(t *testing.T) {
	got := DraftInstruction("ws-spec")
	for _, want := range []string{
		"-s deferred",
		"drafted-by:ws-spec",
		"--no-inherit-labels",
		"before you exit",
		"before you close your own bead",
		"Never publish",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("DraftInstruction missing %q:\n%s", want, got)
		}
	}
	if DraftedBy("ws-spec") != "drafted-by:ws-spec" {
		t.Errorf("DraftedBy = %q", DraftedBy("ws-spec"))
	}
}

// The one HITL closing line (Spec D8) keeps /exit literal.
func TestClosingLine(t *testing.T) {
	bar := "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	want := "When you have closed your bead, print exactly these four lines and stop:\n" + bar + "\n" +
		"✅ **The work for this session is finished.** You may close this chat.\n" +
		"`/exit` or hit Ctrl+C twice to close.\n" + bar
	got := ClosingLine()
	if got != want {
		t.Errorf("ClosingLine = %q, want %q", got, want)
	}
	// Auto-exit arms on the phrase alone (Spec D1), so it must sit in the line.
	if ClosingPhrase != "The work for this session is finished." || !strings.Contains(got, ClosingPhrase) {
		t.Errorf("ClosingPhrase = %q, not in ClosingLine", ClosingPhrase)
	}
}

// Spec D1: StripKindPrefix removes every leading kind prefix, driven
// from Kinds so a new Kind const is covered without touching this test.
func TestStripKindPrefix(t *testing.T) {
	if len(Kinds) != 17 {
		t.Fatalf("len(Kinds) = %d, want 17", len(Kinds))
	}
	for _, k := range Kinds {
		if got := StripKindPrefix(string(k) + ": Foo misses bar"); got != "Foo misses bar" {
			t.Errorf("%s: got %q", k, got)
		}
	}
	for in, want := range map[string]string{
		"CRITIQUE: x":                         "x",
		"critique: x":                         "x",
		"  critique : x":                      "x",
		"Chart-conflict: x":                   "x",
		"Spec: Chart: foo":                    "foo",
		"Spec:":                               "Spec:",
		"Spec:  ":                             "Spec:  ",
		"Checkpoint mc-x conflicts with main": "Checkpoint mc-x conflicts with main",
		"Build failures on arm":               "Build failures on arm",
		"Fix Spec: wording":                   "Fix Spec: wording",
		"Chartless: x":                        "Chartless: x",
		" Unprefixed  title ":                 " Unprefixed  title ",
	} {
		if got := StripKindPrefix(in); got != want {
			t.Errorf("StripKindPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
