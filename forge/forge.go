// Package forge is mugicrew's GitLab client: plain functions over `glab api -i`,
// so auth is glab's login and no token lives here (ADR 0185). Tests fake the glab
// binary on PATH, not a type. Nothing here ever arms auto-merge, cancels it or merges
// (ADR 0189): the merge is a human's act.
package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// Project is a GitLab project: its host and URL-encoded <namespace>/<project> (the :id).
type Project struct {
	Host, ID string
}

// ParseRemote reads the host and project path out of a Forge remote's URL in its
// three forms (git@host:ns/p.git, ssh://git@host/ns/p.git, https://host/ns/p.git).
// glab's :fullpath is not used: it may pick a different remote than the Forge's.
func ParseRemote(remote string) (Project, error) {
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return Project{}, fmt.Errorf("forge: remote %q: %w", remote, err)
		}
		host, path = u.Hostname(), u.Path
		if u.Scheme == "http" || u.Scheme == "https" {
			host = u.Host // the API shares the web port; an ssh port is not the API's
		}
	} else if h, p, ok := strings.Cut(remote, ":"); ok { // scp-like git@host:ns/p.git
		host, path = h[strings.LastIndex(h, "@")+1:], p
	}
	path = strings.Trim(strings.TrimSuffix(strings.Trim(path, "/"), ".git"), "/")
	if host == "" || path == "" {
		return Project{}, fmt.Errorf("forge: remote %q names no host and project", remote)
	}
	return Project{Host: host, ID: url.PathEscape(path)}, nil
}

// Path is the project's unencoded <namespace>/<project>.
func (p Project) Path() string {
	path, err := url.PathUnescape(p.ID)
	if err != nil {
		return p.ID
	}
	return path
}

// MRCanonical is the canonical identity of a merge request, <host>/<path>!<iid>:
// the Watches: mr value and the tail of a MR Latch's external ref.
func MRCanonical(p Project, iid int) string { return fmt.Sprintf("%s/%s!%d", p.Host, p.Path(), iid) }

// ParseMRCanonical is MRCanonical's inverse. The host is everything before the
// first "/", the path everything up to the last "!".
func ParseMRCanonical(s string) (Project, int, error) {
	host, rest, ok := strings.Cut(s, "/")
	i := strings.LastIndex(rest, "!")
	if !ok || host == "" || i < 1 {
		return Project{}, 0, fmt.Errorf("forge: %q is not <host>/<path>!<iid>", s)
	}
	iid, err := strconv.Atoi(rest[i+1:])
	if err != nil || iid < 1 {
		return Project{}, 0, fmt.Errorf("forge: %q is not <host>/<path>!<iid>", s)
	}
	return Project{Host: host, ID: url.PathEscape(rest[:i])}, iid, nil
}

var mrURLPath = regexp.MustCompile(`^/(.+)/-/merge_requests/(\d+)(?:/.*)?$`)

// ParseMRRef reads a merge request ref against the Forge's own project: a web URL
// (its host must be the Forge's), <path>!<iid> (the Forge's host) or !<iid> (the
// Forge's project). The returned Project's ID stays URL-encoded.
func ParseMRRef(ref string, forgeProj Project) (Project, int, error) {
	bad := fmt.Errorf("forge: %q is not an MR URL, <group/project>!<iid> or !<iid>", ref)
	var path, num string
	if strings.Contains(ref, "://") {
		u, err := url.Parse(ref)
		if err != nil {
			return Project{}, 0, bad
		}
		if u.Host != forgeProj.Host {
			return Project{}, 0, fmt.Errorf("forge: %q is on host %s, not the Forge's %s", ref, u.Host, forgeProj.Host)
		}
		m := mrURLPath.FindStringSubmatch(u.Path)
		if m == nil {
			return Project{}, 0, bad
		}
		path, num = m[1], m[2]
	} else {
		var ok bool
		if path, num, ok = strings.Cut(ref, "!"); !ok {
			return Project{}, 0, bad
		}
	}
	iid, err := strconv.Atoi(num)
	if err != nil || iid < 1 || strings.Contains(path, "!") {
		return Project{}, 0, bad
	}
	if path == "" {
		return forgeProj, iid, nil
	}
	return Project{Host: forgeProj.Host, ID: url.PathEscape(strings.Trim(path, "/"))}, iid, nil
}

