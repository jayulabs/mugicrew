package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/agentprofile"
	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/claudecode"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
)

// profileWorkspace is a Workspace root outside any git repo (no local layers)
// holding catalog as its checked-in agent-profiles.toml and mapping as its
// .mugicrew/config.toml, with claude on the stubbed PATH.
func profileWorkspace(t *testing.T, catalog, mapping string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	t.Setenv("OE_ROOT", "")
	eventlogtest.Capture(t)
	if catalog != "" {
		if err := os.WriteFile(filepath.Join(root, "agent-profiles.toml"), []byte(catalog), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".mugicrew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".mugicrew", "config.toml"), []byte(mapping), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := lookPath
	lookPath = func(bin string) (string, error) {
		if bin == "claude" {
			return "/bin/claude", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = orig })
	return root
}

const deepseekCatalog = `
[profiles.deepseek]
harness = "claudecode"
model = "deepseek-chat"
env = { ANTHROPIC_BASE_URL = "https://api.deepseek.com/anthropic", ANTHROPIC_AUTH_TOKEN = "${DEEPSEEK_API_KEY}" }

[profiles.pi]
harness = "pi"
`

// ctxForKind runs kind → mapping → catalog → checks (Spec "Profile resolver",
// ADR 0123 §1-2): a runnable profile builds its harness with its model and
// expanded env; nothing mapped is the always-runnable built-in; every unrunnable
// case names its reason and never falls back to another layer, default or the built-in.
func TestCtxForKindResolution(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-123")
	deepseek := &claudecode.Backend{Model: "deepseek-chat",
		Env: map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "sk-123"}}
	for _, tc := range []struct {
		name, catalog, mapping string
		profile                string
		want                   *claudecode.Backend
		reason                 string // non-empty: unrunnable, the error contains it
	}{
		{"built-in when nothing mapped", "", "", "", &claudecode.Backend{}, ""},
		{"built-in with a catalog but no mapping", deepseekCatalog, "", "", &claudecode.Backend{}, ""},
		{"runnable, env expanded", deepseekCatalog, "[kind_profiles]\nbuild = \"deepseek\"\n", "deepseek", deepseek, ""},
		{"default names the profile", deepseekCatalog, "[kind_profiles]\ndefault = \"deepseek\"\n", "deepseek", deepseek, ""},
		{"absent profile", deepseekCatalog, "[kind_profiles]\nbuild = \"opus\"\n", "", nil, "not declared"},
		{"empty catalog", "", "[kind_profiles]\nbuild = \"deepseek\"\n", "", nil, "not declared"},
		{"dropped layer", deepseekCatalog + "typo = 1\n", "[kind_profiles]\nbuild = \"deepseek\"\n", "", nil, "not declared"},
		{"unsupported harness", deepseekCatalog, "[kind_profiles]\nbuild = \"pi\"\n", "", nil, "unsupported harness"},
		{"no fallback to default", deepseekCatalog, "[kind_profiles]\nbuild = \"opus\"\ndefault = \"deepseek\"\n", "", nil, "not declared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := profileWorkspace(t, tc.catalog, tc.mapping)
			ctx, name, err := ctxForKind(root, contract.KindBuild)
			if tc.reason != "" {
				if err == nil || !strings.Contains(err.Error(), tc.reason) {
					t.Fatalf("ctxForKind = %q, %v; want unrunnable %q", name, err, tc.reason)
				}
				return
			}
			if err != nil || name != tc.profile || !reflect.DeepEqual(ctx.Harness(), tc.want) {
				t.Errorf("ctxForKind = %q, %v; want %+v on %q", name, err, tc.want, tc.profile)
			}
		})
	}
}

// A harness whose binary is not on PATH is unrunnable.
func TestCtxForKindMissingBinary(t *testing.T) {
	root := profileWorkspace(t, deepseekCatalog, "[kind_profiles]\nbuild = \"deepseek\"\n")
	t.Setenv("DEEPSEEK_API_KEY", "sk")
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, _, err := ctxForKind(root, contract.KindBuild); err == nil || !strings.Contains(err.Error(), "claude not on PATH") {
		t.Errorf("err = %v, want claude not on PATH", err)
	}
}

