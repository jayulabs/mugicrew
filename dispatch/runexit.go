package dispatch

// A run-now host's exit codes (ADR 0332): the one set the host (mugicrew run)
// picks from and the TUI maps to a status. Anything else, a 2 from a flag error
// included, reads as an unknown failure; a signal means the host was not absorbed.
const (
	RunClean       = 0  // clean, every pause lift done
	RunFailed      = 1  // the effort failed: an error, an abort, a stranded integrate, a limited effort
	RunPreDispatch = 10 // failed before Dispatch; claim released
	RunNotRun      = 11 // CutBusy/CutHeld: claim released, the last output line says why
	RunLiftFailed  = 12 // clean, but a pause lift failed; the last output line names the label
	RunAbandoned   = 13 // abandoned on a second signal
)
