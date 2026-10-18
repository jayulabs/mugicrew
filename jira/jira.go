// Package jira reads a Jira work item through acli, the Atlassian CLI, by bare
// name so PATH lookup applies and acli's own login is the only credential (ADR 0207).
// No Setpgid: intake is human-run, so Ctrl-C should stop the child too.
package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Item is the part of a work item intake and the latch read. Category is the
// status category key (new, indeterminate, done).
type Item struct {
	Key, Summary, Status, Category string
}

// Done reports whether the item's status category is Done; Jira has spelled the
// key both "done" and "DONE".
func (i Item) Done() bool { return strings.EqualFold(i.Category, "done") }

// View is one `acli jira workitem view <key> --fields summary,status --json`. The key
// returned wins over the typed one (Jira renames a moved item). A failure is
// diagnosed (ADR 0207) into an error reading "<what failed>; <fix>"; site is
// the Workspace's [jira] site, compared with acli's active one.
func View(key, site string) (Item, error) {
	out, err := exec.Command("acli", "jira", "workitem", "view", key, "--fields", "summary,status", "--json").Output()
	if err != nil {
		return Item{}, diagnose(err, key, site)
	}
	var v struct {
		Key    string
		Fields struct {
			Summary string
			Status  struct {
				Name           string
				StatusCategory struct{ Key string }
			}
		}
	}
	if err := json.Unmarshal(out, &v); err != nil || v.Key == "" || v.Fields.Summary == "" || v.Fields.Status.Name == "" || v.Fields.Status.StatusCategory.Key == "" {
		return Item{}, fmt.Errorf("acli gave unexpected output for %s; it may be too old (check 'acli --version' and update)", key)
	}
	return Item{v.Key, v.Fields.Summary, v.Fields.Status.Name, v.Fields.Status.StatusCategory.Key}, nil
}

// ErrNotFound and ErrNotLoggedIn are ActiveSite's two failures, each reading
// "<what failed>; <fix>".
var (
	ErrNotFound    = errors.New("acli not found on PATH; install the Atlassian CLI, then 'acli jira auth login'")
	ErrNotLoggedIn = errors.New("acli is not logged in to Jira; run 'acli jira auth login'")
)

// ActiveSite is the host of acli's active Jira login, read from `acli jira auth
// status`: "" when logged in but no Site: line is printed.
func ActiveSite() (string, error) {
	out, err := exec.Command("acli", "jira", "auth", "status").Output()
	var ee *exec.Error
	if errors.As(err, &ee) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrNotLoggedIn
	}
	for _, l := range strings.Split(string(out), "\n") {
		if host, ok := strings.CutPrefix(strings.TrimSpace(l), "Site: "); ok && host != "" {
			return host, nil
		}
	}
	return "", nil
}

// ValidSite is the [jira] site shape: a non-empty bare host.
func ValidSite(site string) bool { return site != "" && !strings.Contains(site, "/") }

// diagnose explains a failed view; it runs only on the failure path.
func diagnose(err error, key, site string) error {
	var ee *exec.Error
	if errors.As(err, &ee) {
		return ErrNotFound
	}
	var xe *exec.ExitError
	errors.As(err, &xe) // any other failure leaves xe nil: auth status still runs and the quoted message is empty
	if host, aerr := ActiveSite(); aerr != nil {
		return aerr
	} else if host != "" && host != site {
		return fmt.Errorf("acli is on %s, Workspace config wants %s; run 'acli jira auth switch --site %s'", host, site, site)
	}
	msg := ""
	if xe != nil {
		for _, l := range strings.Split(string(xe.Stderr), "\n") {
			if m, ok := strings.CutPrefix(strings.TrimSpace(l), "✗ Error: "); ok {
				msg = m
			}
		}
	}
	return fmt.Errorf("acli could not read %s (%q); check the key and your access on %s", key, msg, site)
}
