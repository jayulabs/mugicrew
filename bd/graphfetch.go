package bd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// fetchMarker is written into a Scratch graph dir; only a dir carrying it is
// removed and re-cloned by the next fetch (ADR 0218).
const fetchMarker = ".mugicrew-graph-fetch"

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`) // it lands in SQL and a path; not a valid Dolt database name (dotted usernames), so keep the two apart

var dbNameRE = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// NoGraphRef is FetchGraph's refusal: the Forge has no Graph ref for Name, so
// it was never pushed. Nothing was written.
type NoGraphRef struct{ Name, Ref, Forge string }

func (e *NoGraphRef) Error() string {
	return fmt.Sprintf("bd: no Graph ref %s on the Forge: %s never pushed one; list graphs with git ls-remote %s 'refs/mugicrew/graphs/*'", e.Ref, e.Name, e.Forge)
}

// scratchRun is one bd call against the Scratch graph in dir. BEADS_DIR is set
// to <dir>/.beads in its own env, never inherited, and DOLT_REMOTE_INFO_BRANCH=
// keeps the scratch server off the Forge's shared branch. A var for tests.
var scratchRun = func(dir string, args ...string) (string, error) {
	cmd := exec.Command("bd", args...)
	cmd.Dir = dir
	cmd.Env = append(withoutEnv(os.Environ(), "BEADS_DIR", "DOLT_REMOTE_INFO_BRANCH"),
		"BEADS_DIR="+filepath.Join(dir, ".beads"), "DOLT_REMOTE_INFO_BRANCH=")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("bd %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("bd %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func withoutEnv(env []string, keys ...string) []string {
	var out []string
next:
	for _, kv := range env {
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				continue next
			}
		}
		out = append(out, kv)
	}
	return out
}

// resolveExisting evaluates symlinks in the longest existing prefix of p, so a
// not-yet-created dir under a symlinked parent still resolves.
func resolveExisting(p string) string {
	p, _ = filepath.Abs(p)
	for rest := ""; ; {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest, p = filepath.Join(filepath.Base(p), rest), parent
	}
}

// within reports whether dir is path or sits under it.
func within(dir, path string) bool {
	dir, path = resolveExisting(dir), resolveExisting(path)
	rel, err := filepath.Rel(path, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// FetchGraph clones name's Graph ref into a Scratch graph at dir, readable with
// plain bd, and leaves no server running. keep lists dirs the Scratch graph must
// not equal or sit under (the Workspace root, the caller's BEADS_DIR). A dir
// carrying an earlier fetch's marker is removed and re-cloned; any other
// non-empty dir is refused. A missing ref is *NoGraphRef.
func FetchGraph(name, dir string, keep ...string) (err error) {
	if !usernameRE.MatchString(name) {
		return fmt.Errorf("bd: %q is not a GitLab username", name)
	}
	for _, k := range keep {
		if k != "" && within(dir, k) {
			return fmt.Errorf("bd: refusing Scratch graph dir %s: it is, or sits inside, %s", dir, k)
		}
	}
	ref := GraphRefOf(name)
	db := dbNameRE.ReplaceAllString(name, "_") // Dolt rejects '.' in a database name
	raw, err := forgeURL()
	if err != nil {
		return fmt.Errorf("bd: Forge remote URL: %w", err)
	}
	if has, err := forgeHasRef(ref, nil); err != nil {
		return fmt.Errorf("bd: list %s on the Forge: %w", ref, err)
	} else if !has {
		return &NoGraphRef{Name: name, Ref: ref, Forge: raw}
	}
	if ents, err := os.ReadDir(dir); err == nil && len(ents) > 0 {
		if _, err := os.Stat(filepath.Join(dir, fetchMarker)); err != nil {
			return fmt.Errorf("bd: refusing Scratch graph dir %s: not empty and not an earlier graph fetch", dir)
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Marked up front so a fetch that fails midway is re-cloned, not stranded.
	if err := os.WriteFile(filepath.Join(dir, fetchMarker), nil, 0o644); err != nil {
		return err
	}
	// No git init: bd init here sets BEADS_DIR explicitly, so an unrelated git
	// repo above dir is not touched (the e2e runs in a non-git temp dir).
	sr := func(args ...string) (string, error) { return scratchRun(dir, args...) }
	defer func() {
		if _, serr := sr("dolt", "stop"); err == nil {
			err = serr
		}
	}()
	if _, err := sr("init", "--server", "--non-interactive", "-p", "scratch"); err != nil {
		return err
	}
	if out, err := sr("config", "get", "issue_prefix"); err != nil || out != "scratch" {
		return fmt.Errorf("bd: Scratch graph in %s reads issue_prefix %q (%v), want scratch; refusing to go on", dir, out, err)
	}
	if _, err := sr("dolt", "stop"); err != nil {
		return err
	}
	if _, err := sr("dolt", "start"); err != nil {
		return err
	}
	if _, err := sr("sql", "CALL DOLT_CLONE('--ref','"+ref+"','"+DoltGitURL(raw)+"','"+db+"')"); err != nil {
		return err
	}
	out, err := sr("sql", "--csv", "SELECT value FROM `"+db+"`.metadata WHERE `key`='_project_id'")
	if err != nil {
		return err
	}
	f := strings.Fields(out) // header, then the id
	if len(f) != 2 {
		return fmt.Errorf("bd: cloned %s has no _project_id: %q", ref, out)
	}
	if err := repointMetadata(filepath.Join(dir, ".beads", "metadata.json"), db, f[1]); err != nil {
		return err
	}
	if _, err := sr("migrate", "schema"); err != nil { // leases, events… never ride the ref
		return err
	}
	return setReadonly(filepath.Join(dir, ".beads", "config.yaml"))
}

// setReadonly appends bd's own readonly key, after migrate schema (itself a
// write), so bd refuses every later write to the Scratch graph.
func setReadonly(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	return os.WriteFile(path, append(b, "readonly: true\n"...), 0o644)
}

// repointMetadata aims the Scratch graph's metadata.json at the cloned database
// and its project id, else bd reports PROJECT IDENTITY MISMATCH.
func repointMetadata(path, database, projectID string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("bd: read %s: %w", path, err)
	}
	m["dolt_database"], m["project_id"] = database, projectID
	if b, err = json.MarshalIndent(m, "", "  "); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
