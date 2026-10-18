# ADR 0195: On darwin, liveness reads the process table by sysctl and cwds by lsof

Status: Accepted
Date: 2026-10-14

Amends ADR 0025 and ADR 0127 §2 for darwin. Chart mc-j43f.2. The prototype
"Check the CGO-free macOS process reads for liveness on a real Mac" probed
these calls on macOS 26.4 arm64, built with CGO_ENABLED=0. Its code is on
`refs/mugicrew/prototypes/mc-j43f.2`.

Liveness reads `/proc` three ways (Alive/Pids, InTree, Lineage), and darwin
has no `/proc`. The port keeps the binary static and CGO-free (OE ADR 0007).

## Decision

1. **Split by build tag.** `liveness/proc_linux.go` holds today's `/proc`
   reads, with the `procDir` test seam unchanged. `liveness/proc_darwin.go`
   implements the same three reads. The functions callers use stay the same.
2. **Pids and argv.** `unix.SysctlKinfoProcSlice("kern.proc.all")` lists the
   pids, and `unix.SysctlRaw("kern.procargs2", pid)` gets each one's argv by
   sysctl name. The buffer is a little-endian int32 argc, then the exec path,
   then NUL padding, then argc NUL-terminated strings. Any error from that
   sysctl means skip the pid, as an EACCES on `/proc` is skipped today. Another
   user's process returns **EINVAL**, not EPERM, and so does a pid that exits
   mid-scan. Match argv[0]'s basename, never `p_comm`: the native claude
   runs from `~/.local/share/claude/versions/<ver>`, so its `p_comm` is the
   version string (`2.1.300`), while its argv[0] is `claude`.
3. **Cwd.** InTree runs `lsof -a -d cwd -F pn` once per call and parses the
   `p<pid>` and `n<path>` records. lsof exits 0 even when it can't read some
   processes, and it reports only the user's own processes, which is the
   EACCES skip again. The paths it prints are real paths (`/private/var/…`),
   so the existing `EvalSymlinks(tree)` stays. After the cwd is deleted, lsof
   still prints the old path with no marker. So there is nothing to strip on
   darwin, and a process in a deleted tree still holds it, as on Linux.
4. **Lineage.** Walk `unix.SysctlKinfoProc("kern.proc.pid", pid).Eproc.Ppid`.
   An error ends the walk.
5. **Fail closed.** If `lsof` is missing or fails to run, InTree must not
   return an empty answer that recover would read as "no holder". It returns
   an error, and recover refuses (ADR 0127 §2).

## Considered Options

- **libproc `proc_pidinfo(PROC_PIDVNODEPATHINFO)` for cwd.** x/sys v0.36.0
  doesn't expose it. Calling it without CGO needs a hand-written libSystem
  trampoline, which Go doesn't support. Rejected.
- **gopsutil.** On darwin it reads cwd through CGO or by shelling out to lsof
  anyway, so it adds a dependency for no gain.
- **`ps -axww -o pid=,args=`.** It joins argv with spaces, so it can't match
  an exact token that contains a space. kern.procargs2 keeps the argv split.

## Consequences

- An npm-installed claude, if it runs as `node …/claude`, still reads as dead
  on every OS. The probe saw only the native installer, so this stays the
  open caveat in `liveness/liveness.go`.
