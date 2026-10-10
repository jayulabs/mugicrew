package loop

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"jayulabs.org/mugicrew/git"
)

// Setting is the resolved AFK pool size and where it came from, from one read.
type Setting struct {
	N        int    // the pool size in force
	Source   string // "local" | "config.toml" | "default"
	Unusable string // the first layer skipped as unusable ("local" | "config.toml"); "" if none
	Shared   int    // the checked-in value when Source is "local" and one is set; 0 otherwise
}

// Pool is the AFK pool size in force: PoolSetting(root).N.
func Pool(root string) int { return PoolSetting(root).N }

// AutoExit is the resolved Auto-exit duration and where it came from, from one read.
type AutoExit struct {
	D         time.Duration // the duration in force; 0 is off
	Source    string        // "local" | "config.toml" | "default"
	Unusable  string        // the first layer skipped as unusable; "" if none
	Shared    time.Duration // the checked-in value when Source is "local" and SharedSet
	SharedSet bool          // a checked-in value is set under a local one (it may be 0, off)
}

// DefaultAutoExit is the built-in Auto-exit duration (ADR 0112 §2).
const DefaultAutoExit = time.Minute

// PoolSetting layers the AFK pool size (Spec FR27-32, ADR 0110, amending
// FR2/FR12): the machine-local override at <git common dir of
// root>/mugicrew/config.toml, then root's checked-in .mugicrew/config.toml (the
// Workspace's shared default), then DefaultPool. Every layer is re-read on every
// call and never cached, so a save applies on the loop's next tick and run-now's
// next press with no restart (FR4). Each layer is decoded on its own: a missing
// file or key falls through silently (an unresolvable common dir is a missing local
// layer); an unusable one (malformed TOML, a non-integer, or < 1) warns and falls
// through to the next layer, never a wedge. Unknown keys are ignored (FR13).
func PoolSetting(root string) Setting {
	l := resolve(root, readConcurrency, DefaultPool)
	return Setting{N: l.v, Source: l.source, Unusable: l.unusable, Shared: l.shared}
}

// AutoExitSetting layers auto_exit the way PoolSetting layers the pool (Spec
// D8, ADR 0112): local, then checked-in, then DefaultAutoExit, re-read on
// every call. A layer's value is a Go duration string; 0 is off. A missing key
// falls through silently; a bare integer, a negative value or bad syntax warns
// config.unusable and falls through.
func AutoExitSetting(root string) AutoExit {
	l := resolve(root, readAutoExit, DefaultAutoExit)
	return AutoExit{D: l.v, Source: l.source, Unusable: l.unusable, Shared: l.shared, SharedSet: l.sharedSet}
}

// layered is one setting resolved across the local, checked-in and default layers.
type layered[T any] struct {
	v                T
	source, unusable string
	shared           T
	sharedSet        bool // shared holds the checked-in value under a local one
}

// resolve reads one setting's layers with read and falls back to def.
func resolve[T any](root string, read func(path string) (T, layerState), def T) layered[T] {
	shared, sharedState := read(filepath.Join(root, ".mugicrew", "config.toml"))
	var l layered[T]
	switch v, state := read(localPath(root)); state {
	case layerSet:
		l.v, l.source = v, "local"
		if sharedState == layerSet {
			l.shared, l.sharedSet = shared, true
		}
		return l
	case layerUnusable:
		l.unusable = "local"
	}
	switch sharedState {
	case layerSet:
		l.v, l.source = shared, "config.toml"
	case layerUnusable:
		if l.unusable == "" {
			l.unusable = "config.toml"
		}
		fallthrough
	default:
		l.v, l.source = def, "default"
	}
	return l
}

// localPath is the machine-local override, <git common dir of root>/mugicrew/
// config.toml; "" (a missing layer) when the common dir can't be resolved.
func localPath(root string) string {
	common, err := git.CommonDirOf(root)
	if err != nil {
		return ""
	}
	return filepath.Join(common, "mugicrew", "config.toml")
}

// SetLocalPool saves n as the machine-local concurrency (Spec FR18-20,
// ADR 0110, amended by ADR 0112 §6) via setLocal.
func SetLocalPool(root string, n int) error {
	if n < 1 {
		return fmt.Errorf("pool size %d < 1", n)
	}
	return setLocal(root, "concurrency", n)
}

// SetLocalAutoExit saves d as the machine-local auto_exit (Spec D9, ADR
// 0112 §6) via setLocal; 0 saves off.
func SetLocalAutoExit(root string, d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("auto-exit %s < 0", d)
	}
	return setLocal(root, "auto_exit", d.String())
}

// setLocal sets one key of the machine-local override and keeps the rest: it
// reads <git common dir of root>/mugicrew/config.toml, sets key, and writes the
// whole back to a temp file in that dir, creating it if needed, renamed over
// config.toml, so a reader never sees a half-written file. No worktree, ref or
// lock is touched, and a racing second writer simply wins last.
func setLocal(root, key string, v any) error {
	common, err := git.CommonDirOf(root)
	if err != nil {
		return err
	}
	dir := filepath.Join(common, "mugicrew")
	path := filepath.Join(dir, "config.toml")
	cfg := map[string]any{}
	// ponytail: a malformed file can't be kept, so the save replaces it with this key alone.
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		cfg = map[string]any{}
	}
	cfg[key] = v
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "config.toml.*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // a no-op once renamed
	err = toml.NewEncoder(f).Encode(cfg)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

type layerState int

const (
	layerMissing layerState = iota
	layerSet
	layerUnusable
)

// decodeLayer decodes one config file into cfg: layerSet when it decoded, missing
// when there is no file, unusable (warned) when it is malformed or mistyped.
func decodeLayer(path string, cfg any) layerState {
	if path == "" {
		return layerMissing
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return layerMissing
		}
		slog.Warn("config.unusable", "path", path, "err", err.Error())
		return layerUnusable
	}
	return layerSet
}

// readConcurrency decodes concurrency from one config file; path "" is missing.
func readConcurrency(path string) (int, layerState) {
	var cfg struct{ Concurrency *int }
	if state := decodeLayer(path, &cfg); state != layerSet {
		return 0, state
	}
	if cfg.Concurrency == nil {
		return 0, layerMissing
	}
	if *cfg.Concurrency < 1 {
		slog.Warn("config.unusable", "path", path, "concurrency", *cfg.Concurrency)
		return 0, layerUnusable
	}
	return *cfg.Concurrency, layerSet
}

// readAutoExit decodes auto_exit from one config file; path "" is missing.
func readAutoExit(path string) (time.Duration, layerState) {
	var cfg struct {
		AutoExit *string `toml:"auto_exit"`
	}
	if state := decodeLayer(path, &cfg); state != layerSet {
		return 0, state
	}
	if cfg.AutoExit == nil {
		return 0, layerMissing
	}
	d, err := time.ParseDuration(*cfg.AutoExit)
	if err != nil || d < 0 {
		slog.Warn("config.unusable", "path", path, "auto_exit", *cfg.AutoExit)
		return 0, layerUnusable
	}
	return d, layerSet
}
