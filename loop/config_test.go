package loop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
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
