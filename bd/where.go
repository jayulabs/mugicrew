package bd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

// NoGraphError is GraphDir's refusal (Spec FR6): bd resolves no Bead
// Graph from Dir. Its message names the bd commands that create one (FR42).
type NoGraphError struct {
	Dir string
	Err error // bd where's failure
}

func (e *NoGraphError) Error() string {
	return fmt.Sprintf("no Bead Graph resolves from %s; run mugicrew init", e.Dir)
}

func (e *NoGraphError) Unwrap() error { return e.Err }

// GraphDir resolves the Bead Graph from dir the way bd does (FR4: in-tree
// .beads/, .beads/redirect, BEADS_DIR, contributor routing) by asking bd where,
// and returns its absolute dir. bd exiting non-zero is a *NoGraphError; bd
// itself missing or unrunnable is a plain error.
func GraphDir(dir string) (string, error) {
	out, err := runIn(dir, "where", "--json")
	if ee := (*exec.ExitError)(nil); errors.As(err, &ee) {
		return "", &NoGraphError{Dir: dir, Err: err}
	}
	if err != nil {
		return "", err
	}
	var w struct{ Path string }
	if err := json.Unmarshal([]byte(out), &w); err != nil || w.Path == "" {
		return "", fmt.Errorf("bd where: unreadable output %q: %v", out, err)
	}
	return w.Path, nil
}
