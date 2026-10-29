package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
)

func TestDoctorBdActorWarnsWithoutDevice(t *testing.T) {
	doctorWS(t)
	doctorActor = func() (string, bool) { return "alice", false }
	out, code := doctorRun(t)
	if code != 0 {
		t.Errorf("a warn exited %d", code)
	}
	for _, w := range []string{"warn  bd actor", "~/.config/bd/config.yaml", "actor: alice@<device>"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
	if strings.Contains(out, "Safe — doctor fix runs") {
		t.Errorf("bare actor offered a safe fix: %q", out)
	}
}

func TestDoctorBdActorOKWithDevice(t *testing.T) {
	doctorWS(t)
	out, _ := doctorRun(t)
	if !strings.Contains(out, "ok  bd actor") || !strings.Contains(out, "alice@laptop") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorBdActorSkipsWhenUnreadable(t *testing.T) {
	doctorWS(t)
	doctorActor = func() (string, bool) { return "alice", true }
	out, code := doctorRun(t)
	if code != 0 || !strings.Contains(out, "skip  bd actor") {
		t.Errorf("code %d out %q", code, out)
	}
}

func bareClaims(ids ...string) func(string, string) ([]bd.Bead, error) {
	return func(string, string) ([]bd.Bead, error) {
		var bs []bd.Bead
		for _, id := range ids {
			bs = append(bs, bd.Bead{ID: id})
		}
		return bs, nil
	}
}

func stubBareClaims(t *testing.T, f func(string, string) ([]bd.Bead, error)) {
	old := doctorBareClaims
	t.Cleanup(func() { doctorBareClaims = old })
	doctorBareClaims = f
}

func TestDoctorBareClaimsOKWhenNone(t *testing.T) {
	doctorWS(t)
	stubBareClaims(t, bareClaims())
	out, _ := doctorRun(t)
	if !strings.Contains(out, "ok  bare-name claims") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorBareClaimsWarnsWithSafeReassign(t *testing.T) {
	doctorWS(t)
	var gotBare string
	stubBareClaims(t, func(_, bare string) ([]bd.Bead, error) { gotBare = bare; return bareClaims("x1", "x2")("", "") })
	out, code := doctorRun(t)
	if code != 0 || gotBare != "alice" {
		t.Errorf("code %d bare %q", code, gotBare)
	}
	for _, w := range []string{"warn  bare-name claims: 2 in-progress", "Safe — doctor fix runs:", "bd update x1 x2 --if-assignee alice --assignee alice@laptop"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
}

func TestDoctorBareClaimsSkipsWithoutDevice(t *testing.T) {
	doctorWS(t)
	doctorActor = func() (string, bool) { return "alice", false }
	stubBareClaims(t, func(string, string) ([]bd.Bead, error) { t.Error("listed without a Device"); return nil, nil })
	out, _ := doctorRun(t)
	if !strings.Contains(out, "skip  bare-name claims") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorBareClaimsSkipsWhenUnreadable(t *testing.T) {
	doctorWS(t)
	stubBareClaims(t, func(string, string) ([]bd.Bead, error) { return nil, errors.New("no graph\nmore") })
	out, _ := doctorRun(t)
	if !strings.Contains(out, "skip  bare-name claims: cannot list beads: no graph") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorFixReassignsBareClaimsThenOK(t *testing.T) {
	ran, _ := fixWS(t, "acme.atlassian.net")
	left := []string{"x1"}
	stubBareClaims(t, func(string, string) ([]bd.Bead, error) { return bareClaims(left...)("", "") })
	doctorExec = func(argv []string) (string, error) {
		*ran = append(*ran, argv)
		left = nil
		return "ok", nil
	}
	out, _ := fixRun(t)
	want := [][]string{{"bd", "update", "x1", "--if-assignee", "alice", "--assignee", "alice@laptop"}}
	if !reflect.DeepEqual(*ran, want) {
		t.Errorf("ran %v", *ran)
	}
	if !strings.Contains(out, "ok  bare-name claims") {
		t.Errorf("out %q", out)
	}
}

func TestDoctorBdActorEmptyUserPartSuggestsPlaceholder(t *testing.T) {
	doctorWS(t)
	doctorActor = func() (string, bool) { return "@laptop", false }
	out, _ := doctorRun(t)
	if !strings.Contains(out, "actor: <user>@<device>") || strings.Contains(out, "@laptop@") {
		t.Errorf("empty user part gave a malformed line:\n%s", out)
	}
}
