package bd

import (
	"encoding/csv"
	"fmt"
	"strings"

	"jayulabs.org/mugicrew/git"
)

// The Graph ref (ADR 0217): the Bead Graph's real Dolt data on the Forge at
// refs/mugicrew/graphs/<graph-name>, reached through a SQL-added Dolt remote
// that sits beside bd's own sync (sync.remote, refs/dolt/data) and never
// touches it. Server mode only: bd sql needs it.
const (
	graphRemote  = "forge-graph" // any name but origin; bd only touches sync.remote for origin
	DoltInfoRef  = "refs/heads/__dolt_remote_info__"
	graphRefBase = "refs/mugicrew/graphs/"
)

// Seams: the Forge's URL and ref listing, faked in tests.
var (
	forgeURL    = git.ForgeURL
	forgeHasRef = git.ForgeHasRefEnv
)

// GraphRefOf is the Graph ref for a Graph name.
func GraphRefOf(name string) string { return graphRefBase + name }

// DoltGitURL rewrites a Forge remote URL to Dolt's git-remote form: scp-style
// git@host:path → git+ssh://git@host/path, ssh → git+ssh, http(s) → git+http(s).
func DoltGitURL(u string) string {
	switch {
	case strings.HasPrefix(u, "git+"):
		return u
	case strings.Contains(u, "://"):
		return "git+" + u
	}
	if host, path, ok := strings.Cut(u, ":"); ok {
		return "git+ssh://" + host + "/" + strings.TrimPrefix(path, "/")
	}
	return u
}

// runWith is run, or runEnv when the Graph push carries env entries.
func runWith(env []string, args ...string) (string, error) {
	if env == nil {
		return run(args...)
	}
	return runEnv(env, args...)
}

// sqlRows runs q against the graph and returns its CSV rows below the header.
func sqlRows(env []string, q string) ([][]string, error) {
	out, err := runWith(env, "sql", "--csv", q)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[1:], nil
}

// ensureGraphRemote makes forge-graph exist with the Forge's URL and ref: the
// same url+ref is left alone; a changed one (username rename, changed Forge) is
// removed then re-added. It never deletes a ref on the Forge.
func ensureGraphRemote(ref, raw string, env []string) error {
	url := DoltGitURL(raw)
	rows, err := sqlRows(env, "SELECT url, JSON_UNQUOTE(JSON_EXTRACT(params, '$.git_ref')) FROM dolt_remotes WHERE name='"+graphRemote+"'")
	if err != nil {
		return fmt.Errorf("bd: read dolt_remotes: %w", err)
	}
	if len(rows) == 1 && len(rows[0]) == 2 {
		if rows[0][0] == url && rows[0][1] == ref {
			return nil
		}
		if _, err := runWith(env, "sql", "CALL DOLT_REMOTE('remove','"+graphRemote+"')"); err != nil {
			return err
		}
	}
	_, err = runWith(env, "sql", "CALL DOLT_REMOTE('add','"+graphRemote+"','"+url+"','--ref','"+ref+"')")
	return err
}

// ForeignGraphRef is GraphPush's refusal: the Graph ref holds another Bead
// Graph's data (ForeignLanding's shape). Nothing was pushed.
type ForeignGraphRef struct {
	Ref, Ours, Theirs string
}

func (e *ForeignGraphRef) Error() string {
	return fmt.Sprintf("bd: %s holds Bead Graph %s, not ours (%s); another account's ref? check glab auth status — mugicrew will not overwrite or delete it",
		e.Ref, e.Theirs, e.Ours)
}

// GraphPushResult is a completed Graph push: the ref, and a Warning (not an
// error) when the Forge carries the shared __dolt_remote_info__ branch.
type GraphPushResult struct {
	Ref, Warning string
}

// remoteRootSQL is ADR 0217 §3: the fetched ref's root commit.
const remoteRootSQL = "SELECT commit_hash FROM dolt_log('remotes/" + graphRemote + "/main') WHERE commit_hash IN (SELECT commit_hash FROM dolt_commit_ancestors WHERE parent_hash IS NULL)"

// GraphPush pushes this Bead Graph to the Graph ref for name. It force-pushes
// only when the ref is absent or its root commit is our Graph id (so a compacted
// graph gets through and a foreign one never does); otherwise it refuses with
// *ForeignGraphRef and makes no push. There is no override.
//
// Every call carries glab's credential helper for an http(s) Forge (ADR 0256).
// If the fetch or the push fails it restarts bd's sql-server under that env and
// retries the body once; the second failure is returned as is.
func GraphPush(name string) (GraphPushResult, error) {
	if !usernameRE.MatchString(name) {
		return GraphPushResult{}, fmt.Errorf("bd: %q is not a GitLab username", name)
	}
	raw, err := forgeURL()
	if err != nil {
		return GraphPushResult{Ref: GraphRefOf(name)}, fmt.Errorf("bd: Forge remote URL: %w", err)
	}
	env := git.GlabHelperEnv(raw)
	res, restart, err := graphPushOnce(name, raw, env)
	if err == nil || !restart || env == nil {
		return res, err
	}
	if _, rerr := runWith(env, "dolt", "stop"); rerr != nil {
		return res, fmt.Errorf("%w; restart: %v", err, rerr)
	}
	if _, rerr := runWith(env, "dolt", "start"); rerr != nil {
		return res, fmt.Errorf("%w; restart: %v", err, rerr)
	}
	res, _, err = graphPushOnce(name, raw, env)
	return res, err
}

// graphPushOnce is GraphPush's body; restart reports a fetch or push failure,
// the server-credential failures a restart can cure.
func graphPushOnce(name, raw string, env []string) (res GraphPushResult, restart bool, err error) {
	ref := GraphRefOf(name)
	res = GraphPushResult{Ref: ref}
	ours, err := git.GraphID()
	if err != nil {
		return res, false, err
	}
	if err := ensureGraphRemote(ref, raw, env); err != nil {
		return res, false, err
	}
	exists, err := forgeHasRef(ref, env)
	if err != nil {
		return res, false, fmt.Errorf("bd: list %s on the Forge: %w", ref, err)
	}
	if exists {
		if _, err := runWith(env, "sql", "CALL DOLT_FETCH('"+graphRemote+"')"); err != nil {
			return res, true, err
		}
		rows, err := sqlRows(env, remoteRootSQL)
		if err != nil {
			return res, false, err
		}
		if len(rows) != 1 || len(rows[0]) != 1 {
			return res, false, fmt.Errorf("bd: %s has %d Dolt root commits, want exactly 1", ref, len(rows))
		}
		if theirs := rows[0][0]; theirs != ours {
			return res, false, &ForeignGraphRef{Ref: ref, Ours: ours, Theirs: theirs}
		}
	}
	if _, err := runWith(env, "dolt", "push", "--remote", graphRemote, "--force"); err != nil {
		return res, true, err
	}
	if has, err := forgeHasRef(DoltInfoRef, env); err == nil && has {
		res.Warning = DoltInfoRef + " exists on the Forge: set DOLT_REMOTE_INFO_BRANCH= in .beads/.env via mugicrew init (delete the branch by hand if wanted)"
	}
	return res, false, nil
}
