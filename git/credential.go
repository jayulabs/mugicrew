package git

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"jayulabs.org/mugicrew/forge"
)

// ErrCredentialNA is CredentialFill's result for a non-http(s) remote: nothing to probe.
var ErrCredentialNA = errors.New("git: remote is not http(s); no credential to probe")

// CredentialFill reports whether this shell's git holds an https credential for the
// remote URL, without prompting (GIT_TERMINAL_PROMPT=0, no askpass). Stdout is discarded (a
// successful fill prints the password); a failure carries only git's stderr. It probes
// this shell's git and environment, not the bd server's.
func CredentialFill(remoteURL string) error { return CredentialFillEnv(remoteURL, "", nil) }

// CredentialFillEnv is CredentialFill run from dir (empty: the current dir) with extra
// env entries appended.
func CredentialFillEnv(remoteURL, dir string, env []string) error {
	u, err := url.Parse(remoteURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrCredentialNA
	}
	p, err := forge.ParseRemote(remoteURL)
	if err != nil {
		return err
	}
	cmd := exec.Command("git", "credential", "fill")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "LC_ALL=C", "GIT_ASKPASS=") // empty GIT_ASKPASS also stops the core.askPass/SSH_ASKPASS fallthrough
	cmd.Env = append(cmd.Env, env...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(fmt.Sprintf("protocol=%s\nhost=%s\npath=%s\n\n", u.Scheme, p.Host, p.Path()))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git credential fill: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
