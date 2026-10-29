// Package backup is the single seam for every touch of a Bead Graph's backup
// remote (a Dolt remote named backup on the bd-managed sql-server's database).
// SQL goes to that server over the dolt CLI as a SQL client, never bd sql, whose
// client caps a call at about 10 s (a large push outlasts it). It never starts
// the server: a down server is an error.
package backup

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RemoteName and Branch name the remote and the branch the backup pushes.
const (
	RemoteName = "backup"
	Branch     = "main"
)

// SQL executes one SQL statement against the graph's sql-server and returns the
// result as CSV (header row first); a var so tests stub it.
var SQL = doltSQL

// Remote is the backup remote's url, ok false when the graph has none.
func Remote(beadsDir string) (url string, ok bool, err error) {
	rows, err := query(beadsDir, "select url from dolt_remotes where name = '"+RemoteName+"'")
	if err != nil || len(rows) == 0 {
		return "", false, err
	}
	return rows[0][0], true, nil
}

// Unpushed is the count of commits on main the backup remote lacks, and the
// date of the oldest (zero when count is 0).
func Unpushed(beadsDir string) (count int, oldest time.Time, err error) {
	rows, err := query(beadsDir, "select count(*), min(date) from dolt_log('remotes/"+RemoteName+"/"+Branch+".."+Branch+"')")
	if err != nil {
		return 0, time.Time{}, err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return 0, time.Time{}, fmt.Errorf("backup: unreadable unpushed count")
	}
	if count, err = strconv.Atoi(rows[0][0]); err != nil {
		return 0, time.Time{}, fmt.Errorf("backup: unpushed count %q: %w", rows[0][0], err)
	}
	if count > 0 {
		if oldest, err = parseDate(rows[0][1]); err != nil {
			return 0, time.Time{}, err
		}
	}
	return count, oldest, nil
}

// Push pushes main to the backup remote, authenticating as user when set. It
// never forces.
func Push(beadsDir, user string) error {
	_, err := SQL(beadsDir, pushCall(user))
	return err
}

func pushCall(user string) string {
	args := []string{quote(RemoteName), quote(Branch)}
	if user != "" {
		args = append([]string{"'--user'", quote(user)}, args...)
	}
	return "CALL DOLT_PUSH(" + strings.Join(args, ", ") + ")"
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// query runs q and returns its data rows, the header dropped.
func query(beadsDir, q string) ([][]string, error) {
	out, err := SQL(beadsDir, q)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("backup: unreadable sql output: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[1:], nil
}

func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339Nano, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("backup: unreadable commit date %q", s)
}

// doltSQL runs q through the dolt CLI against the bd-managed sql-server named by
// beadsDir's port file and metadata.json database.
func doltSQL(beadsDir, q string) (string, error) {
	port, err := os.ReadFile(filepath.Join(beadsDir, "dolt-server.port"))
	if err != nil {
		return "", fmt.Errorf("backup: no bd sql-server port in %s (is the server running?): %w", beadsDir, err)
	}
	raw, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json"))
	if err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	var meta struct {
		DoltDatabase string `json:"dolt_database"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil || meta.DoltDatabase == "" {
		return "", fmt.Errorf("backup: no dolt_database in %s/metadata.json", beadsDir)
	}
	cmd := exec.Command("dolt", "--host", "127.0.0.1", "--port", strings.TrimSpace(string(port)),
		"--no-tls", "-u", "root", "-p", "", "--use-db", meta.DoltDatabase, "sql", "-r", "csv", "-q", q)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String() + stdout.String()); msg != "" {
			return "", fmt.Errorf("%s", msg) // Dolt's own message, verbatim
		}
		return "", err // dolt printed nothing: not on PATH, not executable, or killed
	}
	return stdout.String(), nil // stdout only: a stderr warning must not corrupt the CSV
}

// StaleAfter is how old the oldest unpushed commit may be before the backup is stale.
const StaleAfter = 24 * time.Hour

// Stale reports whether the backup has gone stale: some commit is unpushed
// (oldest non-zero) and the oldest is over StaleAfter old at now.
func Stale(oldest, now time.Time) bool {
	return !oldest.IsZero() && now.Sub(oldest) > StaleAfter
}

// SameFilesystem reports whether url is a file:// destination on the same device
// as beadsDir, using the nearest existing parent of a path not yet created. Any
// other URL kind, or an unreadable path, is false.
func SameFilesystem(url, beadsDir string) bool {
	path, ok := strings.CutPrefix(url, "file://")
	if !ok {
		return false
	}
	want, err := deviceOf(beadsDir)
	if err != nil {
		return false
	}
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		if got, err := deviceOf(p); err == nil {
			return got == want
		}
		if p == filepath.Dir(p) {
			return false
		}
	}
}

func deviceOf(path string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Dev), nil
}
