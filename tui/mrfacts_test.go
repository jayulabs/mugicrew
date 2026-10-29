package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"jayulabs.org/mugicrew/contract"
)

const (
	head40 = "0123456789abcdef0123456789abcdef01234567"
	old40  = "fedcba9876543210fedcba9876543210fedcba98"
)

// factLines is the four MR Watch lines of a green MR, each overridable by key.
func factLines(over map[string]string) string {
	f := map[string]string{
		"MR head:":         head40,
		"MR pipeline:":     "success 77 " + head40,
		"MR threads:":      "0/2",
		"MR merge status:": "can_be_merged",
	}
	var b strings.Builder
	b.WriteString("MR: !7\n")
	for _, k := range []string{"MR head:", "MR pipeline:", "MR threads:", "MR merge status:"} {
		v, ok := f[k]
		if o, set := over[k]; set {
			v, ok = o, o != "<missing>"
		}
		if ok {
			b.WriteString(k + " " + v + "\n")
		}
	}
	return b.String()
}

// greenGraph is mrGraph with the Execute Watch w1 and Defog Watch w3 carrying lines.
func greenGraph(status string, over map[string]string) graphData {
	g := mrGraph(status)
	g.landEv = map[string]string{}
	g.beadByID("w1").Description = factLines(over)
	g.beadByID("w3").Description = factLines(over)
	return g
}

func TestMRRowAllGreen(t *testing.T) {
	for _, status := range []string{"not_approved", "need_rebase", "draft_status", "mergeable", ""} {
		e, d := mrWords(t, greenGraph(status, nil))
		if !strings.HasSuffix(e, "· all green") || !strings.HasSuffix(d, "· all green") {
			t.Errorf("status %q: %q / %q", status, e, d)
		}
	}
}

func TestMRRowNotAllGreenWhenAGuardFails(t *testing.T) {
	cases := map[string]map[string]string{
		"pipeline failed":   {"MR pipeline:": "failed 77 " + head40},
		"pipeline running":  {"MR pipeline:": "running 77 " + head40},
		"ran on old head":   {"MR pipeline:": "success 77 " + old40},
		"no pipeline":       {"MR pipeline:": "none"},
		"train pipeline":    {"MR pipeline:": "success 77 -"},
		"open threads":      {"MR threads:": "1/2"},
		"unparseable":       {"MR threads:": "x"},
		"cannot be merged":  {"MR merge status:": "cannot_be_merged"},
		"unchecked":         {"MR merge status:": "unchecked"},
		"checking":          {"MR merge status:": "checking"},
		"recheck":           {"MR merge status:": "cannot_be_merged_recheck"},
		"head missing":      {"MR head:": "<missing>"},
		"pipeline missing":  {"MR pipeline:": "<missing>"},
		"threads missing":   {"MR threads:": "<missing>"},
		"merge status gone": {"MR merge status:": "<missing>"},
	}
	for name, over := range cases {
		e, d := mrWords(t, greenGraph("mergeable", over))
		if strings.Contains(e, "all green") || strings.Contains(d, "all green") {
			t.Errorf("%s: %q / %q", name, e, d)
		}
		if !strings.HasSuffix(e, "awaiting merge") { // falls through to today's word
			t.Errorf("%s: Execute %q lost the Landing word", name, e)
		}
	}
}

func TestMRRowWaterfall(t *testing.T) {
	g := greenGraph("mergeable", nil)
	g.beads = append(g.beads,
		dbead("k1", contract.KindBuild, "open", "cp", 9), dbead("k2", contract.KindBuild, "open", "ch", 9))
	if e, d := mrWords(t, g); !strings.HasSuffix(e, "· stabilizing") || !strings.HasSuffix(d, "· stabilizing") {
		t.Errorf("stabilizing must outrank all green: %q / %q", e, d)
	}
	g = greenGraph("mergeable", nil)
	g.beads[0].Status = "closed"
	g.beadByID("w1").Description = factLines(nil) + "Close-outs due: 1\n"
	if e, _ := mrWords(t, g); !strings.HasSuffix(e, "closing out · 1 due") || strings.Contains(e, "all green") {
		t.Errorf("outliving Watch %q", e)
	}
	g = greenGraph("mergeable", map[string]string{"MR threads:": "1/1"})
	g.beadByID("w1").Description = "MR: !7\n"
	if e, _ := mrWords(t, g); !strings.HasSuffix(e, "awaiting merge") {
		t.Errorf("no lines: %q", e)
	}
}

func detailOf(t *testing.T, g graphData) (exec, defog string) {
	t.Helper()
	for _, r := range g.execList(map[string]bool{}) {
		if r.kind == eMR && r.b.ID == "w1" {
			exec = ansi.Strip((execTab{g: g}).rowText(r))
		}
	}
	for _, r := range g.defogList(map[string]bool{}) {
		if r.kind == dMR && r.b.ID == "w3" {
			defog = ansi.Strip((defogTab{g: g}).rowText(r))
		}
	}
	return
}

func TestMRDetailShowsFacts(t *testing.T) {
	e, d := detailOf(t, greenGraph("mergeable", nil))
	for _, got := range []string{e, d} {
		for _, want := range []string{"pipeline", "success #77 at 01234567 · head 01234567", "threads", "0 open of 2", "merge status", "can_be_merged"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
	}
	e, _ = detailOf(t, greenGraph("mergeable", map[string]string{
		"MR pipeline:": "failed 78 " + old40, "MR merge status:": "cannot_be_merged"}))
	if !strings.Contains(e, "failed #78 at fedcba98 · head 01234567") || !strings.Contains(e, "cannot_be_merged (conflict)") {
		t.Errorf("detail:\n%s", e)
	}
	e, _ = detailOf(t, greenGraph("mergeable", map[string]string{"MR pipeline:": "none"}))
	if !strings.Contains(e, "none") {
		t.Errorf("none pipeline:\n%s", e)
	}
	e, _ = detailOf(t, greenGraph("mergeable", map[string]string{"MR pipeline:": "success 5 -"}))
	if !strings.Contains(e, "success #5 at train") && !strings.Contains(e, "train") {
		t.Errorf("train pipeline:\n%s", e)
	}
	e, _ = detailOf(t, greenGraph("mergeable", map[string]string{
		"MR pipeline:": "<missing>", "MR threads:": "<missing>", "MR merge status:": "<missing>"}))
	for _, k := range []string{"pipeline", "threads", "merge status"} {
		if strings.Contains(e, k) {
			t.Errorf("%s field shown with its line missing:\n%s", k, e)
		}
	}
}
