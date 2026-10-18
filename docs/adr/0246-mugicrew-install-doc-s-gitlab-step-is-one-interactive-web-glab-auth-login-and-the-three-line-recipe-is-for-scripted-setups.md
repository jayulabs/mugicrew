# ADR 0246: install.md's GitLab step is one interactive web `glab auth login`, and the three-line recipe is for scripted setups

Status: Accepted
Date: 2026-10-16

Chart mc-uqpj.2 (Standalone install and bootstrap of mugicrew in a client
Workspace). Settles prototype mc-uqpj.2.10. It narrows ADR 0247
(gitlab-https-credentials-are-glab-s-helper-with-its-user-set…), which probed
only the non-interactive `--stdin` login. The probe ran on a clean HOME with
glab 1.107.0, git 2.55.0 and gitlab.com, and is kept on
`refs/mugicrew/prototypes/mc-uqpj.2`
(`apps/mugicrew/docs/prototype-gitlab-interactive-login/`).

## Decision

1. **install.md's GitLab section is one command:** an interactive
   `glab auth login` where you pick gitlab.com, **Web**, HTTPS, and answer
   **Yes** to "Authenticate Git with your GitLab credentials?". That alone:
   - writes the global `credential.https://gitlab.com.helper` pair (an empty
     reset, then `!<absolute path to glab> auth git-credential`);
   - sets glab's `user` for the host;
   - stores an OAuth token, which the helper fills as `username=oauth2`.

   With nothing else configured, `git clone`, then pushing and deleting a
   branch over https, all worked with no prompts.
2. **The three-line recipe from ADR 0247 stays, for scripted or non-interactive
   setups only.** That is `--stdin`/`--token` login, the helper line, and
   `glab config set user`. The "token scope: `api` alone" note belongs with
   this recipe. The web login uses glab's OAuth app, so no scope is chosen
   there.
3. **A machine that is already logged in to glab skips the step.** The first
   client project's humans already have glab authenticated. install.md says to
   run doctor first. Only if doctor's glab-user or credential-fill check fails
   do you run the interactive login (or, in a script, the recipe).

## Consequences

- The helper is written as glab's absolute path. Moving or reinstalling glab
  under a new path, such as a Nix store path or a package-manager prefix
  change, silently breaks git over https while glab itself still works.
  doctor's dry-run credential fill catches this. The fix is to re-run the login.
- An existing glab login made with `--stdin`/`--token` has neither the helper
  nor the user set. doctor flags it the same way.
- The interactive **Token** path (pasting a PAT) was not probed. install.md
  points at Web.