// Typed outcomes of a glab api call, matched with errors.Is.
var (
	ErrNotFound    = errors.New("forge: not found")
	ErrDuplicate   = errors.New("forge: already exists")
	ErrRateLimited = errors.New("forge: rate limited")
	ErrForbidden   = errors.New("forge: forbidden")
)

// Error is any other Forge failure, including glab exiting non-zero with no HTTP
// status line (glab missing a login, a network error). Status is 0 then.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return "forge: " + e.Msg
	}
	return fmt.Sprintf("forge: HTTP %d: %s", e.Status, e.Msg)
}

// Merge request states and pipeline statuses the callers branch on.
const (
	StateOpened  = "opened"
	StateMerged  = "merged"
	StateClosed  = "closed"
	StatusFailed = "failed"
)

// MR is the part of a merge request this package reads.
type MR struct {
	IID                 int       `json:"iid"`
	State               string    `json:"state"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	DetailedMergeStatus string    `json:"detailed_merge_status"` // decoded so tests prove conflict is never keyed on it (Spec FR 24–26)
	HasConflicts        bool      `json:"has_conflicts"`
	MergeError          string    `json:"merge_error"`
	SquashCommitSHA     string    `json:"squash_commit_sha"`
	MergeCommitSHA      string    `json:"merge_commit_sha"`
	UserNotesCount      int       `json:"user_notes_count"` // gates the discussions read (Spec FR 22)
	HeadPipeline        *Pipeline `json:"head_pipeline"`
	SHA                 string    `json:"sha"` // the source branch's current head
	WebURL              string    `json:"web_url"`
}

// Pipeline is a merge request's head pipeline.
type Pipeline struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
	WebURL string `json:"web_url"`
	Ref    string `json:"ref"`
	SHA    string `json:"sha"`
	// UpdatedAt changes when a Forge retry re-runs the pipeline under the same id.
	UpdatedAt string `json:"updated_at"`
}

// MergedResults is whether the pipeline ran on the merge ref (refs/merge-requests/N/merge):
// its sha is a merge commit whose first parent is the target branch's sha.
func (p Pipeline) MergedResults() bool { return strings.HasSuffix(p.Ref, "/merge") }

// Job is a failed pipeline job.
type Job struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	WebURL        string `json:"web_url"`
	FailureReason string `json:"failure_reason"`
}

// RetryPipeline re-runs a pipeline's failed jobs (POST …/pipelines/:id/retry).
func RetryPipeline(p Project, id int) error {
	_, err := call(p, "POST", fmt.Sprintf("pipelines/%d/retry", id), "")
	return err
}

// FailedJobs lists a pipeline's failed jobs.
func FailedJobs(p Project, pipeline int) ([]Job, error) {
	body, err := call(p, "GET", fmt.Sprintf("pipelines/%d/jobs?scope=failed", pipeline), "")
	if err != nil {
		return nil, err
	}
	var jobs []Job
	if err := json.Unmarshal(body, &jobs); err != nil {
		return nil, fmt.Errorf("forge: decode failed jobs: %w", err)
	}
	return jobs, nil
}

// TraceTail is the last n lines of a job's trace with ANSI stripped, never the raw log.
func TraceTail(p Project, job, n int) (string, error) {
	body, err := call(p, "GET", fmt.Sprintf("jobs/%d/trace", job), "")
	if err != nil {
		return "", err
	}
	return Tail(string(body), n), nil
}

// TargetOf is the first parent of a merged-results pipeline's merge commit: the
// target branch sha the pipeline ran against.
func TargetOf(p Project, sha string) (string, error) { return parentOf(p, sha, 0) }

// SourceOf is the second parent of a merged-results pipeline's merge commit: the
// source branch sha the pipeline ran on.
func SourceOf(p Project, sha string) (string, error) { return parentOf(p, sha, 1) }

func parentOf(p Project, sha string, n int) (string, error) {
	body, err := call(p, "GET", "repository/commits/"+sha, "")
	if err != nil {
		return "", err
	}
	var c struct {
		ParentIDs []string `json:"parent_ids"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return "", fmt.Errorf("forge: commit %s: %w", sha, err)
	}
	if len(c.ParentIDs) <= n {
		return "", fmt.Errorf("forge: commit %s has no parent %d", sha, n+1)
	}
	return c.ParentIDs[n], nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// Tail strips ANSI escapes from a job trace, keeps what a carriage return left