// ${VAR} uses lookup-env semantics (ADR 0123 §1): unset refuses, set-but-empty
// expands to "".
func TestCtxForKindEnvExpansion(t *testing.T) {
	root := profileWorkspace(t, deepseekCatalog, "[kind_profiles]\nbuild = \"deepseek\"\n")
	t.Setenv("DEEPSEEK_API_KEY", "") // and restores it after the Unsetenv below
	ctx, _, err := ctxForKind(root, contract.KindBuild)
	if err != nil || ctx.Harness().(*claudecode.Backend).Env["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Errorf("set-but-empty: %v; want token expanded to empty", err)
	}
	os.Unsetenv("DEEPSEEK_API_KEY")
	if _, _, err := ctxForKind(root, contract.KindBuild); err == nil || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Errorf("unset: err = %v, want unrunnable naming DEEPSEEK_API_KEY", err)
	}
}

// resolveRecorded starts from a session's recorded profile name and never reads
// the mapping (ADR 0122 §6): "" is the built-in, a name is looked up in the
// current catalog.
func TestResolveRecordedIgnoresMapping(t *testing.T) {
	root := profileWorkspace(t, deepseekCatalog, "[kind_profiles]\ndefault = \"opus\"\n")
	t.Setenv("DEEPSEEK_API_KEY", "sk")
	if got, err := resolveRecorded(root, ""); err != nil || !reflect.DeepEqual(got, agentprofile.Resolution{Harness: "claudecode"}) {
		t.Errorf("recorded \"\" = %+v, %v; want the built-in", got, err)
	}
	if got, err := resolveRecorded(root, "deepseek"); err != nil || got.Profile != "deepseek" || got.Model != "deepseek-chat" {
		t.Errorf("recorded deepseek = %+v, %v; want deepseek", got, err)
	}
}

// An effort's ctx is assembled for its kind's profile (ADR 0125):
// a mapped kind runs a claudecode backend with the profile's model and expanded
// env, an unmapped kind the built-in (no --model, no extra env), and an
// unrunnable mapping is an error, never a fallback.
func TestCtxForKind(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-123")
	root := profileWorkspace(t, deepseekCatalog, "[kind_profiles]\nbuild = \"deepseek\"\nresearch = \"opus\"\n")
	ctx, name, err := ctxForKind(root, contract.KindBuild)
	want := &claudecode.Backend{Model: "deepseek-chat",
		Env: map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "sk-123"}}
	if err != nil || name != "deepseek" || !reflect.DeepEqual(ctx.Harness(), want) {
		t.Errorf("mapped build = %+v, %q, %v; want %+v on deepseek", ctx.Harness(), name, err, want)
	}
	ctx, name, err = ctxForKind(root, contract.KindSpec)
	if err != nil || name != "" || !reflect.DeepEqual(ctx.Harness(), &claudecode.Backend{}) {
		t.Errorf("unmapped spec = %+v, %q, %v; want the built-in", ctx.Harness(), name, err)
	}
	if _, name, err := ctxForKind(root, contract.KindResearch); err == nil || name != "opus" {
		t.Errorf("an unrunnable mapping must be an error naming its profile; got %q, %v", name, err)
	}
}

// A resume's ctx is assembled from the session's recorded profile, read from
// the current catalog with no mapping (ADR 0122 §6): "" is the built-in, and an
// unrunnable name is an error.
func TestCtxForProfile(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-123")
	root := profileWorkspace(t, deepseekCatalog, "[kind_profiles]\ndefault = \"opus\"\n")
	ctx, err := ctxForProfile(root, "deepseek")
	if err != nil || ctx.Harness().(*claudecode.Backend).Model != "deepseek-chat" {
		t.Errorf("recorded deepseek = %+v, %v; want the deepseek backend", ctx, err)
	}
	if ctx, err := ctxForProfile(root, ""); err != nil || !reflect.DeepEqual(ctx.Harness(), &claudecode.Backend{}) {
		t.Errorf("recorded built-in = %+v, %v; want the built-in, not default's mapping", ctx, err)
	}
	if _, err := ctxForProfile(root, "opus"); err == nil {
		t.Error("an unrunnable recorded profile must be an error")
	}
}

