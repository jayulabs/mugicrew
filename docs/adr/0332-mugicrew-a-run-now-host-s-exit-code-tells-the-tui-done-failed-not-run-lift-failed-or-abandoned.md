# A run-now host's exit code tells the TUI done, failed, not run, lift failed, or abandoned

Status: accepted

A run-now effort is hosted by a setsid-detached `mugicrew run <id>` (ADR 0330
(mugicrew-a-run-now-host-inherits-the-effort-flock-by-fd…)). The TUI that started
it learns how the effort ended only from `cmd.Wait()`: either an exit status or a
signal. Today `runDoneMsg.err` is Dispatch's error joined with any unpause error.
Because of that, a busy/held refusal reads `done` (Dispatch returns nil when
nothing ran), and a failed pause lift after a clean run reads `failed`. The host's
exit code therefore carries one code per word the TUI would say differently. The
detail stays in `run-<id>.out`.

| Code | Meaning | TUI status | Tail |
|---|---|---|---|
| 0 | Dispatch clean, every lift done | `run X done` | none |
| 1 | Effort failed. This includes an abort (`Outcome.Aborted`), a stranded integrate and a limited effort | `run X failed: exit status 1` | last 12 lines |
| 10 | Host failed before Dispatch (`run.failed`, ADR 0331 (mugicrew-a-run-now-host-adds-only-run-failed…)) | `run X failed before dispatch` | last 12 lines |
| 11 | Not run: `CutBusy`/`CutHeld`, bead unclaimed | `run X not run` | the host's last line, `branch <b> busy\|held by <path>` |
| 12 | Dispatch clean, but lifting `paused:failing` or `paused:foreign-hold` failed | `run X done; lifting paused:<cause> failed — clear it by hand` | the host's last line, naming the label and the error |
| 13 | Abandoned on the second SIGTERM/SIGINT (`paused:abandoned` written) | `run X abandoned (paused:abandoned)` | last 12 lines |
| any other code | Unknown. This includes 2, which is a Go panic or a usage error | `run X failed: exit status N` | last 12 lines |
| signal | Death by a signal the host didn't absorb, e.g. SIGKILL; the claim may be stranded | `run X killed: signal: <name>` | last 12 lines |

- **A reserved block, 10 and up.** The meaningful codes stay clear of 2 (Go's
  panic exit, and mugicrew's usage error, ADR 0283 §7) and of a shell's 126/127.
  So a crashing host can't read as "not run".
- **Precedence when outcomes coincide.** Abandon beats everything. A busy/held
  refusal whose unclaim errs is 1, because the claim may be stranded. A failed
  Dispatch whose foreign-hold lift also errs is 1, and both errors go to
  `run-<id>.out`. A pre-Dispatch failure whose unclaim errs stays 10.
- **The first, absorbed signal is not a code of its own.** An abort winds down
  through the child's exit or the ADR 0155 re-read and exits 1. The tail shows it
  was an abort.
- **The host abandons, then exits 13 rather than re-raising the signal.** That
  keeps an Abandon, which leaves a pause the human can see, apart from a SIGKILL,
  which writes nothing.

## Considered Options

- **Two codes (0/1), with all detail from the tail.** Rejected. A refusal would
  read as a failure (or as done), and a lift error would hide behind "failed"
  even though the work integrated.
- **One code per Dispatch outcome (aborted, stranded, limited).** Rejected. None
  of them changes the TUI's word, and the tail already explains them.
- **Re-raise the signal after Abandon.** Rejected. `Wait` could no longer tell an
  Abandon from a SIGKILL.