// visible on each line, and returns the last n lines. Pure.
func Tail(trace string, n int) string {
	lines := strings.Split(strings.TrimRight(ansi.ReplaceAllString(strings.ReplaceAll(trace, "\r\n", "\n"), ""), "\n"), "\n")
	for i, l := range lines {
		lines[i] = l[strings.LastIndex(l, "\r")+1:]
	}
	return strings.Join(lines[max(0, len(lines)-n):], "\n")
}

// GetMR reads one merge request by iid.
func GetMR(p Project, iid int) (*MR, error) {
	body, err := call(p, "GET", fmt.Sprintf("merge_requests/%d", iid), "")
	if err != nil {
		return nil, err
	}
	var mr MR
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("forge: decode merge request: %w", err)
	}
	return &mr, nil
}

// Discussion is one merge request discussion: a thread of notes, or a lone comment
// (IndividualNote).
type Discussion struct {
	ID             string `json:"id"`
	IndividualNote bool   `json:"individual_note"`
	Resolvable     bool   `json:"resolvable"`
	Resolved       bool   `json:"resolved"`
	Notes          []Note `json:"notes"` // chronological
}

// Note is one comment of a Discussion.
type Note struct {
	ID     int    `json:"id"`
	Body   string `json:"body"`
	System bool   `json:"system"`
	Author struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	} `json:"author"`
	Position *Position `json:"position"` // diff notes only
}

// Position is where a diff note sits.
type Position struct {
	NewPath string `json:"new_path"`
	NewLine int    `json:"new_line"`
	OldPath string `json:"old_path"`
	OldLine int    `json:"old_line"`
	HeadSHA string `json:"head_sha"`
}

const discussionsPerPage = 100

// Discussions reads every discussion of the merge request, page by page (glab's
// --paginate cannot be combined with -i, whose status line call classifies on).
func Discussions(p Project, iid int) ([]Discussion, error) {
	var all []Discussion
	for page := 1; ; page++ {
		body, err := call(p, "GET", fmt.Sprintf("merge_requests/%d/discussions?per_page=%d&page=%d", iid, discussionsPerPage, page), "")
		if err != nil {
			return nil, err
		}
		var ds []Discussion
		if err := json.Unmarshal(body, &ds); err != nil {
			return nil, fmt.Errorf("forge: decode discussions: %w", err)
		}
		all = append(all, ds...)
		if len(ds) < discussionsPerPage {
			return all, nil
		}
	}
}

// IsBot is whether the GitLab user is a bot account (GET /users/:id; a note's author
// is a UserBasic, which has no bot field). The endpoint is instance-wide, not project-scoped.
func IsBot(p Project, userID int) (bool, error) {
	body, err := call(p, "GET", fmt.Sprintf("/users/%d", userID), "")
	if err != nil {
		return false, err
	}
	var u struct {
		Bot bool `json:"bot"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return false, fmt.Errorf("forge: decode user: %w", err)
	}
	return u.Bot, nil
}

// CurrentUser is the authenticated user's username (GET /user): the graph name.
func CurrentUser(p Project) (string, error) {
	name, _, err := currentUser(p)
	return name, err
}

// CurrentUserID is the authenticated user's numeric id (GET /user), the assignee_id
// a created merge request takes.
func CurrentUserID(p Project) (int, error) {
	_, id, err := currentUser(p)
	return id, err
}

func currentUser(p Project) (string, int, error) {
	body, err := call(p, "GET", "/user", "")
	if err != nil {
		return "", 0, err
	}
	var u struct {
		Username string `json:"username"`
		ID       int    `json:"id"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return "", 0, fmt.Errorf("forge: decode current user: %w", err)
	}
	return u.Username, u.ID, nil
}

