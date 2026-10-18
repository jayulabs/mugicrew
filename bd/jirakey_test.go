package bd

import (
	"testing"

	"jayulabs.org/mugicrew/contract"
)

func TestJiraKeyOfWalk(t *testing.T) {
	beads := map[string]Bead{
		"cp":  {ID: "cp", Kind: contract.KindCheckpoint, Parent: "ini"},
		"sp":  {ID: "sp"},
		"ch":  {ID: "ch"},
		"sig": {ID: "sig", ExternalRef: "jira-SIG-1"},
		"ini": {ID: "ini", Kind: contract.KindInitiative, ExternalRef: "jira-INI-9"},
		"gh":  {ID: "gh", ExternalRef: "gh-5"},
	}
	chain := map[string]string{"cp/spec": "sp", "sp/chart": "ch", "ch/signal": "sig"}
	for _, c := range []struct {
		name  string
		from  string
		edges map[string]string
		edit  func(map[string]Bead)
		want  string
	}{
		{"signal beats initiative", "cp", chain, nil, "SIG-1: "},
		{"initiative when signal has none", "cp", chain, func(m map[string]Bead) { m["sig"] = Bead{ID: "sig"} }, "INI-9: "},
		{"no ref", "cp", chain, func(m map[string]Bead) {
			m["sig"] = Bead{ID: "sig"}
			m["ini"] = Bead{ID: "ini", Kind: contract.KindInitiative}
		}, ""},
		{"non-jira ref ignored", "cp", chain, func(m map[string]Bead) { m["sig"] = m["gh"]; delete(m, "ini") }, ""},
		{"missing edge stops", "cp", map[string]string{"cp/spec": "sp"}, func(m map[string]Bead) { delete(m, "ini") }, ""},
		{"parent not an initiative", "cp", nil, func(m map[string]Bead) { m["ini"] = Bead{ID: "ini", ExternalRef: "jira-X-1"} }, ""},
		{"cycle terminates", "cp", map[string]string{"cp/spec": "sp", "sp/chart": "cp"}, func(m map[string]Bead) { delete(m, "ini") }, ""},
	} {
		m := map[string]Bead{}
		for k, v := range beads {
			m[k] = v
		}
		if c.edit != nil {
			c.edit(m)
		}
		causedBy := func(id string, k contract.Kind) (string, bool, error) {
			p, ok := c.edges[id+"/"+string(k)]
			return p, ok, nil
		}
		show := func(id string) (Bead, error) { return m[id], nil }
		got, err := jiraPrefixOf(m[c.from], causedBy, show)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}
