package loop

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog/eventlogtest"
	"jayulabs.org/mugicrew/git"
)

// writeConfig writes body as root's .mugicrew/config.toml.
func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".mugicrew")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitRepo is a fresh git repo whose common dir holds the machine-local override.
func gitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return root
}

// writeLocal writes body as the machine-local override in root's git common dir.
func writeLocal(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".git", "mugicrew")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// PoolSetting layers the machine-local override over the Workspace's checked-in
// .mugicrew/config.toml over DefaultPool (Spec FR27/FR30-32, ADR 0110): each
// layer is decoded on its own, a missing file or key falls through silently, an
// unusable one (malformed, non-integer, < 1) falls through to the NEXT layer, and
// unknown keys are ignored in both files. Pool is its N.
func TestPoolReadsConcurrency(t *testing.T) {
	const none = "\x00" // no file at all
	for _, tc := range []struct {
		name, local, shared string
		want                Setting
	}{
		{"neither", none, none, Setting{N: 3, Source: "default"}},
		{"shared missing key", none, "other = 7\n", Setting{N: 3, Source: "default"}},
		{"shared valid, unknown keys ignored", none, "concurrency = 5\nlater = \"x\"\n", Setting{N: 5, Source: "config.toml"}},
		{"shared context table ignored", none, "concurrency = 5\n[context.x]\npath = \"apps/x\"\n", Setting{N: 5, Source: "config.toml"}},
		{"shared malformed", none, "concurrency = = 5\n", Setting{N: 3, Source: "default", Unusable: "config.toml"}},
		{"shared zero", none, "concurrency = 0\n", Setting{N: 3, Source: "default", Unusable: "config.toml"}},
		{"shared negative", none, "concurrency = -2\n", Setting{N: 3, Source: "default", Unusable: "config.toml"}},
		{"shared string", none, "concurrency = \"four\"\n", Setting{N: 3, Source: "default", Unusable: "config.toml"}},
		{"shared float", none, "concurrency = 2.5\n", Setting{N: 3, Source: "default", Unusable: "config.toml"}},
		{"local wins over shared", "concurrency = 5\n", "concurrency = 2\n", Setting{N: 5, Source: "local", Shared: 2}},
		{"local alone, no shared value", "concurrency = 5\nlater = 1\n", "other = 7\n", Setting{N: 5, Source: "local"}},
		{"local missing key falls to shared", "other = 1\n", "concurrency = 2\n", Setting{N: 2, Source: "config.toml"}},
		{"local malformed falls to shared", "concurrency = = 5\n", "concurrency = 2\n", Setting{N: 2, Source: "config.toml", Unusable: "local"}},
		{"local zero falls to shared", "concurrency = 0\n", "concurrency = 2\n", Setting{N: 2, Source: "config.toml", Unusable: "local"}},
		{"local negative falls to shared", "concurrency = -1\n", "concurrency = 2\n", Setting{N: 2, Source: "config.toml", Unusable: "local"}},
		{"local string falls to shared", "concurrency = \"x\"\n", "concurrency = 2\n", Setting{N: 2, Source: "config.toml", Unusable: "local"}},
		{"local float falls to shared", "concurrency = 1.5\n", "concurrency = 2\n", Setting{N: 2, Source: "config.toml", Unusable: "local"}},
		{"both unusable", "concurrency = 0\n", "concurrency = = 1\n", Setting{N: 3, Source: "default", Unusable: "local"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gitRepo(t)
			if tc.local != none {
				writeLocal(t, root, tc.local)
			}
			if tc.shared != none {
				writeConfig(t, root, tc.shared)
			}
			if got := PoolSetting(root); got != tc.want {
				t.Errorf("PoolSetting = %+v, want %+v", got, tc.want)
			}
			if got := Pool(root); got != tc.want.N {
				t.Errorf("Pool = %d, want %d", got, tc.want.N)
			}
		})
	}
}

// Outside any git repo the common dir can't be resolved, so the local layer is
// skipped as missing and the checked-in file still applies.
func TestPoolNoCommonDirSkipsLocal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	writeConfig(t, root, "concurrency = 4\n")
	if got := PoolSetting(root); got != (Setting{N: 4, Source: "config.toml"}) {
		t.Errorf("PoolSetting = %+v, want 4 from config.toml", got)
	}
}

