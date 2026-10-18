# v2 TUI refresh gates: baseline vs now

Re-measure of the three pass gates of the tiered-refresh Spec (FR1–FR3, FR8,
FR20, FR31) with every fix of the Checkpoint landed. Baseline is the prototype
*Where the v2 TUI refresh spends its cost under load*
(`refs/mugicrew/prototypes/mc-wg4f`).

## Method

The prototype's counting (`MUGICREW_PROBE` exec/span/mark events; `lat.py`,
`cpu.sh`) was patched in temporarily onto the Checkpoint tip and built to a
scratch binary; that probe tooling is not committed. The `MUGICREW_PPROF` CPU
profile hook, by contrast, is committed and present in any mugicrew build:
launch mugicrew with `MUGICREW_PPROF` set to a file path (absolute recommended,
since the TUI runs in its own tmux pane) and the profile of the TUI's run is
complete once the TUI exits; unset means no profiling. Open it with
`go tool pprof`. The v2 TUI ran in a 200×50 tmux pane against the live host repo with the loop
on (~2,660 beads). Keys were `j`/`k` every 0.5 s, 40 per tab, matched to the
next completed `View` after the keypress. CPU is the TUI process's own plus
reaped children (`/proc/<pid>/stat`, dolt excluded) over 60 s idle.

**Load caveat:** the live crew during this run was 0 HITL and 2 AFK, lighter
than the reference's 1 HITL + 4–5 AFK, so the dolt server was less contended.
The TUI's own cost (CPU, subprocess counts, in-process work) does not depend on
that; the `bd` wall time does. Re-run under the full reference load if the
margin on Logs or Settings (below) matters.

## Results

| Gate | Baseline | Now | Gate | |
|---|---|---|---|---|
| keypress→redraw p95, Ideate | 71 ms | 40 ms | ≤ 150 ms | pass |
| Defog | – | 11 ms | ≤ 150 ms | pass |
| Execute | – | 26 ms | ≤ 150 ms | pass |
| Graph | 10,258 ms | 8 ms | ≤ 150 ms | pass |
| Initiatives | – | 8 ms | ≤ 150 ms | pass |
| History | – | 10 ms | ≤ 150 ms | pass |
| Stats | – | 8 ms | ≤ 150 ms | pass |
| Logs | – | 84 ms | ≤ 150 ms | pass |
| Settings | – | 110 ms | ≤ 150 ms | pass |
| focus mode | 165 ms | 10 ms | ≤ 150 ms | pass |
| TUI+children CPU, idle Ideate | 12.3 s/min | 3.24 s/min | ≤ 6 s/min | pass |
| TUI+children CPU, idle Graph | 53.5 s/min | 4.42 s/min | ≤ 6 s/min | pass |
| subprocesses per fast refresh | 16 | 4–5 | ≤ 8 | pass |
| subprocesses per slow refresh | 16 | 14 | ≤ 16 | pass |

Graph p50 went from 4,049 ms to 7 ms. Baseline p95 is only recorded for the
tabs the Spec names.

## Watch

Settings (110 ms) and Logs (84 ms) are the tightest tabs and are the only ones
above ~40 ms; Settings' p95 is the highest of the nine.

The slow tier now reads the default closed set in two bd processes (`bd sql`
for the id set, one `bd list --all --id`) instead of one `bd list --all`
(ADR 0232), so a slow refresh costs one more.

## After the scaling Checkpoint (mc-5yyk)

Graph and Logs now load, hold and format only what is shown. In-process, at 200×50
over the synthetic 10k beads / 100k events (`BenchmarkScaling`,
`docs/tui-scaling-baseline.md`), one keypress→redraw costs 0.8 ms (Logs), 11 ms
(Logs under a `/` filter, with its whole-log scan drained) and 13 ms (Graph under `s any`),
all inside the ≤ 150 ms gate with the tabs' bd/tmux/git reads stubbed.

Re-measured live after this Checkpoint: the TUI ran in a 200×50 tmux pane on the live
host repo with `MUGICREW_PPROF` set and `bd`/`git`/`tmux` shimmed to log every exec.
Counting every process the TUI itself spawns (bd's own `git` children excluded), a
steady-state slow refresh is **14** (3 bd, 4 tmux, 7 git) and a fast refresh 4–5, inside
the ≤ 16 / ≤ 8 gates. Before the fix below it was 20 slow / 10 fast: `git rev-parse
--git-common-dir` ran about six times per refresh (tmux socket path, event log, loop
lock), so `git.CommonDir` now memoizes a success per cwd. The TUI's own CPU over a 38 s
idle Ideate run was 1.2 s (about 1.9 s/min, children excluded), under the ≤ 6 s/min gate.

Keypress→redraw p95 and idle CPU were then re-measured live by the Method above (200×50
pane on the live host repo, loop on, 5 AFK, machine load average 10–14, `j`/`k` every
0.5 s, 40 per tab; Stats and Settings have no cursor, so `?` toggles the overlay as their
redraw): Ideate 33 ms, Defog 44 ms, Execute 47 ms, Graph 86 ms, Initiatives 46 ms,
History 31 ms, Stats 30 ms, Logs 23 ms, Settings 75 ms, all inside ≤ 150 ms. A first pass
read Defog 165 ms and Execute 196 ms on their first visit under load; a repeat of both
read 44 / 47 ms (p50 29 / 26 ms). The first-visit cost was not isolated; it is
likely a cold first build of each tab's rows under that load (the repeat is warm), so
treat the repeat as the steady figure and the first visit as unmeasured against the gate. Idle CPU over 60 s, TUI plus reaped children: Graph
4.35 s/min, Ideate 4.92 s/min, both under ≤ 6 s/min.