// Reply posts body as a new note on the discussion, sent as an LF file like CreateMR's
// description. ErrForbidden on a locked discussion.
func Reply(p Project, iid int, discussionID, body string) error {
	_, err := callField(p, "POST", fmt.Sprintf("merge_requests/%d/discussions/%s/notes", iid, discussionID), "body", body)
	return err
}

// Resolve marks the discussion resolved; resolving a resolved one is harmless.
func Resolve(p Project, iid int, discussionID string) error {
	_, err := call(p, "PUT", fmt.Sprintf("merge_requests/%d/discussions/%s?resolved=true", iid, discussionID), "")
	return err
}

// FindOpenMR is the branch's one open merge request, nil when none is open.
func FindOpenMR(p Project, branch string) (*MR, error) {
	q := url.Values{"source_branch": {branch}, "state": {StateOpened}}
	body, err := call(p, "GET", "merge_requests?"+q.Encode(), "")
	if err != nil {
		return nil, err
	}
	var mrs []MR
	if err := json.Unmarshal(body, &mrs); err != nil {
		return nil, fmt.Errorf("forge: decode merge request list: %w", err)
	}
	if len(mrs) == 0 {
		return nil, nil
	}
	return &mrs[0], nil
}

// CreateMR opens a squash-on merge request from branch into target. A 409 is
// ErrDuplicate: another tick won the race, so re-list. A non-zero assigneeID is sent
// as assignee_id in the same POST.
func CreateMR(p Project, branch, target, title, description string, assigneeID int) (*MR, error) {
	fields := []string{"-f", "source_branch=" + branch, "-f", "target_branch=" + target, "-f", "title=" + title, "-F", "squash=true"}
	if assigneeID != 0 {
		fields = append(fields, "-F", fmt.Sprintf("assignee_id=%d", assigneeID))
	}
	body, err := call(p, "POST", "merge_requests", description, fields...)
	if err != nil {
		return nil, err
	}
	var mr MR
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, fmt.Errorf("forge: decode created merge request: %w", err)
	}
	return &mr, nil
}

// SetDescription replaces the merge request's description.
func SetDescription(p Project, iid int, description string) error {
	if description == "" { // call sends no field for "": a silent no-op
		return &Error{Msg: "empty merge request description"}
	}
	_, err := call(p, "PUT", fmt.Sprintf("merge_requests/%d", iid), description)
	return err
}

// SetMR replaces the merge request's title and description in one PUT.
func SetMR(p Project, iid int, title, description string) error {
	if description == "" { // call sends no field for "": a silent no-op
		return &Error{Msg: "empty merge request description"}
	}
	_, err := call(p, "PUT", fmt.Sprintf("merge_requests/%d", iid), description, "-f", "title="+title)
	return err
}

// CloseMR closes the merge request by iid (state_event=close). A 429 is ErrRateLimited.
func CloseMR(p Project, iid int) error {
	_, err := call(p, "PUT", fmt.Sprintf("merge_requests/%d", iid), "", "-f", "state_event=close")
	return err
}

// Settings is the part of a GitLab project's configuration this package reads.
type Settings struct {
	SquashCommitTemplate string `json:"squash_commit_template"`
	SquashOption         string `json:"squash_option"`
	MergeMethod          string `json:"merge_method"`
}

// ReadSettings is GET /projects/:id.
func ReadSettings(p Project) (Settings, error) {
	body, err := call(p, "GET", "", "")
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	if err := json.Unmarshal(body, &s); err != nil {
		return Settings{}, fmt.Errorf("forge: decode project: %w", err)
	}
	return s, nil
}

