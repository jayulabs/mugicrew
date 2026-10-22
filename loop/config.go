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

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
	"jayulabs.org/mugicrew/registry"
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

// LandStrategy is the resolved land_strategy and where it came from, from one read.
type LandStrategy struct {
	Name     string // "local" | "gitlab"
	Source   string // "local" | "config.toml" | "default"
	Unusable string // the first layer skipped as unusable; "" if none
	// UnusableValue is that layer's rejected land_strategy as written (%v), for errors.
	UnusableValue string
	Shared        string // the checked-in value when Source is "local" and SharedSet
	SharedSet     bool
}

// DefaultLandStrategy is the built-in land strategy.
const DefaultLandStrategy = git.LandStrategyLocal

// LandStrategySetting layers land_strategy the way AutoExitSetting layers auto_exit:
// local, then checked-in, then DefaultLandStrategy, re-read on every call. Any
// value other than "local" or "gitlab" warns config.unusable and falls through.
func LandStrategySetting(root string) LandStrategy {
	l := resolve(root, readLandStrategy, DefaultLandStrategy)
	ls := LandStrategy{Name: l.v, Source: l.source, Unusable: l.unusable, Shared: l.shared, SharedSet: l.sharedSet}
	switch l.unusable {
	case "local":
		ls.UnusableValue = rawLandStrategy(cfglayer.LocalPath(root))
	case "config.toml":
		ls.UnusableValue = rawLandStrategy(cfglayer.SharedPath(root))
	}
	return ls
}

func init() {
	// ponytail: one config read per call, like every land_strategy reader; cache if it shows in profiles.
	git.LandStrategyName = func() (string, error) {
		root, err := git.TopLevelOf(".")
		if err != nil {
			return "", err
		}
		return LandStrategyAt(root)
	}
	git.SequenceRules = func(r git.Repo) ([]git.Rule, error) {
		root, err := git.TopLevelOf(".")
		if err != nil {
			return nil, err
		}
		return cfglayer.SequenceRules(root, r)
	}
}

// LandStrategyAt is root's land_strategy name; an unusable value is an error
// naming it and its layer, never a fallback to local (ADR 0147 §3). The one reader
// behind git.LandStrategyName and Jinbe's resolver.
func LandStrategyAt(root string) (string, error) {
	s := LandStrategySetting(root)
	if s.Unusable != "" {
		return "", fmt.Errorf("land_strategy %q in %s is unusable (want local or gitlab)", s.UnusableValue, s.Unusable)
	}
	return s.Name, nil
}

// rawLandStrategy is land_strategy as written in one config file, any type, "" if unreadable.
func rawLandStrategy(path string) string {
	var cfg struct {
		LandStrategy any `toml:"land_strategy"`
	}
	if path == "" {
		return ""
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil || cfg.LandStrategy == nil {
		return ""
	}
	return fmt.Sprintf("%v", cfg.LandStrategy)
}

// layered is one setting resolved across the local, checked-in and default layers.
type layered[T any] struct {
	v                T
	source, unusable string
	shared           T
	sharedSet        bool // shared holds the checked-in value under a local one
}

// resolve reads one setting's layers with read and falls back to def.
func resolve[T any](root string, read func(path string) (T, cfglayer.State), def T) layered[T] {
	shared, sharedState := read(cfglayer.SharedPath(root))
	var l layered[T]
	switch v, state := read(cfglayer.LocalPath(root)); state {
	case cfglayer.Set:
		l.v, l.source = v, "local"
		if sharedState == cfglayer.Set {
			l.shared, l.sharedSet = shared, true
		}
		return l
	case cfglayer.Unusable:
		l.unusable = "local"
	}
	switch sharedState {
	case cfglayer.Set:
		l.v, l.source = shared, "config.toml"
	case cfglayer.Unusable:
		if l.unusable == "" {
			l.unusable = "config.toml"
		}
		fallthrough
	default:
		l.v, l.source = def, "default"
	}
	return l
}

// SetLocalPool saves n as the machine-local concurrency (Spec FR18-20,
// ADR 0110, amended by ADR 0112 §6) via setLocal.
func SetLocalPool(root string, n int) error {
	if n < 1 {
		return fmt.Errorf("pool size %d < 1", n)
	}
	return editLocal(root, false, func(cfg map[string]any) { cfg["concurrency"] = n })
}

// SetLocalAutoExit saves d as the machine-local auto_exit (Spec D9, ADR
// 0112 §6) via setLocal; 0 saves off.
func SetLocalAutoExit(root string, d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("auto-exit %s < 0", d)
	}
	return editLocal(root, false, func(cfg map[string]any) { cfg["auto_exit"] = d.String() })
}

