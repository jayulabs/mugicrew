package bd

import (
	"fmt"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

func TestOffshootPlacement(t *testing.T) {
	beads := map[string]Bead{
		"cp":     {ID: "cp", Kind: contract.KindCheckpoint, Title: "CP", Parent: "init"},
		"cpbare": {ID: "cpbare", Kind: contract.KindChart, Title: "Bare"},
		"cpold":  {ID: "cpold", Kind: contract.KindCheckpoint, Title: "Old", Parent: "done"},
		"init":   {ID: "init", Kind: contract.KindInitiative, Title: "Init", Status: "open"},
		"done":   {ID: "done", Kind: contract.KindInitiative, Title: "Done", Status: "closed"},
	}
	show := func(id string) (Bead, error) {
		b, ok := beads[id]
		if !ok {
			return Bead{}, fmt.Errorf("no %s", id)
		}
		return b, nil
	}
	for _, tc := range []struct {
		name                         string
		cause                        Bead
		parent, line, container, ini string
	}{
		{"checkpoint under initiative", Bead{ID: "c", Kind: contract.KindBuild, Title: "T", Parent: "cp"},
			"init", "Initiative: Init", "cp", "init"},
		{"unclosed member errand is its own container", Bead{ID: "e", Kind: contract.KindErrand, Title: "E", Status: "open", Parent: "init"},
			"init", "Initiative: Init", "e", "init"},
		{"discussion under initiative takes none", Bead{ID: "d", Kind: contract.KindDiscussion, Title: "D", Parent: "init"},
			"", "Initiative: outside (D has no Initiative)", "", ""},
		{"closed initiative", Bead{ID: "c", Kind: contract.KindBuild, Title: "T", Parent: "cpold"},
			"", "Initiative: outside (Done is closed)", "cpold", "done"},
		{"container without initiative", Bead{ID: "c", Kind: contract.KindBuild, Title: "T", Parent: "cpbare"},
			"", "Initiative: outside (Bare has no Initiative)", "cpbare", ""},
		{"no parent", Bead{ID: "c", Kind: contract.KindBuild, Title: "T"},
			"", "Initiative: outside (T has no Initiative)", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent, line, container, init, err := OffshootPlacement(tc.cause, show)
			if err != nil {
				t.Fatal(err)
			}
			if parent != tc.parent || line != tc.line || container.ID != tc.container || init.ID != tc.ini {
				t.Errorf("got (%q, %q, %q, %q), want (%q, %q, %q, %q)", parent, line, container.ID, init.ID,
					tc.parent, tc.line, tc.container, tc.ini)
			}
		})
	}
	if _, _, _, _, err := OffshootPlacement(Bead{ID: "c", Parent: "missing"}, show); err == nil {
		t.Error("want an error when the parent can't be read")
	}
}
