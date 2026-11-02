// Package isolate gives tests one way to isolate mugicrew's per-user config
// and cache directories. Go's os.UserConfigDir / os.UserCacheDir honour XDG on
// Linux but use $HOME/Library/... on darwin, so isolation sets HOME and both
// XDG variables together. Only tests import this package; it must not ship.
package isolate

import (
	"os"
	"testing"
)

// Dirs points HOME, XDG_CONFIG_HOME and XDG_CACHE_HOME at fresh temp dirs for
// the test and returns the config and cache dirs as os.UserConfigDir /
// os.UserCacheDir resolve them afterwards.
func Dirs(t testing.TB) (config, cache string) {
	t.Helper()
	for k, v := range settings(t) {
		t.Setenv(k, v)
	}
	return resolve(t)
}

// Env is the subprocess form: the same settings as KEY=VALUE entries to append
// to a child's environment, plus the dirs the child will resolve on this OS.
// The parent's environment is left unchanged.
func Env(t testing.TB) (env []string, config, cache string) {
	t.Helper()
	s := settings(t)
	saved := map[string]*string{}
	for k, v := range s {
		if old, ok := os.LookupEnv(k); ok {
			saved[k] = &old
		} else {
			saved[k] = nil
		}
		env = append(env, k+"="+v)
		os.Setenv(k, v)
	}
	defer func() {
		for k, old := range saved {
			if old == nil {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, *old)
			}
		}
	}()
	config, cache = resolve(t)
	return env, config, cache
}

func settings(t testing.TB) map[string]string {
	root := t.TempDir()
	return map[string]string{
		"HOME":            root,
		"XDG_CONFIG_HOME": root + "/xdg-config",
		"XDG_CACHE_HOME":  root + "/xdg-cache",
	}
}

func resolve(t testing.TB) (config, cache string) {
	t.Helper()
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	cache, err = os.UserCacheDir()
	if err != nil {
		t.Fatalf("os.UserCacheDir: %v", err)
	}
	return config, cache
}
