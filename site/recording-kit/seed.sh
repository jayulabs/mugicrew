#!/usr/bin/env bash
# Builds the demo fixture the tapes record: a scratch git repo with its own
# bd store (prefix demo). Usage: seed.sh <dir>
# <dir> must be short: the TUI's tmux socket lives at <dir>/.git/mugicrew/tmux.sock
# and unix sockets are capped at 103 bytes. Bead ids are random on every run,
# so tapes navigate by keys and position, never by id.
set -euo pipefail
D=${1:?usage: seed.sh <dir>}
[ -e "$D" ] && { echo "refusing: $D exists"; exit 1; }
mkdir -p "$D"; cd "$D"; D=$PWD
export BEADS_DIR=$D/.beads # never the real Bead Graph
git init -q -b main
git -c user.name=demo -c user.email=demo@example.invalid commit -q --allow-empty -m "demo: empty app"
mugicrew init --prefix demo >/dev/null
[ "$(bd config get issue_prefix)" = demo ] || { echo "wrong bead store"; exit 1; }

c() { bd create --silent --no-inherit-labels "$@"; }
dep() { bd dep add "$1" "$2" >/dev/null; }
shut() { bd close "$1" --reason "$2" -q; }

# Inbox: two Signals waiting for triage.
c --type task -l kind:signal --title "Login page flashes unstyled on slow networks" --description "Seen on 3G." >/dev/null
c --type task -l kind:signal --title "Dark mode for the dashboard" --description "Several users asked." >/dev/null

# A Chart mid-defog: one question answered, two still fog.
s1=$(c --type task -l kind:signal --title "Export invoices as CSV" --description "Finance wants a CSV of the month's invoices.")
ch=$(c --type epic -l kind:chart --title "Invoice export" --description "## Destination

A Spec for a CSV export of a month's invoices.

## Notes

## Decisions-so-far

## Not-yet-specified

- Which columns finance needs
- Where the export button lives

## Out-of-scope
")
dep "$ch" "$s1"; shut "$s1" "charted: $ch"
git branch "chart/$ch" main # the Chart's container branch, which a launch from the Defog tab cuts its effort from
c --type task -p 1 -l kind:grilling --parent "$ch" --title "Which columns does finance need?" --description "Ask finance." >/dev/null
c --type task -l kind:prototype --parent "$ch" --title "Where the export button lives" --description "Try three spots." >/dev/null
r1=$(c --type task -l kind:research --parent "$ch" --title "Streaming CSV in the current web framework" --description "Look it up.")
shut "$r1" "researched: stream with chunked responses"

# One chain, Signal → Chart → Spec → Checkpoint, with Builds b1 → b2 → b3, b1 done.
s4=$(c --type task -l kind:signal --title "Rate-limit the public API" --description "Abuse spike last week.")
ch2=$(c --type epic -l kind:chart --title "API rate limiting" --description "## Destination

A Spec for per-key rate limits.")
dep "$ch2" "$s4"; shut "$s4" "charted: $ch2"
sp=$(c --type task -l kind:spec --title "Per-key token bucket on the public API" --description "Spec body.")
dep "$sp" "$ch2"; shut "$ch2" "specced: $sp"
cp=$(c --type epic -l kind:checkpoint --title "Per-key token bucket on the public API" --description "Checkpoint.")
dep "$cp" "$sp"; shut "$sp" "accepted"
b1=$(c --type task -l kind:build --parent "$cp" --title "Token bucket store keyed by API key" --description "b")
b2=$(c --type task -l kind:build --parent "$cp" --title "429 responses with Retry-After" --description "b")
b3=$(c --type task -l kind:build --parent "$cp" --title "Limits in the admin settings page" --description "b")
dep "$b2" "$b1"; dep "$b3" "$b2"; shut "$b1" "built"

# A second, small Checkpoint with every Build closed and a pending Approval, so the
# Execute tab lists a Decision Packet waiting on the human with no live agent run.
sp2=$(c --type task -l kind:spec --title "Dark mode toggle in the dashboard" --description "Spec body.")
cp2=$(c --type epic -l kind:checkpoint --title "Dark mode toggle in the dashboard" --description "Checkpoint.")
dep "$cp2" "$sp2"; shut "$sp2" "accepted"
d1=$(c --type task -l kind:build --parent "$cp2" --title "Theme switch in the dashboard header" --description "b")
shut "$d1" "built"
c --type task -l kind:approval --parent "$cp2" --title "Approve: Dark mode toggle in the dashboard" --description "Commit: 1a2b3c4d5e6f1a2b3c4d5e6f1a2b3c4d5e6f1a2b

## Background

Several users asked for a dark mode. The Spec adds a theme switch to the dashboard header that remembers the choice per browser.

## Summary

| Change | Where | Why |
|---|---|---|
| Theme switch | dashboard header | One click flips light and dark |
| Saved choice | browser storage | The choice survives a reload |

## Evidence

The clean Review certified commit 1a2b3c4. The dashboard tests pass.

## Merge Danger

Two-way door. Blast radius: the dashboard only.

## Execution Notes

None.

## Approval History

First Approval on this Checkpoint." >/dev/null

# Stats fixture: finished sessions in the event log (what the Stats tab sums),
# spread over the last week so the default 7d window holds them. Fake ids and
# costs only; nothing from a real run.
LOG=$D/.git/mugicrew/events.jsonl
mkdir -p "$(dirname "$LOG")"
ev() { # <hours-ago> <bead> <kind> <crewmate> <dur_ms> <api_ms> <cost_usd>
  printf '{"time":"%s","level":"INFO","msg":"session.ended","bead":"%s","kind":"%s","crewmate":"%s","dur_ms":%s,"api_ms":%s,"cost_usd":%s}\n' \
    "$(date -u -d "$1 hours ago" +%Y-%m-%dT%H:%M:%S.000000000Z)" "$2" "$3" "$4" "$5" "$6" "$7" >> "$LOG"
}
ev 150 "$s1" signal robin 180000 90000 0.42
ev 120 "$ch" chart nami 540000 300000 1.85
ev 96 "$ch2" chart nami 480000 260000 1.60
ev 72 "$sp" spec usopp 420000 240000 1.30
ev 48 "$b1" build franky 900000 600000 3.10
ev 30 "$b1" build franky 600000 380000 2.05
ev 12 "$b2" build franky 780000 520000 2.70
ev 6 "$b2" review zoro 300000 180000 0.95
echo "seeded $D"
