package bd

import (
	"errors"
	"math/rand/v2"
	"os/exec"
	"strings"
	"time"
)

// maxAttempts bounds a bd call's tries, the first included (Spec D1, ADR 0153).
const maxAttempts = 3

// sleep is a var so a test skips the real backoff.
var sleep = time.Sleep

var (
	// neverRetry names a failure whose write may have landed; checked first, so
	// an indeterminate error that also names 1213 is attempted once.
	neverRetry = []string{"indeterminate", "stage and commit after regular SQL commit"}
	// retryOn names a clean, replay-safe conflict or a transient server outage.
	retryOn = []string{"1213 (40001)", "already exists", "connection refused", "circuit breaker is open"}
)

// retryable reports whether a failed bd call is worth replaying: an exit error
// (bd ran and failed) whose text is a retryOn string and no neverRetry string.
// A non-exit error (bd missing) and any other exit, 13 included, are one-shot.
func retryable(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	msg := err.Error()
	for _, s := range neverRetry {
		if strings.Contains(msg, s) {
			return false
		}
	}
	for _, s := range retryOn {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// backoff is the jittered wait (about 0.2 s, 0.5 s, 1 s) after the attempt-th
// failed try, ±50% so concurrent retriers spread out of lockstep.
func backoff(attempt int) time.Duration {
	base := [...]time.Duration{200 * time.Millisecond, 500 * time.Millisecond, time.Second}[min(attempt, 2)]
	return base/2 + rand.N(base)
}
