# ADR 0256: The Graph push runs under an injected glab helper, and restarts bd's sql-server once on failure

Status: Superseded by ADR 0300
Date: 2026-10-17

Chart mc-uqpj.14 (Forge-ready Land MRs and an https-safe Graph ref push under
land_strategy=gitlab). Settles prototype mc-uqpj.14.2. A probe on a clean HOME
(glab 1.107.0, git 2.55.0, bd 1.3.0 server mode, dolt 2.3.4, gitlab.com, two
real accounts; kept on `refs/mugicrew/prototypes/mc-uqpj.14`) ran each leg of
`bd.GraphPush` with no global `~/.gitconfig` credential entry. Glab was logged in
and its `user` set, per ADR 0247
(mugicrew-gitlab-https-credentials-are-glab-s-helper…) §1 steps 1 and 3.

## Findings

| Leg | Runs in | Without a helper |
|---|---|---|
| `git ls-remote` (`forgeHasRef`) | mugicrew's env | fails |
| `CALL DOLT_REMOTE('add', …)` | sql-server | passes: offline |
| `CALL DOLT_FETCH` | sql-server | fails |
| `bd dolt push --remote forge-graph` | **sql-server** (SQL error 1105) | fails |

- The push goes through the sql-server too, not the dolt CLI in mugicrew's env.
  This corrects the Chart's code fact. So moving legs "off the sql-server" is
  not available. Injecting into the bd call alone does not fix fetch or push.
- bd does **not** scrub the server's env. A server started by an injected bd
  call carries `GIT_CONFIG_*` and `GITLAB_TOKEN`, and then fetch and push pass
  even from bare bd calls. They fail only because the running server was started
  by a caller without the injection (a human's bd, a session's).
- ADR 0216 (mugicrew-a-failed-graph-push-warns…) §6 holds as written: `pushGraph`
  calls `forgeUser` (glab) before `GraphPush`, so a missing glab login never
  reaches the push. The gap is a glab login with no git helper, which does reach
  it and is what this ADR closes.

## Decision

1. **Inject the helper only**, into the env of every git and bd call mugicrew's
   Graph push makes:
   `GIT_CONFIG_COUNT=1`,
   `GIT_CONFIG_KEY_0=credential.https://<host>.helper`,
   `GIT_CONFIG_VALUE_0=!glab auth git-credential`.
   Do **not** inject `credential.<url>.username`. Under `GITLAB_TOKEN` for a
   second account, injecting that account's username trips glab's
   "requested username doesn't match the one configured" refusal. The helper
   alone answers with glab's stored user and the active token, and GitLab
   accepts it for both accounts. glab's `user` must still be set (ADR 0247 §1
   step 3).
2. **Restart once on failure.** GraphPush runs as is. If `DOLT_FETCH` or the push
   fails, it restarts the sql-server once under the injection (`bd dolt stop`,
   then an injected `bd dolt start`) and retries. If the retry fails too, that is
   the push failure ADR 0216 warns about.
3. **A second account needs nothing new.** With `GITLAB_TOKEN` exported in the
   clone's shell (ADR 0247 §2), mugicrew's env carries it, and so does the server
   restarted under it. Every leg then authenticates as that account.
4. No token in the remote URL. mugicrew ships no helper of its own: it points
   git at glab's.

## Considered Options

- **Always restart before the push.** Rejected: it bounces the shared server for
  every session on every Landing.
- **Inject only at launch, never restart.** Rejected: a server started by a bare
  human bd call stays broken until something restarts it.
- **Inject glab's user as `credential.username` too.** Rejected: see 1.

## Consequences

A Graph push with a credential gap pays one server restart. Concurrent bd calls
see a brief outage, which `runIn`'s retry absorbs. Shared-server mode (one server
for many clones) cannot carry a per-clone `GITLAB_TOKEN`. That is untested and
outside this decision.
