# TUI Graph and Logs scaling: baseline

Where the Graph and Logs tabs spend their cost as closed beads and events grow,
measured before the bounded-by-what-is-shown Spec. The scaling gate (Chart *TUI
Graph and Logs tabs bounded by what is shown*) is read against these numbers.
Primary source: the prototype *Where Graph and Logs spend their cost as closed
beads and events grow* (`refs/mugicrew/prototypes/mc-1nvm`), whose throwaway
harness (`tui/zz_proto_scale_test.go`) and report are not committed.

## Method

In-package Go benchmarks drive the real `Model` (200×50) through `Update` and
`View` with the shell's bd, tmux and git reads stubbed, as `tui`'s `TestMain`
does. Fixtures are a live `bd list --all -n 0 --json` and `events.jsonl` taken
2026-10-16. The synthetic store clones every closed bead twice with fresh ids
(parents and deps remapped) and the events four times, older in time; the open
set is unchanged. Run with `CGO_ENABLED=0`, `-benchtime 3s -benchmem`.

**Flat means allocs/op.** Wall time on the WSL dev box varies 2–4× between
identical runs; allocations per op are deterministic. The scaling gate reads
"flat" as **allocs/op at the synthetic store ≤ 1.25× today's**, alongside the
unchanged ≤ 150 ms keypress→redraw p95 of `docs/tui-refresh-gates.md`.

## Results

Today: 3,497 beads (3,434 closed), 25,633 events. Synthetic: 10,365 beads
(10,302 closed), 102,532 events.

| Path | When | ms today → synthetic | allocs/op today → synthetic | growth | Where it goes |
|---|---|---|---|---|---|
| Logs key, `/` filter kept | each key/frame | 3,655 → 3,782 | 4.43M → 17.7M | ×4.0 | `rows()` re-concatenates every event's msg+attrs and runs `matchWords`, ~8× per frame (current, header twice via bodyH, list, bindings/extra, hints, gotoOpts) |
| Graph key, `s any` | each key/frame | 660 → 1,440 | 381k → 1.13M | ×3.0 | `lines()` formats every row; `containerPrio` does a linear `beadIndex` per row (rows × beads) though `snapIndex` exists |
| Logs key, unfiltered | each key/frame | 282 → 407 | 337k → 1.34M | ×4.0 | `list()` formats every event (clock + two lipgloss renders) before the drawer slices the window; `rows()` ~8× |
| Logs frame (View only) | each redraw incl. 3 s tick | 81 → 405 | 337k → 1.34M | ×4.0 | as above |
| Event log full parse | startup, rotation (off loop) | 348 → 1,552 | 1.82M → 7.27M | ×4.0 | JSON into `map[string]any` plus sorted attr strings per event |
| `bd list --all` parse | slow tier, Graph entry, drop-out (off loop) | 95 → 314 | 54k → 162k | ×3.0 | whole-store unmarshal (9.7 → 29 MB); bd's own wall ~1.5 s vs ~0.8 s for the open set |
| Fold of one load | every 3 s tick, any tab | 22 → 105 | 8.9k → 25.4k (23 → 74 MB) | ×2.8 | Graph rebuild (buildGraph, subtree, newMemo) ≈ half; `initStatsOf` ≈ 30%; `newSnapIndex` |
| Fold of a full log read | startup, rotation | 15 → 44 | 2.19k → 2.25k | ×1.02 | `statsTab.recompute` |
| Graph key, default (unclosed) | each key/frame | 4.8 → 10.4 | 3.8k → 4.7k | ×1.24 | 24 rows; whole-snapshot helpers only |
| Ideate frame (control) | each redraw | 11 → 37 | 2.20k → 2.30k | ×1.05 | out of scope |

## Reading

Only the default Graph view and the Logs fold already pass the flat reading.
Ranked by what the human feels: Logs' per-frame full formatting and repeated
`rows()` rescans (worst under a word filter), then Graph's every-row formatting
with a quadratic container lookup once closed beads are shown, then the per-tick
fold over every closed bead. The two parses run off the event loop and cost CPU
and freshness, not keypress latency.

## After (mc-5yyk landed)

The scaling gate is now a committed benchmark, `BenchmarkScaling` in
`tui/scaling_bench_test.go` (`CGO_ENABLED=0 go test ./tui -run '^$' -bench
BenchmarkScaling -benchtime 3x`). It does not need the live fixtures: it builds a
store in code (a fixed open set plus N parentless closed Charts of nine closed Builds,
and an `events.jsonl` of N events) at today's size (3.5k beads / 25k events) and the
synthetic 10k beads / 100k events, drives the real `Model` through `Update` and `View`
(Logs through the real `loadLogs`, the TUI on the default closed set), and **fails** if
allocs/op at the synthetic store exceed 1.25× today's.

| Path | ms today → synthetic | allocs/op today → synthetic | growth | was (synthetic) |
|---|---|---|---|---|
| Logs key, `/` filter kept | 2.8 → 11.4 | 2,023 → 2,059 | ×1.02 | 3,782 ms, 17.7M |
| Logs key, unfiltered | 0.78 → 0.78 | 1,924 → 1,924 | ×1.00 | 407 ms, 1.34M |
| Logs frame | 0.72 → 0.74 | 1,848 → 1,845 | ×1.00 | 405 ms, 1.34M |
| Graph key, `s any` | 5.5 → 12.7 | 4,916 → 4,936 | ×1.00 | 1,440 ms, 1.13M |
| Graph key, default | 0.89 → 0.91 | 3,025 → 3,025 | ×1.00 | 10.4 ms, 4.7k |
| Fold of one load | 0.15 → 0.18 | 423 → 423 | ×1.00 | 105 ms, 25.4k |

Wall time is one `-benchtime 3x` run on the WSL dev box (noisy); the gate reads allocs/op.
The filtered Logs key's wall time still grows with the log because the whole-log scan
streams on every filter change, but its per-key allocations do not.
