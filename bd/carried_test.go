package bd

import (
	"errors"
	"testing"
)

func TestCarriedApprovalBody(t *testing.T) {
	one := []Rename{{Path: "docs/adr/0395-x.md", Old: "0394", New: "0395"}}
	two := []Rename{
		{Path: "docs/adr/0395-x.md", Old: "0394", New: "0395"},
		{Path: "docs/adr/0396-y.md", Old: "0395", New: "0396"},
	}
	for _, tc := range []struct {
		name, patch string
		renames     []Rename
		want        string
	}{
		{"checkpoint one", "", one, "Approval carried from Approval Old title: renumber-only.\nCommit: newhead\n\n### Approval History\n\ncarried (renumber-only from oldsha: 0394 → 0395)\n"},
		{"chart two", "abc123", two, "Approval carried from Approval Old title: renumber-only.\nCommit: newhead\nPatch-id: abc123\n\n### Approval History\n\ncarried (renumber-only from oldsha: 0394 → 0395, 0395 → 0396)\n"},
	} {
		got := CarriedApprovalBody("Old title", "newhead", tc.patch, "oldsha", tc.renames)
		if got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
		if !IsCarriedApproval(got) {
			t.Errorf("%s: built body not recognised", tc.name)
		}
	}
}

func TestIsCarriedApprovalFalseForOthers(t *testing.T) {
	human := "Commit: x\n### Approval History\n\napproved by human\n"
	afk := "Commit: x\n### Approval History\n\nauto-approved (afk)\n"
	for _, b := range []string{human, afk, "", "carried (renumber-only. in prose"} {
		if IsCarriedApproval(b) {
			t.Errorf("IsCarriedApproval(%q) = true", b)
		}
	}
}

func TestApprovalOrigin(t *testing.T) {
	carried := func(id, by string) Bead {
		return Bead{ID: id, CausedBy: by, Description: CarriedApprovalBody("t", "h", "", "s", []Rename{{"p", "0001", "0002"}})}
	}
	root := Bead{ID: "root", Description: "Commit: x\n### Approval History\n\napproved\n"}
	all := map[string]Bead{"root": root, "c1": carried("c1", "root"), "c2": carried("c2", "c1")}
	get := func(id string) (Bead, error) {
		b, ok := all[id]
		if !ok {
			return Bead{}, errors.New("missing")
		}
		return b, nil
	}
	if o, err := ApprovalOrigin(all["c2"], get); err != nil || o.ID != "root" {
		t.Errorf("two carries: origin %q, %v", o.ID, err)
	}
	if o, _ := ApprovalOrigin(root, get); o.ID != "root" {
		t.Errorf("non-carried: origin %q", o.ID)
	}
	if _, err := ApprovalOrigin(carried("c3", "gone"), get); err == nil {
		t.Error("broken chain: want error")
	}
	if _, err := ApprovalOrigin(carried("c4", ""), get); err == nil {
		t.Error("no caused-by: want error")
	}
}

func TestNotCarriedNote(t *testing.T) {
	if got, want := NotCarriedNote("slug changed", "docs/adr/0395-x.md"), "not carried: slug changed (docs/adr/0395-x.md)"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
