# Robin's read-only is a --disallowedTools flag on her argv, not a sandbox

Robin's sessions are read-only because her launch and resume argv carry
`--disallowedTools Edit,Write,NotebookEdit`. Claude Code then drops those tools
from the session entirely. Prototyping showed this (mc-4ryp.2.3, side ref
refs/mugicrew/prototypes/mc-4ryp.2): Bash still runs `bd show`, `git log` and
`bd create -s deferred`, which is what Robin needs. The flag is set by the code
that launches Robin, whatever her prompt says. It is not set in her Agent
Profile, since a Profile holds only harness, model and env (root ADR 0093) and
the Chart rules out a new config key. `ResumeCommand` builds its argv
separately, so it must carry the flag too, or a resumed Robin gets the edit
tools back.

## Considered Options

- A `permissions.deny` rule merged into the `--settings` JSON that HITL launches
  already pass (ADR 0099) blocked the tools just as well. We rejected it because
  the flag is visible in `ps`, and Robin's launch would otherwise need its own
  copy of the waiting-hooks JSON.
- A real sandbox (no Bash, or Bash limited to `bd`/`git` patterns) was rejected.
  Robin needs Bash to read the Workspace, and a pattern allow-list is easy to
  get around through `bd`/`git` themselves.

## Consequences

Read-only is a guardrail, not a security boundary. The accepted ceiling is that
Bash can still write anything: `echo > file`, `sed -i` and `git commit` all
work. So can any write-capable MCP tools the user has connected, plus WebFetch.
Nothing past the prompt stops Robin from writing; a human approving Bash prompts
in the TTY is the backstop.
