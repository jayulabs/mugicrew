// Package agentprofile reads Agent Profile tables (root ADR 0093): the OE
// catalog's two agent-profiles.toml files strictly, and [profiles.*] tables in
// any other TOML file leniently, and merges ordered layers first-wins. It is
// path-agnostic beyond the OE pair, and knows nothing of mugicrew's kinds or
// harnesses, so it can later lift into a shared harness Library.
package agentprofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/BurntSushi/toml"
)

// Profile is one named harness/model/env choice. Env values are unexpanded.
type Profile struct {
	Name, Harness, Model string
	Env                  map[string]string
}

// Entry is one declared name's winning table: its Profile and the file it came
// from. Err is non-nil for a bad config.toml table, which still shadows the
// name as unrunnable; Profile.Name is set either way.
type Entry struct {
	Profile Profile
	Path    string
	Err     error
}

// FileError is an unrunnable reason that carries the winning file's path, so an
// event site can log it without parsing the reason text. Its text is Err's.
type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string { return e.Err.Error() }
func (e *FileError) Unwrap() error { return e.Err }

// ErrPath is the winning file's path carried by err (or an error it wraps), ""
// when it carries none: an undeclared name has no file.
func ErrPath(err error) string {
	var fe *FileError
	if errors.As(err, &fe) {
		return fe.Path
	}
	return ""
}

// ReasonAttrs are profile.unusable's trailing attrs: path (omitted when err
// names no file), reason, launch.
func ReasonAttrs(err error, launch string) []any {
	var attrs []any
	if p := ErrPath(err); p != "" {
		attrs = append(attrs, "path", p)
	}
	return append(attrs, "reason", err.Error(), "launch", launch)
}

// Layer is one file's read: its usable profiles and, per name, the tables that
// failed to decode. Merge takes layers highest-precedence first.
type Layer struct {
	Path     string
	Profiles map[string]Profile
	Bad      map[string]error
}

// Merge is first-wins over layers (earlier outranks later): the winning table
// replaces every lower same-name table whole, bad or good. Readers never log.
func Merge(layers ...Layer) map[string]Entry {
	out := map[string]Entry{}
	for _, l := range layers {
		for name, p := range l.Profiles {
			if _, ok := out[name]; !ok {
				out[name] = Entry{Profile: p, Path: l.Path}
			}
		}
		for name, err := range l.Bad {
			if _, ok := out[name]; !ok {
				out[name] = Entry{Profile: Profile{Name: name}, Path: l.Path, Err: err}
			}
		}
	}
	return out
}

// OEPaths are the two OE catalog files: the checked-in agent-profiles.toml at
// $OE_ROOT (else workspaceRoot) and the machine-local one in commonDir ("" is a
// missing local layer, returned as "").
func OEPaths(workspaceRoot, commonDir string) (shared, local string) {
	oe := os.Getenv("OE_ROOT")
	if oe == "" {
		oe = workspaceRoot
	}
	shared = filepath.Join(oe, "agent-profiles.toml")
	if commonDir != "" {
		local = filepath.Join(commonDir, "agent-profiles.toml")
	}
	return shared, local
}

// ReadStrict decodes one OE catalog file whole: every key must be known and
// every profile must name its harness, else the whole file is the error (a
// missing file wraps fs.ErrNotExist).
func ReadStrict(file string) (Layer, error) {
	var cfg struct {
		Profiles map[string]struct {
			Harness, Model string
			Env            map[string]string
		}
	}
	md, err := toml.DecodeFile(file, &cfg)
	if err != nil {
		return Layer{}, err
	}
	if un := md.Undecoded(); len(un) > 0 {
		return Layer{}, fmt.Errorf("unknown key %s", un[0])
	}
	out := Layer{Path: file, Profiles: map[string]Profile{}}
	for name, p := range cfg.Profiles {
		if p.Harness == "" {
			return Layer{}, fmt.Errorf("profile %s has no harness", name)
		}
		out.Profiles[name] = Profile{Name: name, Harness: p.Harness, Model: p.Model, Env: p.Env}
	}
	return out, nil
}

// ErrProfilesNotTable is ReadTables' file-level error for a profiles key that
// isn't a table; the file then contributes no names.
var ErrProfilesNotTable = errors.New("profiles is not a table")

// ReadTables reads the [profiles.*] tables of any TOML file leniently: keys
// outside profiles are ignored, and each table is checked alone (unknown key,
// missing harness or wrong type make just that name bad, keyed by name). A
// syntax error or a non-table profiles is the file's error (a missing file wraps
// fs.ErrNotExist). Never logs.
func ReadTables(file string) (Layer, error) {
	var raw map[string]any
	if _, err := toml.DecodeFile(file, &raw); err != nil {
		return Layer{}, err
	}
	out := Layer{Path: file, Profiles: map[string]Profile{}, Bad: map[string]error{}}
	rawProfiles, ok := raw["profiles"]
	if !ok {
		return out, nil
	}
	if _, ok := rawProfiles.(map[string]any); !ok {
		return Layer{}, ErrProfilesNotTable
	}
	var top struct{ Profiles map[string]toml.Primitive }
	md, err := toml.DecodeFile(file, &top)
	if err != nil {
		return Layer{}, err
	}
	type table struct {
		Harness, Model string
		Env            map[string]string
	}
	tables := map[string]table{}
	for name, prim := range top.Profiles {
		var t table
		if err := md.PrimitiveDecode(prim, &t); err != nil {
			out.Bad[name] = err
			continue
		}
		tables[name] = t
	}
	unknown := map[string]string{}
	for _, k := range md.Undecoded() {
		if len(k) > 2 && k[0] == "profiles" {
			if _, seen := unknown[k[1]]; !seen {
				unknown[k[1]] = toml.Key(k[2:]).String()
			}
		}
	}
	for name, t := range tables {
		switch {
		case unknown[name] != "":
			out.Bad[name] = fmt.Errorf("unknown key %s", unknown[name])
		case t.Harness == "":
			out.Bad[name] = errors.New("no harness")
		default:
			out.Profiles[name] = Profile{Name: name, Harness: t.Harness, Model: t.Model, Env: t.Env}
		}
	}
	return out, nil
}

// Resolution is a runnable profile: its name ("" = an Application's unnamed
// built-in), harness, model and env with every ${VAR} expanded.
type Resolution struct {
	Profile, Harness, Model string
	Env                     map[string]string
}

var varRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Expand expands every ${VAR} in env from lookupEnv (root ADR 0093 §2 as
// amended by mugicrew ADR 0123): set-but-empty expands to "", unset is an error
// naming the variable. Nothing else in a value is touched.
func Expand(env map[string]string, lookupEnv func(string) (string, bool)) (map[string]string, error) {
	var out map[string]string
	for k, v := range env {
		var unset string
		v = varRef.ReplaceAllStringFunc(v, func(ref string) string {
			name := ref[2 : len(ref)-1]
			val, ok := lookupEnv(name)
			if !ok && unset == "" {
				unset = name
			}
			return val
		})
		if unset != "" {
			return nil, fmt.Errorf("env %s names unset ${%s}", k, unset)
		}
		if out == nil {
			out = map[string]string{}
		}
		out[k] = v
	}
	return out, nil
}