// CheckAuth fails loudly when glab is missing or not logged in to host.
func CheckAuth(host string) error {
	cmd := exec.Command("glab", "auth", "status", "--hostname", host)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // a loop's Ctrl-C drains; it must not kill a Forge call (ADR 0050)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("forge: glab is not usable for %s (installed and logged in?): %w: %s", host, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ProjectRefusal is a project setting that would drop the land trailer from
// mainline: the squash-commit template or the squash option (Spec FR 37).
type ProjectRefusal struct {
	Field, Value, Fix string
}

// Wrong is what is wrong with the project, without the fix or the README pointer.
func (e *ProjectRefusal) Wrong() string {
	return fmt.Sprintf("project %s is %q", e.Field, e.Value)
}

func (e *ProjectRefusal) Error() string {
	return fmt.Sprintf("forge: project %s is %q: %s (README, \"GitLab land strategy\")", e.Field, e.Value, e.Fix)
}

// ValidateSettings refuses a project whose squash_commit_template lacks
// %{description} on a line of its own, or whose squash_option is not always
// (Require locks the MR squash box, so no merge lands without the trailer). Pure.
func ValidateSettings(s Settings) error {
	if s.SquashOption != "always" {
		return &ProjectRefusal{"squash_option", s.SquashOption, "set Squash commits to Require"}
	}
	lines := strings.Split(s.SquashCommitTemplate, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r") // a CRLF-stored template is still correct
	}
	if !slices.Contains(lines, "%{description}") {
		return &ProjectRefusal{"squash_commit_template", s.SquashCommitTemplate, "make it \"%{title}\", a blank line, then %{description} on a line of its own"}
	}
	return nil
}

// call runs `glab api -i` for one project-scoped endpoint (or an instance-wide one,
// leading "/"), sends a non-empty description as LF text from a file (descriptions can exceed argv comfort), classifies the HTTP status
// line (glab exits 1 on every HTTP error) and returns the JSON body of a 2xx.
func call(p Project, method, endpoint, description string, fields ...string) ([]byte, error) {
	return callField(p, method, endpoint, "description", description, fields...)
}

// callField is call with the text sent as the named form field.
func callField(p Project, method, endpoint, field, text string, fields ...string) ([]byte, error) {
	args := []string{"api", "-i", "--hostname", p.Host, "-X", method}
	args = append(args, fields...)
	if text != "" {
		tmp, err := os.CreateTemp("", "mugicrew-mr-*.md")
		if err != nil {
			return nil, fmt.Errorf("forge: %w", err)
		}
		defer os.Remove(tmp.Name())
		_, werr := tmp.WriteString(strings.ReplaceAll(text, "\r", ""))
		if cerr := tmp.Close(); werr != nil || cerr != nil {
			return nil, fmt.Errorf("forge: %w", errors.Join(werr, cerr))
		}
		args = append(args, "-F", field+"=@"+tmp.Name())
	}
	path := "projects/" + p.ID + "/" + endpoint
	if rest, ok := strings.CutPrefix(endpoint, "/"); ok { // an instance-wide endpoint
		path = rest
	}
	args = append(args, strings.TrimSuffix(path, "/"))
	var stderr bytes.Buffer
	cmd := exec.Command("glab", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // a loop's Ctrl-C drains; it must not kill a Forge write mid-flight (ADR 0050)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		return nil, fmt.Errorf("forge: run glab (is it installed and logged in to %s?): %w", p.Host, err)
	}
	status, body := parse(out)
	switch {
	case status >= 200 && status < 300:
		return body, nil
	case status == 403:
		return nil, ErrForbidden
	case status == 404:
		return nil, ErrNotFound
	case status == 409 && method == "POST" && endpoint == "merge_requests":
		return nil, ErrDuplicate
	case status == 429:
		return nil, ErrRateLimited
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = strings.TrimSpace(stderr.String())
	}
	return nil, &Error{Status: status, Msg: msg}
}

// parse splits `glab api -i` output into the HTTP status and the body after the
// headers; no status line is status 0.
func parse(out []byte) (status int, body []byte) {
	head, body, _ := bytes.Cut(bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n")), []byte("\n\n"))
	var proto string
	if _, err := fmt.Sscanf(string(head), "%s %d", &proto, &status); err != nil || !strings.HasPrefix(proto, "HTTP/") {
		return 0, out
	}
	return status, body
}

// CloseOpenMR closes whatever MR find reports open on branch, through close, and returns
// its iid; 0 and no Forge write when none is open. A landed container's open MR is stale
// (GitLab allows one per source branch, and the merged one is no longer open), so there is
// no iid comparison. find and close are seams for the two crewmates' tests.
func CloseOpenMR(p Project, branch string, find func(Project, string) (*MR, error), close func(Project, int) error) (int, error) {
	mr, err := find(p, branch)
	if err != nil || mr == nil {
		return 0, err
	}
	return mr.IID, close(p, mr.IID)
}