// An edit to concurrency between two reads applies to the next one, with no
// restart (FR4): Pool is re-read every call, never cached, so a lowered or raised
// setting changes how many candidates the next tick's selectDispatch may pick.
func TestPoolEditAppliesNextTick(t *testing.T) {
	root := t.TempDir()
	cands := []bd.Bead{
		{ID: "r1", Kind: contract.KindResearch, Parent: "ws-c1"},
		{ID: "r2", Kind: contract.KindResearch, Parent: "ws-c2"},
		{ID: "r3", Kind: contract.KindResearch, Parent: "ws-c3"},
	}
	writeConfig(t, root, "concurrency = 1\n")
	if got := selectDispatch(cands, Pool(root), 0, nil); len(got) != 1 {
		t.Fatalf("pool 1 picked %d, want 1", len(got))
	}
	writeConfig(t, root, "concurrency = 3\n")
	if got := selectDispatch(cands, Pool(root), 0, nil); len(got) != 3 {
		t.Fatalf("pool 3 picked %d, want 3", len(got))
	}
}

// SetLocalPool atomically saves the machine-local override (Spec FR18-20,
// ADR 0110, amended by ADR 0112 §6): it creates <common>/mugicrew, PoolSetting
// reads it back as local, an existing file keeps its other keys, n < 1 is
// refused, and no temp file is left beside it.
func TestSetLocalPool(t *testing.T) {
	root := gitRepo(t)
	if err := SetLocalPool(root, 0); err == nil {
		t.Error("SetLocalPool(0) succeeded")
	}
	if err := SetLocalPool(root, 4); err != nil {
		t.Fatal(err)
	}
	if got := PoolSetting(root); got != (Setting{N: 4, Source: "local"}) {
		t.Errorf("read back %+v, want 4 local", got)
	}
	writeLocal(t, root, "concurrency = 9\nother = 1\n")
	if err := SetLocalPool(root, 7); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".git", "mugicrew")
	if b, _ := os.ReadFile(filepath.Join(dir, "config.toml")); string(b) != "concurrency = 7\nother = 1\n" {
		t.Errorf("override %q, want only concurrency rewritten", b)
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Errorf("override dir holds %d entries, want only config.toml", len(es))
	}
}

