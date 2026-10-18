package agentprofile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"jayulabs.org/mugicrew/eventlog/eventlogtest"
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

// Load layers the machine-local catalog over the checked-in one (root ADR 0093):
// whole-profile replacement, no field or env merge; an unusable layer (malformed
// TOML, a profile without harness, an unknown key) is dropped whole and logs
// agent-profiles.unusable {file, reason}; a missing file is silent.
func TestLoad(t *testing.T) {
	deepseek := Profile{Name: "deepseek", Harness: "claudecode", Model: "deepseek-chat",
		Env: map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "${DEEPSEEK_API_KEY}"}}
	opus := Profile{Name: "opus", Harness: "claudecode", Model: "opus"}
	for _, tc := range []struct {
		name, shared, local string
		want                map[string]Profile
		unusable            string // "shared" | "local" | ""
	}{
		{"neither", none, none, map[string]Profile{}, ""},
		{"shared only", sharedOpus + sharedDeepseek, none, map[string]Profile{"opus": opus, "deepseek": deepseek}, ""},
		{"local only", none, sharedOpus, map[string]Profile{"opus": opus}, ""},
		{"local replaces whole, no env merge", sharedDeepseek,
			"[profiles.deepseek]\nharness = \"claudecode\"\nenv = { ANTHROPIC_BASE_URL = \"http://localhost:8080\" }\n",
			map[string]Profile{"deepseek": {Name: "deepseek", Harness: "claudecode", Env: map[string]string{"ANTHROPIC_BASE_URL": "http://localhost:8080"}}}, ""},
		{"local adds a profile", sharedOpus, sharedDeepseek, map[string]Profile{"opus": opus, "deepseek": deepseek}, ""},
		{"any harness name accepted", "[profiles.pi]\nharness = \"pi\"\n", none, map[string]Profile{"pi": {Name: "pi", Harness: "pi"}}, ""},
		{"shared malformed dropped", "[profiles.opus\nharness = 1", sharedDeepseek, map[string]Profile{"deepseek": deepseek}, "shared"},
		{"shared missing harness dropped", sharedDeepseek + "[profiles.bare]\nmodel = \"x\"\n", sharedOpus, map[string]Profile{"opus": opus}, "shared"},
		{"shared unknown profile key dropped", sharedDeepseek + "[profiles.opus]\nharness = \"claudecode\"\nmodle = \"opus\"\n", none, map[string]Profile{}, "shared"},
		{"shared unknown top-level key dropped", sharedOpus + "[kind_profiles]\nbuild = \"opus\"\n", none, map[string]Profile{}, "shared"},
		{"local malformed dropped", sharedOpus, "[profiles.opus\n", map[string]Profile{"opus": opus}, "local"},
		{"local missing harness dropped", sharedOpus, "[profiles.opus]\nmodel = \"haiku\"\n", map[string]Profile{"opus": opus}, "local"},
		{"local unknown key dropped", sharedOpus, "[profiles.opus]\nharness = \"claudecode\"\nextra = 1\n", map[string]Profile{"opus": opus}, "local"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, common := t.TempDir(), t.TempDir()
			t.Setenv("OE_ROOT", "")
			write(t, ws, tc.shared)
			write(t, common, tc.local)
			evs := eventlogtest.Capture(t)
			if got := Load(ws, common); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Load = %+v, want %+v", got, tc.want)
			}
			var files []string
			for _, e := range *evs {
				if e.Name != "agent-profiles.unusable" || e.Attrs["reason"] == "" {
					t.Errorf("event %+v, want agent-profiles.unusable with a reason", e)
				}
				files = append(files, e.Attrs["file"].(string))
			}
			var want []string
			switch tc.unusable {
			case "shared":
				want = []string{filepath.Join(ws, "agent-profiles.toml")}
			case "local":
				want = []string{filepath.Join(common, "agent-profiles.toml")}
			}
			if !reflect.DeepEqual(files, want) {
				t.Errorf("unusable files = %v, want %v", files, want)
			}
		})
	}
}

// The checked-in layer lives at $OE_ROOT when set, else at the Workspace root;
// an unresolvable common dir ("") is a missing local layer.
func TestLoadOERoot(t *testing.T) {
	ws, oe := t.TempDir(), t.TempDir()
	write(t, ws, "[profiles.ws]\nharness = \"claudecode\"\n")
	write(t, oe, "[profiles.oe]\nharness = \"claudecode\"\n")

	t.Setenv("OE_ROOT", oe)
	if got := Load(ws, ""); !reflect.DeepEqual(got, map[string]Profile{"oe": {Name: "oe", Harness: "claudecode"}}) {
		t.Errorf("with $OE_ROOT Load = %+v, want only the OE root's profile", got)
	}
	t.Setenv("OE_ROOT", "")
	if got := Load(ws, ""); !reflect.DeepEqual(got, map[string]Profile{"ws": {Name: "ws", Harness: "claudecode"}}) {
		t.Errorf("without $OE_ROOT Load = %+v, want the Workspace root's profile", got)
	}
}
