package git

import (
	"errors"
	"os/exec"
	"strings"
)

// The read-only primitives the triage locate verb composes (ADR 0322). Each takes the
// ref it reads explicitly, so the verb can point them at the Forge's tracking ref.

// TrailerOn is the newest commit on ref whose message carries trailer as a whole
// line (LandedBase's regex), ok false when none does. It reads ref, never local main.
func (r Repo) TrailerOn(ref, trailer string) (sha string, ok bool, err error) {
	sha, err = r.run(trailerLogArgs(ref, trailer)...)
	return sha, err == nil && sha != "", err
}

// FetchTracking fetches the Forge's mainline into refs/remotes/<remote>/<main> and
// returns that ref's name. It never moves local main and takes no land lock.
func (r Repo) FetchTracking(remote string) (string, error) {
	tracking := "refs/remotes/" + remote + "/" + r.mainline()
	if _, err := r.runForge(remote, "fetch", "--quiet", remote, "+refs/heads/"+r.mainline()+":"+tracking); err != nil {
		return "", err
	}
	return tracking, nil
}

// ChangedOn reports whether path differs between base and branch's merge-base: the
// three-dot diff --name-only, i.e. the branch's own change. A git failure is the error.
// An empty path is the repo root.
func (r Repo) ChangedOn(base, branch, path string) (bool, error) {
	if path == "" {
		path = "." // the repo root
	}
	out, err := r.run("diff", "--name-only", base+"..."+branch, "--", path)
	return err == nil && out != "", err
}

// ereMeta are the POSIX ERE metacharacters. git log -G always compiles its pattern as
// a regex (-F touches only --grep/--author), so a literal token escapes each of them.
const ereMeta = `\.[]()*+?{}|^$`

// IntroducesToken reports whether a commit in base..branch adds or removes token,
// read literally (git log -G with token's regex metacharacters escaped). A git
// failure is the error.
func (r Repo) IntroducesToken(base, branch, token string) (bool, error) {
	var lit strings.Builder
	for _, c := range token {
		if strings.ContainsRune(ereMeta, c) {
			lit.WriteByte('\\')
		}
		lit.WriteRune(c)
	}
	out, err := r.run("log", "--format=%h", "-G", lit.String(), base+".."+branch)
	return err == nil && out != "", err
}

// GrepAt reports whether token appears in any file of ref (git grep -F). No match is
// false with a nil error; any other git failure is the error.
func (r Repo) GrepAt(ref, token string) (bool, error) {
	_, err := r.run("grep", "-q", "-F", "-e", token, ref)
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return err == nil, err
}

// PathAt reports whether path exists in ref's tree. An empty path is the root tree.
func (r Repo) PathAt(ref, path string) bool {
	_, err := r.run("cat-file", "-e", ref+":"+path)
	return err == nil
}

// ShortSha is sha cut to seven characters for a message.
func ShortSha(sha string) string {
	sha = strings.TrimSpace(sha)
	return sha[:min(7, len(sha))]
}
