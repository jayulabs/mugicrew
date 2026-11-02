package isolate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func under(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func TestDirsResolveUnderTempTree(t *testing.T) {
	cfg, cache := Dirs(t)
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME not set")
	}
	gotCfg, _ := os.UserConfigDir()
	gotCache, _ := os.UserCacheDir()
	if gotCfg != cfg || gotCache != cache {
		t.Errorf("returned (%q, %q); os resolves (%q, %q)", cfg, cache, gotCfg, gotCache)
	}
	for _, d := range []string{cfg, cache} {
		if !under(home, d) {
			t.Errorf("%q not under temp tree %q", d, home)
		}
	}
	if os.Getenv("XDG_CONFIG_HOME") == "" || os.Getenv("XDG_CACHE_HOME") == "" {
		t.Error("XDG variables unset")
	}
}

func TestEnvEntriesSetHomeAndBothXDG(t *testing.T) {
	before := os.Getenv("HOME")
	env, cfg, cache := Env(t)
	if os.Getenv("HOME") != before {
		t.Error("Env must not change the parent's environment")
	}
	vals := map[string]string{}
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		vals[k] = v
	}
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		if vals[k] == "" {
			t.Fatalf("env lacks %s: %v", k, env)
		}
	}
	for _, d := range []string{cfg, cache} {
		if !under(vals["HOME"], d) {
			t.Errorf("%q not under temp tree %q", d, vals["HOME"])
		}
	}
}
