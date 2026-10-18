package bd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// reworkWorld fakes bd for ReworkApproval: list --all answers rows (the
// Approval's Drafts list answers drafts), create hands out c1, c2…, every other
// write is recorded; runBatch records the script.
func reworkWorld(t *testing.T, rows, drafts []map[string]any) (calls *[][]string, scripts *[]string) {
	t.Helper()
	calls, scripts = &[][]string{}, &[]string{}
	n := 0
	orun, obatch := run, runBatch
	t.Cleanup(func() { run, runBatch = orun, obatch })
	run = func(args ...string) (string, error) {
		switch args[0] {
		case "list":
			src := drafts
			if args[1] == "--all" {
				src = rows
			}
			b, _ := json.Marshal(src)
			return string(b), nil
		case "create":
			n++
			*calls = append(*calls, args)
			return "c" + strconv.Itoa(n), nil
		}
		*calls = append(*calls, args)
		return "", nil
	}
	runBatch = func(s string) error { *scripts = append(*scripts, s); return nil }
	return
}

func row(id, kind, status, parent string, extra map[string]any) map[string]any {
	m := map[string]any{"id": id, "title": "t " + id, "status": status, "labels": []string{"kind:" + kind}, "parent": parent}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func critFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func goodGraph() []map[string]any { return graphWith("in_progress") }

func graphWith(approvalStatus string) []map[string]any {
	return []map[string]any{row("cp", "checkpoint", "open", "", nil), row("ap", "approval", approvalStatus, "cp", nil)}
}

func TestReworkApprovalMalformedWritesNothing(t *testing.T) {
	calls, scripts := reworkWorld(t, goodGraph(), nil)
	ok := critFile(t, "# A\n\nbody\n")
	for name, files := range map[string][]string{
		"none":      nil,
		"no title":  {critFile(t, "body only\n")},
		"no body":   {critFile(t, "# A\n\n  \n")},
		"duplicate": {ok, critFile(t, "# A\n\nother\n")},
		"missing":   {filepath.Join(t.TempDir(), "nope.md")},
	} {
		if _, err := ReworkApproval("ap", files); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if len(*calls)+len(*scripts) != 0 {
		t.Errorf("wrote %v %v", *calls, *scripts)
	}
}

func TestReworkApprovalRefusals(t *testing.T) {
	f := []string{critFile(t, "# A\n\nbody\n")}
	for name, tc := range map[string]struct {
		rows []map[string]any
		want string
	}{
		"not an approval": {[]map[string]any{row("cp", "checkpoint", "open", "", nil), row("ap", "build", "open", "cp", nil)}, "not a kind:approval"},
		"orphan":          {[]map[string]any{row("ap", "approval", "open", "", nil)}, "not a child of a Checkpoint"},
		"closed":          {[]map[string]any{row("cp", "checkpoint", "open", "", nil), row("ap", "approval", "closed", "cp", map[string]any{"close_reason": "rework: x1"})}, "rework: x1"},
		"deferred":        {[]map[string]any{row("cp", "checkpoint", "open", "", nil), row("ap", "approval", "deferred", "cp", nil)}, "is deferred, not open or in_progress"},
		"blocked":         {[]map[string]any{row("cp", "checkpoint", "open", "", nil), row("ap", "approval", "blocked", "cp", nil)}, "is blocked, not open or in_progress"},
		"missing":         {nil, "no bead"},
	} {
		calls, scripts := reworkWorld(t, tc.rows, nil)
		_, err := ReworkApproval("ap", f)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
		if len(*calls)+len(*scripts) != 0 {
			t.Errorf("%s: wrote %v %v", name, *calls, *scripts)
		}
	}
}

func TestReworkApprovalAdoptsPrunesAndPublishes(t *testing.T) {
	drafts := []map[string]any{
		row("d1", "critique", "deferred", "cp", map[string]any{"title": "A"}),     // adopted
		row("d2", "critique", "deferred", "cp", map[string]any{"title": "Stray"}), // pruned
		row("d3", "signal", "deferred", "", map[string]any{"title": "Sig"}),       // rides along
	}
	calls, scripts := reworkWorld(t, goodGraph(), drafts)
	ids, err := ReworkApproval("ap", []string{critFile(t, "# A\n\nbody a\n"), critFile(t, "# B\n\nline1\nline2\n")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"d1", "c1"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if len(*calls) < 2 || !reflect.DeepEqual((*calls)[0], []string{"delete", "d2", "--force"}) {
		t.Fatalf("calls = %v: want the stray deleted, then one create", *calls)
	}
	create := strings.Join((*calls)[1], " ")
	for _, w := range []string{"--labels kind:critique,drafted-by:ap", "--status deferred", "--no-inherit-labels", "--parent cp", "--title B", "--description line1\nline2"} {
		if !strings.Contains(create, w) {
			t.Errorf("create %q lacks %q", create, w)
		}
	}
	s := strings.Join(*scripts, "")
	if len(*scripts) != 1 || !strings.Contains(s, `close ap "rework: d1, c1"`) ||
		!strings.Contains(s, "update d1 status=open") || !strings.Contains(s, "update d3 status=open") {
		t.Errorf("scripts = %q: want one batch opening the Critiques and the Signal Draft, closing the Approval", *scripts)
	}
}

// FR9: an open Approval is accepted as well as an in_progress one.
func TestReworkApprovalAcceptsOpenApproval(t *testing.T) {
	_, scripts := reworkWorld(t, graphWith("open"), nil)
	if _, err := ReworkApproval("ap", []string{critFile(t, "# A\n\nbody\n")}); err != nil {
		t.Fatal(err)
	}
	if len(*scripts) != 1 || !strings.Contains((*scripts)[0], `close ap "rework: c1"`) {
		t.Errorf("scripts = %q: want one batch closing the open Approval", *scripts)
	}
}
