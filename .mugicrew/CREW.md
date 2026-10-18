# mugicrew Crew brief

Traps in this Context's architecture. Read after the Workspace brief.

## Dispatch tests fork the test binary

- **Trap:** `session.Backstop` runs `os.Executable()` `integrate`. Inside a test that re-execs the test binary, a runaway fork.
- **Symptom:** a test package that drives `worktreeRun`/`Backstop` spawns test binaries without end.
- **Fix:** dispatch's `TestMain` stubs backstop and routes `git.NewEffortWorktree` through the test's `git.NewWorktree` stub. Any new test package that drives `worktreeRun`/`Backstop` for real must stub `integrateIn`/`backstop` too.

## Go builds need CGO_ENABLED=0

- **Trap:** on go1.25 linux/386 a default `go build`/`go vet` pulls in `runtime/cgo`.
- **Symptom:** `gnu/stubs-32.h: No such file`.
- **Fix:** build and test with `CGO_ENABLED=0`. This matches the portable static-binary intent. CGO is needed only for the deferred embedded-Dolt in-process bd.
