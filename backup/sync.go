package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// Outcome is what one Sync did.
type Outcome int

const (
	Held     Outcome = iota // another push holds the lock; nothing done
	NoRemote                // no backup remote configured
	Nothing                 // nothing unpushed
	Pushed                  // pushed and recorded
	Failed                  // an attempt failed and was recorded
)

// InstallHint points a user without a backup remote at the docs that add one.
const InstallHint = "no backup remote: see install.md's Backup section"

// ErrNoRemote is Sync's error when the graph has no backup remote.
var ErrNoRemote = errors.New(InstallHint)

// userEnv and passwordEnv are the credential file's variables (mugicrew's
// convention for the user; Dolt reads only the password).
const (
	userEnv     = "DOLT_REMOTE_USER"
	passwordEnv = "DOLT_REMOTE_PASSWORD"
)

// Sync is one backup push of the graph at beadsDir under trigger (eventlog.BackupCLI
// or BackupLoop): lock, Remote, Unpushed, Push, then the event. A held lock, a
// missing remote and nothing unpushed write no event. err is set for NoRemote
// (InstallHint) and for Failed (the scrubbed error, already recorded as
// backup.failed, whether the lock, the Remote or Unpushed read, or the push failed).
func Sync(beadsDir, trigger string) (Outcome, error) {
	lock, err := takeLock()
	if err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return Held, nil
		}
		return failed(trigger, err)
	}
	defer lock.Close() // closing drops the flock
	if _, ok, err := Remote(beadsDir); err != nil {
		return failed(trigger, err)
	} else if !ok {
		return NoRemote, ErrNoRemote
	}
	n, _, err := Unpushed(beadsDir)
	if err != nil {
		return failed(trigger, err)
	}
	if n == 0 {
		return Nothing, nil
	}
	start := time.Now()
	if err := Push(beadsDir, os.Getenv(userEnv)); err != nil {
		return failed(trigger, err)
	}
	eventlog.RecordBackupPushed(trigger, n, time.Since(start))
	return Pushed, nil
}

// failed records err, scrubbed, as backup.failed and returns it as Failed.
func failed(trigger string, err error) (Outcome, error) {
	err = errors.New(scrub(err.Error()))
	eventlog.RecordBackupFailed(trigger, err)
	return Failed, err
}

// takeLock takes the non-blocking push lock under the git common dir's mugicrew/.
func takeLock() (*os.File, error) {
	dir, err := git.CommonDir()
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	path := filepath.Join(dir, "mugicrew", "backup.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

var credURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]*@`)

// RedactURL masks any user:pass@ in s, so no credential shows in a row or a log.
func RedactURL(s string) string {
	return credURL.ReplaceAllString(s, "${1}***@")
}

// scrub drops the password value and any user:pass@ from a URL out of Dolt's error.
func scrub(s string) string {
	if p := os.Getenv(passwordEnv); p != "" {
		s = strings.ReplaceAll(s, p, "***")
	}
	return RedactURL(s)
}
