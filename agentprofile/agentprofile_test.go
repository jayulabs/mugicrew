package agentprofile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const none = "\x00" // no file at all

// write writes body to dir/agent-profiles.toml unless body is none.
func write(t *testing.T, dir, body string) {
	t.Helper()
	if body == none {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-profiles.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const sharedOpus = `
[profiles.opus]
harness = "claudecode"
model = "opus"
`

const sharedDeepseek = `
[profiles.deepseek]
harness = "claudecode"
model = "deepseek-chat"
env = { ANTHROPIC_BASE_URL = "https://api.deepseek.com/anthropic", ANTHROPIC_AUTH_TOKEN = "${DEEPSEEK_API_KEY}" }
`

// ReadStrict is the OE catalog's whole-file rule: a malformed file, a profile
// without harness or any unknown key drops the file; a missing file wraps
// fs.ErrNotExist.
func TestReadStrict(t *testing.T) {
	opus := Profile{Name: "opus", Harness: "claudecode", Model: "opus"}
	for _, tc := range []struct {
		name, body string
		want       map[string]Profile
		bad        bool
	}{
		{"profiles", sharedOpus, map[string]Profile{"opus": opus}, false},
		{"any harness name accepted", "[profiles.pi]\nharness = \"pi\"\n", map[string]Profile{"pi": {Name: "pi", Harness: "pi"}}, false},
		{"malformed", "[profiles.opus\nharness = 1", nil, true},
		{"missing harness", sharedOpus + "[profiles.bare]\nmodel = \"x\"\n", nil, true},
		{"unknown profile key", "[profiles.opus]\nharness = \"claudecode\"\nmodle = \"opus\"\n", nil, true},
		{"unknown top-level key", sharedOpus + "[kind_profiles]\nbuild = \"opus\"\n", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, tc.body)
			l, err := ReadStrict(filepath.Join(dir, "agent-profiles.toml"))
			if (err != nil) != tc.bad || !reflect.DeepEqual(l.Profiles, tc.want) {
				t.Errorf("ReadStrict = %+v, %v; want %v (bad=%v)", l.Profiles, err, tc.want, tc.bad)
			}
		})
	}
	if _, err := ReadStrict(filepath.Join(t.TempDir(), "agent-profiles.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file err = %v, want fs.ErrNotExist", err)
	}
}

// ReadTables reads [profiles.*] in any TOML file, each table checked alone.
func TestReadTables(t *testing.T) {
	good := Profile{Name: "good", Harness: "claudecode", Model: "opus", Env: map[string]string{"K": "${V}"}}
	goodTable := "[profiles.good]\nharness = \"claudecode\"\nmodel = \"opus\"\nenv = { K = \"${V}\" }\n"
	for _, tc := range []struct {
		name, body string
		wantBad    map[string]string // name → error substring
		wantGood   bool
		fileErr    error // errors.Is target; nil = none
		syntax     bool
	}{
		{name: "good table", body: goodTable, wantGood: true},
		{name: "unrelated keys ignored", body: "concurrency = 2\nauto_exit = \"1m\"\n[kind_profiles]\nbuild = \"good\"\n[context.a]\npath = \"a\"\n" + goodTable, wantGood: true},
		{name: "no profiles", body: "concurrency = 2\n"},
		{name: "unknown key", body: goodTable + "[profiles.x]\nharness = \"claudecode\"\nmodle = \"opus\"\n", wantGood: true, wantBad: map[string]string{"x": "unknown key modle"}},
		{name: "no harness", body: goodTable + "[profiles.x]\nmodel = \"m\"\n", wantGood: true, wantBad: map[string]string{"x": "no harness"}},
		{name: "model not a string", body: goodTable + "[profiles.x]\nharness = \"claudecode\"\nmodel = 3\n", wantGood: true, wantBad: map[string]string{"x": "model"}},
		{name: "non-string env", body: goodTable + "[profiles.x]\nharness = \"claudecode\"\nenv = { K = 1 }\n", wantGood: true, wantBad: map[string]string{"x": "incompatible"}},
		{name: "non-table profile", body: "[profiles]\nx = 3\n" + goodTable, wantGood: true, wantBad: map[string]string{"x": ""}},
		{name: "non-table profiles", body: "profiles = 3\n", fileErr: ErrProfilesNotTable},
		{name: "syntax error", body: "[profiles.good\n", syntax: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := filepath.Join(t.TempDir(), "config.toml")
			os.WriteFile(f, []byte(tc.body), 0o644)
			l, err := ReadTables(f)
			switch {
			case tc.syntax:
				if err == nil || errors.Is(err, ErrProfilesNotTable) {
					t.Fatalf("err = %v, want a syntax error", err)
				}
				return
			case tc.fileErr != nil:
				if !errors.Is(err, tc.fileErr) || len(l.Profiles)+len(l.Bad) != 0 {
					t.Fatalf("= %+v, %v; want file error %v and no names", l, err, tc.fileErr)
				}
				return
			case err != nil:
				t.Fatal(err)
			}
			if tc.wantGood != (len(l.Profiles) == 1 && reflect.DeepEqual(l.Profiles["good"], good)) || (!tc.wantGood && len(l.Profiles) != 0) {
				t.Errorf("Profiles = %+v", l.Profiles)
			}
			if len(l.Bad) != len(tc.wantBad) {
				t.Errorf("Bad = %v, want %v", l.Bad, tc.wantBad)
			}
			for name, sub := range tc.wantBad {
				if l.Bad[name] == nil || !strings.Contains(l.Bad[name].Error(), sub) {
					t.Errorf("Bad[%s] = %v, want containing %q", name, l.Bad[name], sub)
				}
			}
		})
	}
	if _, err := ReadTables(filepath.Join(t.TempDir(), "none.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file err = %v, want fs.ErrNotExist", err)
	}
}

// Merge is first-wins over ordered layers, whole-table, with a bad table
// shadowing a good lower one.
func TestMerge(t *testing.T) {
	hi := Layer{Path: "hi", Profiles: map[string]Profile{"a": {Name: "a", Harness: "h1"}}, Bad: map[string]error{"b": errors.New("boom")}}
	lo := Layer{Path: "lo", Profiles: map[string]Profile{"a": {Name: "a", Harness: "h2", Model: "m"}, "b": {Name: "b", Harness: "h2"}, "c": {Name: "c", Harness: "h2"}}}
	got := Merge(hi, lo)
	if e := got["a"]; e.Path != "hi" || e.Profile.Harness != "h1" || e.Profile.Model != "" || e.Err != nil {
		t.Errorf("a = %+v, want hi's table whole", e)
	}
	if e := got["b"]; e.Path != "hi" || e.Err == nil || e.Profile.Name != "b" {
		t.Errorf("b = %+v, want hi's bad table shadowing lo", e)
	}
	if e := got["c"]; e.Path != "lo" || e.Err != nil {
		t.Errorf("c = %+v, want lo's", e)
	}
}

// The OE pair lives at $OE_ROOT when set, else the Workspace root; an
// unresolvable common dir ("") is no local layer.
func TestOEPaths(t *testing.T) {
	t.Setenv("OE_ROOT", "/oe")
	if s, l := OEPaths("/ws", "/common"); s != "/oe/agent-profiles.toml" || l != "/common/agent-profiles.toml" {
		t.Errorf("with $OE_ROOT = %q, %q", s, l)
	}
	t.Setenv("OE_ROOT", "")
	if s, l := OEPaths("/ws", ""); s != "/ws/agent-profiles.toml" || l != "" {
		t.Errorf("without $OE_ROOT = %q, %q", s, l)
	}
}
