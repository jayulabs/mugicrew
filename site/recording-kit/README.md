# Recording kit

Re-records the site's TUI demos (ADR 0365). Run by hand, in the devShell
(podman); never in CI, which needs no VHS, Chromium, ttyd or bd.

```bash
apps/mugicrew/site/recording-kit/record.sh
```

It builds `containers/fresh-install/fedora.Containerfile`, layers `Containerfile`
on it (tmux, dolt, bd, mugicrew from npm, claude, vhs, ttyd, ffmpeg with H.264,
Chromium), then runs `inside.sh` in the image: for each `tapes/*.tape` it seeds
a fresh fixture with `seed.sh` at `/tmp/demo` (with a Stats event log) and records it; a tape marked `# fixture: none` (install) runs unseeded. MP4s and poster
JPGs land in `site/static/recordings/`; commit them.

Tapes navigate by keys and position, never by bead id: ids are random on every
seed. Watch each new MP4 for host paths or real plan usage before committing.

The grilling tape runs against `stub-bin/claude`, a scripted stand-in for the
claude CLI (a live session needs a sign-in and real plan usage); it is on PATH
for that tape only. The first-signal tape does the same with `stub-triage/claude`,
which shows the triage verdict.
The loop-start-ps tape does the same with `stub-loop/claude`, which stays up so
the efforts the loop dispatches read alive in `mugicrew ps`.
