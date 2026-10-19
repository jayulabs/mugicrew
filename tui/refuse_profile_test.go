package tui

import (
	"errors"
	"testing"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// A launch or resume refusal logs profile.unusable with the winning file as path
// when the error carries one, and none for an undeclared name (ADR 0306 §4).
func TestRefuseProfilePathAttr(t *testing.T) {
	b := bd.Bead{ID: "mc-b"}
	for _, launch := range []string{"hitl", "resume"} {
		capt := eventlogtest.Capture(t)
		err := RefuseProfile(b, "p", launch, &agentprofile.FileError{Path: "/w/c.toml", Err: errors.New("bad (from c.toml)")})
		if err == nil || err.Error() != "profile p: bad (from c.toml)" {
			t.Errorf("%s flash = %v", launch, err)
		}
		if a := (*capt)[0].Attrs; a["path"] != "/w/c.toml" || a["reason"] != "bad (from c.toml)" || a["launch"] != launch {
			t.Errorf("%s attrs = %v", launch, a)
		}
		capt = eventlogtest.Capture(t)
		RefuseProfile(b, "p", launch, errors.New("not declared"))
		if a := (*capt)[0].Attrs; a["path"] != nil || a["reason"] != "not declared" {
			t.Errorf("%s undeclared attrs = %v", launch, a)
		}
	}
}