// SetLocalKindProfile sets key's machine-local [kind_profiles] entry to name, or
// deletes it when name is "" (Spec FR31-33/FR38, ADR 0139): key must be a routing
// kind or default. The table is dropped once empty; a non-table value (an inline
// table decodes as one) starts a fresh table. A malformed local file is refused.
func SetLocalKindProfile(root, key, name string) error {
	if _, err := registry.Lookup(contract.Kind(key)); err != nil && key != "default" {
		return fmt.Errorf("kind profile key %q is neither a kind nor default", key)
	}
	return editLocal(root, true, func(cfg map[string]any) {
		kp, ok := cfg["kind_profiles"].(map[string]any)
		if !ok {
			kp = map[string]any{}
		}
		if name == "" {
			delete(kp, key)
		} else {
			kp[key] = name
		}
		if len(kp) == 0 {
			delete(cfg, "kind_profiles")
		} else {
			cfg["kind_profiles"] = kp
		}
	})
}

// editLocal applies edit to the machine-local override and keeps the rest: it
// reads <git common dir of root>/mugicrew/config.toml, edits it, and writes the
// whole back to a temp file in that dir, creating it if needed, renamed over
// config.toml, so a reader never sees a half-written file. No worktree, ref or
// lock is touched, and a racing second writer simply wins last. A file that
// doesn't parse is refused when refuseMalformed, else replaced by the edit alone.
func editLocal(root string, refuseMalformed bool, edit func(cfg map[string]any)) error {
	common, err := git.CommonDirOf(root)
	if err != nil {
		return err
	}
	dir := filepath.Join(common, "mugicrew")
	path := filepath.Join(dir, "config.toml")
	cfg := map[string]any{}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if refuseMalformed && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", path, err)
		}
		// ponytail: a malformed file can't be kept, so a pool/auto_exit save replaces it.
		cfg = map[string]any{}
	}
	edit(cfg)
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

// ProfileMapping is the Agent Profile name a kind maps to and where it came from.
type ProfileMapping struct {
	Name   string // the catalog profile name; "" is the unnamed built-in
	Source string // "local" | "config.toml" | "default" (nothing mapped)
	Key    string // the [kind_profiles] key that set it: the kind or "default"; "" for the built-in
}

// KindProfile maps kind to its Agent Profile name from the [kind_profiles]
// table (ADR 0122 §2-3, ADR 0123 §7): the first set of local <kind>, shared
// <kind>, local default, shared default, else the built-in. Both layers are
// re-read every call. Each key stands alone: a key that is neither a routing
// kind nor default, or whose value isn't a string, logs config.unusable
// {path, key} and is ignored; a malformed file is a missing layer. Values are
// not checked against the catalog; the resolver does that.
func KindProfile(root string, kind contract.Kind) ProfileMapping {
	local, shared := KindProfiles(root)
	return ResolveProfile(local, shared, string(kind))
}

// KindProfiles reads both layers' [kind_profiles] maps in one pass, so each
// unusable key warns once per call however many rows are then resolved.
func KindProfiles(root string) (local, shared map[string]string) {
	return readKindProfiles(cfglayer.LocalPath(root)), readKindProfiles(cfglayer.SharedPath(root))
}