// profileReasons checks a merged set in one pass with the per-profile checks:
// unset ${VAR}, unsupported harness, bad table; runnable names are absent, and
// each reason ends (from <file>).
func TestProfileReasons(t *testing.T) {
	os.Unsetenv("DEEPSEEK_API_KEY")
	lookPath = func(string) (string, error) { return "claude", nil }
	t.Cleanup(func() { lookPath = exec.LookPath })
	got := profileReasons("/ws", map[string]agentprofile.Entry{
		"ok":  {Profile: agentprofile.Profile{Name: "ok", Harness: "claudecode"}, Path: "/ws/agent-profiles.toml"},
		"env": {Profile: agentprofile.Profile{Name: "env", Harness: "claudecode", Env: map[string]string{"K": "${DEEPSEEK_API_KEY}"}}, Path: "/ws/.mugicrew/config.toml"},
		"odd": {Profile: agentprofile.Profile{Name: "odd", Harness: "codex"}, Path: "/elsewhere/agent-profiles.toml"},
		"bad": {Profile: agentprofile.Profile{Name: "bad"}, Path: "/ws/.mugicrew/config.toml", Err: errors.New("unknown key modle")},
	})
	if len(got) != 3 || !strings.Contains(got["env"], "DEEPSEEK_API_KEY") || !strings.HasSuffix(got["env"], " (from .mugicrew/config.toml)") ||
		got["odd"] != "unsupported harness codex (from /elsewhere/agent-profiles.toml)" ||
		got["bad"] != "declared unusable: unknown key modle (from .mugicrew/config.toml)" {
		t.Errorf("reasons = %v", got)
	}
}

// A profile declared in config.toml resolves through the four-file merge; the
// local config.toml beats the OE local; a bad table is refused naming its file;
// an undeclared name is "not declared" with no file.
func TestResolveRecordedConfigToml(t *testing.T) {
	root := profileWorkspace(t, "[profiles.cfg]\nharness = \"claudecode\"\nmodel = \"oe\"\n",
		"[profiles.cfg]\nharness = \"claudecode\"\nmodel = \"shared\"\n[profiles.bad]\nharness = \"claudecode\"\nmodle = \"x\"\n")
	if got, err := resolveRecorded(root, "cfg"); err != nil || got.Model != "shared" {
		t.Errorf("cfg = %+v, %v; want the config.toml table over the OE one", got, err)
	}
	if _, err := resolveRecorded(root, "bad"); err == nil || err.Error() != "declared unusable: unknown key modle (from .mugicrew/config.toml)" {
		t.Errorf("bad err = %v", err)
	}
	if _, err := resolveRecorded(root, "nope"); err == nil || err.Error() != "not declared" {
		t.Errorf("nope err = %v, want not declared", err)
	}
}

// The resolver's error carries the winning file's path for the profile.unusable
// path attr (ADR 0306 §4); an undeclared name carries none.
func TestResolveRecordedErrorCarriesPath(t *testing.T) {
	root := profileWorkspace(t, "", "[profiles.bad]\nharness = \"claudecode\"\nmodle = \"x\"\n[profiles.odd]\nharness = \"codex\"\n")
	for _, name := range []string{"bad", "odd"} {
		_, err := resolveRecorded(root, name)
		if got := agentprofile.ErrPath(err); got != cfglayer.SharedPath(root) {
			t.Errorf("%s: path = %q, want %q (err %v)", name, got, cfglayer.SharedPath(root), err)
		}
	}
	if _, err := resolveRecorded(root, "nope"); agentprofile.ErrPath(err) != "" {
		t.Errorf("nope carries path %q", agentprofile.ErrPath(err))
	}
}

func TestResolveRecordedLocalConfigBeatsOELocal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OE_ROOT", "")
	eventlogtest.Capture(t)
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	for path, model := range map[string]string{".git/mugicrew/config.toml": "mc-local", ".git/agent-profiles.toml": "oe-local"} {
		full := filepath.Join(root, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("[profiles.p]\nharness = \"claudecode\"\nmodel = \""+model+"\"\n"), 0o644)
	}
	orig := lookPath
	lookPath = func(string) (string, error) { return "claude", nil }
	t.Cleanup(func() { lookPath = orig })
	if got, err := resolveRecorded(root, "p"); err != nil || got.Model != "mc-local" {
		t.Errorf("p = %+v, %v; want mugicrew local", got, err)
	}
}
