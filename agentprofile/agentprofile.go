// Package agentprofile reads the OE's Agent Profile catalog (root ADR 0093): a
// checked-in <OE root>/agent-profiles.toml under a machine-local
// <git-common-dir>/agent-profiles.toml. It knows nothing of mugicrew's kinds or
// harnesses, so it can later lift into a shared harness Library.
package agentprofile

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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

// Load reads both catalog layers fresh, never cached: the checked-in
// agent-profiles.toml at $OE_ROOT (else workspaceRoot), then the local one in
// commonDir ("" is a missing layer). A local profile replaces a same-name
// checked-in one whole. A layer that is malformed, has a profile without
// harness, or has an unknown key is dropped whole and logs
// agent-profiles.unusable {file, reason}; a missing file is silent.
func Load(workspaceRoot, commonDir string) map[string]Profile {
	oe := os.Getenv("OE_ROOT")
	if oe == "" {
		oe = workspaceRoot
	}
	out := map[string]Profile{}
	for _, dir := range []string{oe, commonDir} {
		if dir == "" {
			continue
		}
		file := filepath.Join(dir, "agent-profiles.toml")
		layer, err := readLayer(file)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("agent-profiles.unusable", "file", file, "reason", err.Error())
			}
			continue
		}
		for name, p := range layer {
			out[name] = p
		}
	}
	return out
}

// readLayer strictly decodes one catalog file: every key must be known and
// every profile must name its harness.
func readLayer(file string) (map[string]Profile, error) {
	var cfg struct {
		Profiles map[string]struct {
			Harness, Model string
			Env            map[string]string
		}
	}
	md, err := toml.DecodeFile(file, &cfg)
	if err != nil {
		return nil, err
	}
	if un := md.Undecoded(); len(un) > 0 {
		return nil, fmt.Errorf("unknown key %s", un[0])
	}
	out := map[string]Profile{}
	for name, p := range cfg.Profiles {
		if p.Harness == "" {
			return nil, fmt.Errorf("profile %s has no harness", name)
		}
		out[name] = Profile{Name: name, Harness: p.Harness, Model: p.Model, Env: p.Env}
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