// ResolveProfile is the ADR 0122 §3 chain over the two layers' maps: the first
// set of local key, shared key, local default, shared default, else the
// built-in. Resolving with key deleted from local is what an unset would expose.
func ResolveProfile(local, shared map[string]string, key string) ProfileMapping {
	for _, k := range []string{key, "default"} {
		if name, ok := local[k]; ok {
			return ProfileMapping{Name: name, Source: "local", Key: k}
		}
		if name, ok := shared[k]; ok {
			return ProfileMapping{Name: name, Source: "config.toml", Key: k}
		}
	}
	return ProfileMapping{Source: "default"}
}

// readKindProfiles decodes one file's [kind_profiles] table, dropping each
// unusable key alone; path "" or a missing/malformed file reads as empty.
func readKindProfiles(path string) map[string]string {
	var cfg struct {
		KindProfiles map[string]any `toml:"kind_profiles"`
	}
	if cfglayer.Decode(path, &cfg) != cfglayer.Set {
		return nil
	}
	out := map[string]string{}
	for key, v := range cfg.KindProfiles {
		name, ok := v.(string)
		if _, err := registry.Lookup(contract.Kind(key)); (err != nil && key != "default") || !ok {
			slog.Warn("config.unusable", "path", path, "key", key)
			continue
		}
		out[key] = name
	}
	return out
}

// readConcurrency decodes concurrency from one config file; path "" is missing.
func readConcurrency(path string) (int, cfglayer.State) {
	var cfg struct{ Concurrency *int }
	if state := cfglayer.Decode(path, &cfg); state != cfglayer.Set {
		return 0, state
	}
	if cfg.Concurrency == nil {
		return 0, cfglayer.Missing
	}
	if *cfg.Concurrency < 1 {
		slog.Warn("config.unusable", "path", path, "concurrency", *cfg.Concurrency)
		return 0, cfglayer.Unusable
	}
	return *cfg.Concurrency, cfglayer.Set
}

// readAutoExit decodes auto_exit from one config file; path "" is missing.
func readAutoExit(path string) (time.Duration, cfglayer.State) {
	var cfg struct {
		AutoExit *string `toml:"auto_exit"`
	}
	if state := cfglayer.Decode(path, &cfg); state != cfglayer.Set {
		return 0, state
	}
	if cfg.AutoExit == nil {
		return 0, cfglayer.Missing
	}
	d, err := time.ParseDuration(*cfg.AutoExit)
	if err != nil || d < 0 {
		slog.Warn("config.unusable", "path", path, "auto_exit", *cfg.AutoExit)
		return 0, cfglayer.Unusable
	}
	return d, cfglayer.Set
}

// readLandStrategy decodes land_strategy from one config file; path "" is missing.
func readLandStrategy(path string) (string, cfglayer.State) {
	var cfg struct {
		LandStrategy *string `toml:"land_strategy"`
	}
	if state := cfglayer.Decode(path, &cfg); state != cfglayer.Set {
		return "", state
	}
	if cfg.LandStrategy == nil {
		return "", cfglayer.Missing
	}
	if _, err := git.LandStrategyByName(*cfg.LandStrategy); err != nil {
		slog.Warn("config.unusable", "path", path, "land_strategy", *cfg.LandStrategy)
		return "", cfglayer.Unusable
	}
	return *cfg.LandStrategy, cfglayer.Set
}

// ReadAutoApprove is the shared [approvals] settings read, a var so tests fake it.
// An unresolved repo reads off, as the Initiatives read does. The TUI refresh and
// the one-shot verbs share it, so they agree with the loop.
var ReadAutoApprove = func() cfglayer.Approvals {
	root, err := git.RepoRoot()
	if err != nil {
		return cfglayer.Approvals{}
	}
	return cfglayer.AutoApproveSetting(root)
}

// StampAutoApprove stamps the registry's auto-approve snapshot from the shared
// setting, at the start of a one-shot verb or TUI refresh that reaches a mode
// predicate (Spec D4).
func StampAutoApprove() { registry.SetAutoApprove(ReadAutoApprove().AutoApprove) }