// AutoExitSetting layers auto_exit exactly as the pool (Spec D8, ADR
// 0112): local over checked-in over the 1m default; a Go duration string, where
// "0"/"0s" is off; a missing key falls through silently; a bare integer, a
// negative value or bad syntax is unusable and falls through, naming the layer.
func TestAutoExitSetting(t *testing.T) {
	const none = "\x00"
	for _, tc := range []struct {
		name, local, shared string
		want                AutoExit
	}{
		{"neither", none, none, AutoExit{D: time.Minute, Source: "default"}},
		{"missing key falls through", "concurrency = 2\n", "other = 1\n", AutoExit{D: time.Minute, Source: "default"}},
		{"shared", none, "auto_exit = \"2m\"\n", AutoExit{D: 2 * time.Minute, Source: "config.toml"}},
		{"local over shared", "auto_exit = \"90s\"\n", "auto_exit = \"2m\"\n", AutoExit{D: 90 * time.Second, Source: "local", Shared: 2 * time.Minute, SharedSet: true}},
		{"local over shared off", "auto_exit = \"30s\"\n", "auto_exit = \"0\"\n", AutoExit{D: 30 * time.Second, Source: "local", SharedSet: true}},
		{"local alone", "auto_exit = \"30s\"\n", none, AutoExit{D: 30 * time.Second, Source: "local"}},
		{"local missing key falls to shared", "concurrency = 2\n", "auto_exit = \"2m\"\n", AutoExit{D: 2 * time.Minute, Source: "config.toml"}},
		{"zero is off", "auto_exit = \"0\"\n", "auto_exit = \"2m\"\n", AutoExit{Source: "local", Shared: 2 * time.Minute, SharedSet: true}},
		{"0s is off", none, "auto_exit = \"0s\"\n", AutoExit{Source: "config.toml"}},
		{"local integer falls to shared", "auto_exit = 120\n", "auto_exit = \"2m\"\n", AutoExit{D: 2 * time.Minute, Source: "config.toml", Unusable: "local"}},
		{"local negative falls to shared", "auto_exit = \"-1m\"\n", "auto_exit = \"2m\"\n", AutoExit{D: 2 * time.Minute, Source: "config.toml", Unusable: "local"}},
		{"local bad syntax falls to shared", "auto_exit = \"abc\"\n", "auto_exit = \"2m\"\n", AutoExit{D: 2 * time.Minute, Source: "config.toml", Unusable: "local"}},
		{"shared integer falls to default", none, "auto_exit = 120\n", AutoExit{D: time.Minute, Source: "default", Unusable: "config.toml"}},
		{"shared negative falls to default", none, "auto_exit = \"-1m\"\n", AutoExit{D: time.Minute, Source: "default", Unusable: "config.toml"}},
		{"shared bad syntax falls to default", none, "auto_exit = \"abc\"\n", AutoExit{D: time.Minute, Source: "default", Unusable: "config.toml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gitRepo(t)
			if tc.local != none {
				writeLocal(t, root, tc.local)
			}
			if tc.shared != none {
				writeConfig(t, root, tc.shared)
			}
			if got := AutoExitSetting(root); got != tc.want {
				t.Errorf("AutoExitSetting = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Saving one local setting rewrites only its own key (Spec D9, ADR 0112
// §6): the pool save keeps a local auto_exit and unknown keys, the auto_exit save
// keeps a local concurrency, a negative auto_exit is refused, and the write still
// goes through a temp file renamed over config.toml (none left behind).
func TestLocalSavesKeepOtherKeys(t *testing.T) {
	root := gitRepo(t)
	writeLocal(t, root, "auto_exit = \"90s\"\nother = 1\n")
	if err := SetLocalPool(root, 5); err != nil {
		t.Fatal(err)
	}
	if got := AutoExitSetting(root); got != (AutoExit{D: 90 * time.Second, Source: "local"}) {
		t.Errorf("after pool save auto_exit = %+v, want 90s local", got)
	}
	if err := SetLocalAutoExit(root, -time.Second); err == nil {
		t.Error("SetLocalAutoExit(-1s) succeeded")
	}
	if err := SetLocalAutoExit(root, 0); err != nil {
		t.Fatal(err)
	}
	if got := PoolSetting(root); got != (Setting{N: 5, Source: "local"}) {
		t.Errorf("after auto_exit save pool = %+v, want 5 local", got)
	}
	if got := AutoExitSetting(root); got != (AutoExit{Source: "local"}) {
		t.Errorf("auto_exit read back %+v, want off local", got)
	}
	dir := filepath.Join(root, ".git", "mugicrew")
	if b, _ := os.ReadFile(filepath.Join(dir, "config.toml")); !strings.Contains(string(b), "other = 1") {
		t.Errorf("override %q lost the unknown key", b)
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Errorf("override dir holds %d entries, want only config.toml", len(es))
	}
}

// KindProfile picks a kind's Agent Profile name per ADR 0122 §2-3: the first set
// of local <kind>, shared <kind>, local default, shared default, else "" (the
// built-in), each key falling through on its own, re-read every call.
func TestKindProfile(t *testing.T) {
	const none = "\x00"
	for _, tc := range []struct {
		name, local, shared string
		want                ProfileMapping
	}{
		{"nothing mapped", none, none, ProfileMapping{Source: "default"}},
		{"empty tables", "[kind_profiles]\n", "[kind_profiles]\n", ProfileMapping{Source: "default"}},
		{"shared default", none, "[kind_profiles]\ndefault = \"opus\"\n", ProfileMapping{Name: "opus", Source: "config.toml", Key: "default"}},
		{"local default beats shared default", "[kind_profiles]\ndefault = \"haiku\"\n", "[kind_profiles]\ndefault = \"opus\"\n", ProfileMapping{Name: "haiku", Source: "local", Key: "default"}},
		{"shared kind beats shared default", none, "[kind_profiles]\ndefault = \"opus\"\nbuild = \"deepseek\"\n", ProfileMapping{Name: "deepseek", Source: "config.toml", Key: "build"}},
		{"local default does not beat shared kind", "[kind_profiles]\ndefault = \"haiku\"\n", "[kind_profiles]\nbuild = \"deepseek\"\n", ProfileMapping{Name: "deepseek", Source: "config.toml", Key: "build"}},
		{"local kind beats shared kind", "[kind_profiles]\nbuild = \"sonnet\"\n", "[kind_profiles]\nbuild = \"deepseek\"\n", ProfileMapping{Name: "sonnet", Source: "local", Key: "build"}},
		{"local other kind does not hide shared kind", "[kind_profiles]\nresearch = \"haiku\"\n", "[kind_profiles]\nbuild = \"deepseek\"\n", ProfileMapping{Name: "deepseek", Source: "config.toml", Key: "build"}},
		{"other kind only falls to built-in", none, "[kind_profiles]\nresearch = \"haiku\"\n", ProfileMapping{Source: "default"}},
		{"local malformed falls to shared", "[kind_profiles\n", "[kind_profiles]\nbuild = \"deepseek\"\n", ProfileMapping{Name: "deepseek", Source: "config.toml", Key: "build"}},
		{"non-string value ignored alone", "[kind_profiles]\nbuild = 3\ndefault = \"haiku\"\n", none, ProfileMapping{Name: "haiku", Source: "local", Key: "default"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gitRepo(t)
			if tc.local != none {
				writeLocal(t, root, tc.local)
			}
			if tc.shared != none {
				writeConfig(t, root, tc.shared)
			}
			eventlogtest.Capture(t)
			if got := KindProfile(root, contract.KindBuild); got != tc.want {
				t.Errorf("KindProfile = %+v, want %+v", got, tc.want)
			}
			// the TUI's batched read and shared resolution agree with KindProfile
			local, shared := KindProfiles(root)
			if got := ResolveProfile(local, shared, string(contract.KindBuild)); got != tc.want {
				t.Errorf("ResolveProfile(KindProfiles) = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A [kind_profiles] key that is neither a routing kind nor default logs
// config.unusable {path, key} and is ignored alone (ADR 0123 §7): the other keys
// still apply, and the misspelled kind falls through as usual.
func TestKindProfileTypoKey(t *testing.T) {
	root := gitRepo(t)
	writeLocal(t, root, "[kind_profiles]\nbiuld = \"sonnet\"\nresearch = \"haiku\"\n")
	writeConfig(t, root, "[kind_profiles]\ndefault = \"opus\"\n")
	evs := eventlogtest.Capture(t)
	if got := KindProfile(root, contract.KindResearch); got != (ProfileMapping{Name: "haiku", Source: "local", Key: "research"}) {
		t.Errorf("research = %+v, want haiku from local", got)
	}
	if got := KindProfile(root, contract.KindBuild); got != (ProfileMapping{Name: "opus", Source: "config.toml", Key: "default"}) {
		t.Errorf("build = %+v, want the shared default past the typo", got)
	}
	path := filepath.Join(root, ".git", "mugicrew", "config.toml")
	for _, e := range *evs {
		if e.Name != "config.unusable" || e.Attrs["path"] != path || e.Attrs["key"] != "biuld" {
			t.Errorf("event %+v, want config.unusable {path: %s, key: biuld}", e, path)
		}
	}
	if len(*evs) != 2 {
		t.Errorf("events = %v, want one config.unusable per read", eventlogtest.Names(*evs))
	}
}

// KindProfiles reads both layers in one pass, so a bad key warns once per call
// however many rows the TUI then resolves from it (Spec FR44).
func TestKindProfilesWarnsOncePerCall(t *testing.T) {
	root := gitRepo(t)
	writeLocal(t, root, "[kind_profiles]\nbiuld = \"sonnet\"\nresearch = \"haiku\"\n")
	writeConfig(t, root, "[kind_profiles]\ndefault = \"opus\"\nbuild = \"deepseek\"\n")
	evs := eventlogtest.Capture(t)
	local, shared := KindProfiles(root)
	for _, k := range []string{"default", "build", "research", "spec"} {
		ResolveProfile(local, shared, k)
	}
	if len(*evs) != 1 || (*evs)[0].Name != "config.unusable" || (*evs)[0].Attrs["key"] != "biuld" {
		t.Errorf("events = %+v, want one config.unusable {key: biuld}", *evs)
	}
	if got := ResolveProfile(local, shared, "research"); got != (ProfileMapping{Name: "haiku", Source: "local", Key: "research"}) {
		t.Errorf("research = %+v, want haiku from local", got)
	}
	if got := ResolveProfile(local, shared, "default"); got != (ProfileMapping{Name: "opus", Source: "config.toml", Key: "default"}) {
		t.Errorf("default = %+v, want opus from config.toml", got)
	}
	// resolving with the local key removed is what an unset would expose
	delete(local, "research")
	if got := ResolveProfile(local, shared, "research"); got != (ProfileMapping{Name: "opus", Source: "config.toml", Key: "default"}) {
		t.Errorf("research unset = %+v, want the shared default", got)
	}
}

// SetLocalKindProfile sets or deletes one local [kind_profiles] key (Spec FR31-33,
// FR38, ADR 0139): every other local key survives, unknown ones included; a second
// kind keeps the first; deleting the last key drops the table; an inline table is
// written back as a table; a key that is neither a kind nor default is refused.
func TestSetLocalKindProfile(t *testing.T) {
	root := gitRepo(t)
	path := filepath.Join(root, ".git", "mugicrew", "config.toml")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }
	writeLocal(t, root, "concurrency = 4\nauto_exit = \"90s\"\nother = 1\nkind_profiles = { review = \"haiku\" }\n")
	if err := SetLocalKindProfile(root, "biuld", "opus"); err == nil {
		t.Error("SetLocalKindProfile(biuld) succeeded")
	}
	if err := SetLocalKindProfile(root, "build", "opus"); err != nil {
		t.Fatal(err)
	}
	if err := SetLocalKindProfile(root, "default", "sonnet"); err != nil {
		t.Fatal(err)
	}
	b := read()
	for _, want := range []string{"concurrency = 4", "auto_exit = \"90s\"", "other = 1", "[kind_profiles]", "build = \"opus\"", "default = \"sonnet\"", "review = \"haiku\""} {
		if !strings.Contains(b, want) {
			t.Errorf("override %q lacks %q", b, want)
		}
	}
	if got := KindProfile(root, contract.KindBuild); got != (ProfileMapping{Name: "opus", Source: "local", Key: "build"}) {
		t.Errorf("build = %+v, want opus local", got)
	}
	if got := PoolSetting(root); got.N != 4 {
		t.Errorf("pool = %+v, want 4", got)
	}
	for _, k := range []string{"build", "default", "review"} {
		if err := SetLocalKindProfile(root, k, ""); err != nil {
			t.Fatal(err)
		}
	}
	if b := read(); strings.Contains(b, "kind_profiles") || !strings.Contains(b, "other = 1") {
		t.Errorf("after deleting every key override = %q, want no table, other kept", b)
	}
	if got := KindProfile(root, contract.KindBuild); got != (ProfileMapping{Source: "default"}) {
		t.Errorf("build after unset = %+v, want the built-in", got)
	}
}

// A local file that doesn't parse refuses a kind-profile save and is left
// untouched, so a set or delete never throws away the human's other settings.
func TestSetLocalKindProfileMalformedRefuses(t *testing.T) {
	root := gitRepo(t)
	const bad = "concurrency = 4\n[kind_profiles\n"
	writeLocal(t, root, bad)
	if err := SetLocalKindProfile(root, "build", "opus"); err == nil {
		t.Error("save over a malformed file succeeded")
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".git", "mugicrew", "config.toml")); string(b) != bad {
		t.Errorf("malformed file rewritten to %q", b)
	}
}

// LandStrategySetting layers land_strategy as auto_exit: local over checked-in
// over the "local" default; only "local" and "gitlab" are usable.
func TestLandStrategySetting(t *testing.T) {
	const none = "\x00"
	for _, tc := range []struct {
		name, local, shared string
		want                LandStrategy
	}{
		{"neither", none, none, LandStrategy{Name: "local", Source: "default"}},
		{"missing key falls through", "concurrency = 2\n", "other = 1\n", LandStrategy{Name: "local", Source: "default"}},
		{"shared", none, "land_strategy = \"gitlab\"\n", LandStrategy{Name: "gitlab", Source: "config.toml"}},
		{"local over shared", "land_strategy = \"local\"\n", "land_strategy = \"gitlab\"\n", LandStrategy{Name: "local", Source: "local", Shared: "gitlab", SharedSet: true}},
		{"local unknown falls to shared", "land_strategy = \"svn\"\n", "land_strategy = \"gitlab\"\n", LandStrategy{Name: "gitlab", Source: "config.toml", Unusable: "local", UnusableValue: "svn"}},
		{"shared integer falls to default", none, "land_strategy = 1\n", LandStrategy{Name: "local", Source: "default", Unusable: "config.toml", UnusableValue: "1"}},
		{"shared unknown falls to default", none, "land_strategy = \"svn\"\n", LandStrategy{Name: "local", Source: "default", Unusable: "config.toml", UnusableValue: "svn"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gitRepo(t)
			if tc.local != none {
				writeLocal(t, root, tc.local)
			}
			if tc.shared != none {
				writeConfig(t, root, tc.shared)
			}
			if got := LandStrategySetting(root); got != tc.want {
				t.Errorf("LandStrategySetting = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Revert no longer refuses under gitlab: it fails here only for the bare
// repo's missing main (ADR 0212, ADR 0213).
func TestRevertDoesNotRefuseUnderGitlab(t *testing.T) {
	root := gitRepo(t)
	t.Chdir(root)
	writeLocal(t, root, "land_strategy = \"gitlab\"\n")
	_, err := git.Revert("deadbeef", "Reverts-Checkpoint: x")
	if err == nil || strings.Contains(err.Error(), "refused") {
		t.Errorf("err = %v, want the unknown-revision failure, not a refusal", err)
	}
}

// git.LandStrategyName surfaces an unusable land_strategy as an error, never a
// silent fallback to local (ADR 0147 §3).
func TestLandStrategyNameRefusesUnusable(t *testing.T) {
	root := gitRepo(t)
	writeConfig(t, root, "land_strategy = \"GitLab\"\n")
	t.Chdir(root)
	if name, err := git.LandStrategyName(); err == nil || !strings.Contains(err.Error(), `"GitLab"`) {
		t.Errorf("LandStrategyName = %q, %v; want an error naming \"GitLab\"", name, err)
	}
}

// Land reads the checked-in sequences off disk through the wired git seam (no
// injection): a config-declared Flyway clash is refused as the typed clash, an
// unusable rule as a plain error naming the rule and the file.
func TestLandEnforcesConfiguredSequences(t *testing.T) {
	g := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	setup := func(cfg string) {
		root := gitRepo(t)
		t.Chdir(root)
		g("checkout", "-q", "-b", "main")
		g("config", "user.email", "t@example.com")
		g("config", "user.name", "t")
		writeConfig(t, root, cfg)
		g("add", ".")
		g("commit", "-qm", "root")
		g("checkout", "-q", "-b", "chart/x")
		os.MkdirAll("db/migration", 0o755)
		os.WriteFile("db/migration/V12__branch.sql", nil, 0o644)
		g("add", ".")
		g("commit", "-qm", "branch")
		g("checkout", "-q", "main")
		os.MkdirAll("db/migration", 0o755)
		os.WriteFile("db/migration/V12__main.sql", nil, 0o644)
		g("add", ".")
		g("commit", "-qm", "main")
	}

	setup("sequences = [\"db/migration/V{n}__*.sql\"]\n")
	_, err := git.Land("chart/x", "Chart: x", git.LandLocal)
	var clash *git.NumberClash
	if !errors.As(err, &clash) || !strings.Contains(clash.Clashes[0], "V12__branch.sql") {
		t.Fatalf("Land = %v, want *NumberClash on V12", err)
	}

	setup("sequences = [\"db/migration/V__*.sql\"]\n")
	_, err = git.Land("chart/x", "Chart: x", git.LandLocal)
	if errors.As(err, &clash) || err == nil || !strings.Contains(err.Error(), "V__*.sql") || !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("Land = %v, want a plain error naming the rule and config.toml", err)
	}
}
