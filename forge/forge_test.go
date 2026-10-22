package forge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// fakeGlab puts a shell script named glab first on PATH. It prints responses/<n>
// (n = the call's 1-based count) verbatim, appends its argv (one line per call) to
// argv.log and, when the call carries -F description=@file, the file's body (a -f value is recorded literally, as glab sends it) to bodies.log,
// exiting with exit/<n> (default 0).
func fakeGlab(t *testing.T, responses ...string) (argvLog string) {
	t.Helper()
	dir := t.TempDir()
	for i, r := range responses {
		code := "0"
		if strings.HasPrefix(r, "HTTP/2.0 4") || strings.HasPrefix(r, "HTTP/2.0 5") || !strings.HasPrefix(r, "HTTP") {
			code = "1"
		}
		os.WriteFile(filepath.Join(dir, "resp"+string(rune('1'+i))), []byte(r), 0o644)
		os.WriteFile(filepath.Join(dir, "exit"+string(rune('1'+i))), []byte(code), 0o644)
	}
	script := `#!/bin/sh
d=` + dir + `
n=$(( $(cat $d/n 2>/dev/null || echo 0) + 1 ))
echo $n > $d/n
echo "$*" >> $d/argv.log
p=; for a in "$@"; do case "$a" in description=@*|body=@*) if [ "$p" = -F ] || [ "$p" = --field ]; then cat "${a#*=@}" >> $d/bodies.log; else echo "$a" >> $d/bodies.log; fi;; esac; p=$a; done
cat $d/resp$n
exit $(cat $d/exit$n)
`
	os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return filepath.Join(dir, "argv.log")
}

var proj = Project{Host: "gl.example.com", ID: "ns%2Fp"}

func TestParseRemote(t *testing.T) {
	for _, url := range []string{
		"git@gl.example.com:ns/p.git",
		"ssh://git@gl.example.com/ns/p.git",
		"https://gl.example.com/ns/p.git",
	} {
		if got, err := ParseRemote(url); err != nil || got != proj {
			t.Errorf("ParseRemote(%q) = %+v, %v; want %+v", url, got, err, proj)
		}
	}
	if got, _ := ParseRemote("https://gl.example.com/a/b/c.git"); got.ID != "a%2Fb%2Fc" {
		t.Errorf("nested group ID = %q", got.ID)
	}
	if got, _ := ParseRemote("https://gl.example.com:8443/ns/p.git"); got.Host != "gl.example.com:8443" {
		t.Errorf("https port dropped: host = %q", got.Host)
	}
	if got, _ := ParseRemote("ssh://git@gl.example.com:2222/ns/p.git"); got.Host != "gl.example.com" {
		t.Errorf("ssh port is not the API's: host = %q", got.Host)
	}
	if _, err := ParseRemote("/srv/git/p.git"); err == nil {
		t.Error("a local path has no host: want an error")
	}
}

func TestFindOpenMR(t *testing.T) {
	log := fakeGlab(t,
		"HTTP/2.0 200 OK\nContent-Type: application/json\n\n[{\"iid\":7}]",
		"HTTP/2.0 200 OK\n\n[]")
	mr, err := FindOpenMR(proj, "chart/x")
	if err != nil || mr == nil || mr.IID != 7 {
		t.Fatalf("FindOpenMR = %+v, %v", mr, err)
	}
	if mr, err := FindOpenMR(proj, "chart/x"); err != nil || mr != nil {
		t.Errorf("empty list = %+v, %v; want nil, nil", mr, err)
	}
	b, _ := os.ReadFile(log)
	first := strings.Split(string(b), "\n")[0]
	for _, want := range []string{"api -i", "--hostname gl.example.com", "projects/ns%2Fp/merge_requests?", "source_branch=chart%2Fx", "state=opened"} {
		if !strings.Contains(first, want) {
			t.Errorf("argv %q lacks %q", first, want)
		}
	}
}

func TestGetMRReadsTheMirroredFields(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n"+`{"iid":7,"state":"closed","detailed_merge_status":"need_rebase","has_conflicts":true,"merge_error":"boom","squash_commit_sha":"b2","merge_commit_sha":"c3","user_notes_count":3,"head_pipeline":{"id":42,"status":"failed","web_url":"https://gl/p/42"}}`)
	mr, err := GetMR(proj, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := MR{IID: 7, State: "closed", DetailedMergeStatus: "need_rebase", HasConflicts: true, MergeError: "boom",
		SquashCommitSHA: "b2", MergeCommitSHA: "c3", UserNotesCount: 3, HeadPipeline: &Pipeline{ID: 42, Status: "failed", WebURL: "https://gl/p/42"}}
	if got := *mr; got.HeadPipeline == nil || *got.HeadPipeline != *want.HeadPipeline {
		t.Fatalf("GetMR = %+v, want %+v", got, want)
	} else if got.HeadPipeline, want.HeadPipeline = nil, nil; got != want {
		t.Errorf("GetMR = %+v, want %+v", got, want)
	}
	if b, _ := os.ReadFile(log); !strings.Contains(string(b), "projects/ns%2Fp/merge_requests/7") {
		t.Errorf("argv %q lacks the MR endpoint", b)
	}
	fakeGlab(t, "HTTP/2.0 429 Too Many Requests\n\n{}")
	if _, err := GetMR(proj, 7); !errors.Is(err, ErrRateLimited) {
		t.Errorf("429 = %v, want ErrRateLimited", err)
	}
}

func TestCreateMRSendsSquashTargetTitleAndLFBody(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 201 Created\n\n{\"iid\":9}")
	mr, err := CreateMR(proj, "chart/x", "main", "land chart/x", "Tip: a\nChart: x\n", 42)
	if err != nil || mr.IID != 9 {
		t.Fatalf("CreateMR = %+v, %v", mr, err)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"-X POST", "source_branch=chart/x", "target_branch=main", "title=land chart/x", "squash=true", "assignee_id=42", "description=@"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("argv %q lacks %q", b, want)
		}
	}
	if bodies, _ := os.ReadFile(filepath.Join(filepath.Dir(log), "bodies.log")); string(bodies) != "Tip: a\nChart: x\n" {
		t.Errorf("body = %q", bodies)
	}
}

func TestCreateMRWithoutAssigneeSendsNone(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 201 Created\n\n{\"iid\":9}")
	CreateMR(proj, "b", "main", "t", "d", 0)
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "assignee") {
		t.Errorf("argv %q has an assignee", b)
	}
}

func TestCurrentUserIDReadsID(t *testing.T) {
	fakeGlab(t, "HTTP/2.0 200 OK\n\n{\"username\":\"bot\",\"id\":77}")
	if id, err := CurrentUserID(proj); err != nil || id != 77 {
		t.Errorf("CurrentUserID = %d, %v", id, err)
	}
}

func TestSetDescriptionPutsOnTheMR(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n{\"iid\":9}")
	if err := SetDescription(proj, 9, "x"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "-X PUT") || !strings.Contains(string(b), "merge_requests/9\n") {
		t.Errorf("argv = %q", b)
	}
}

func TestClassifiesHTTPStatus(t *testing.T) {
	rateLimited, notFound, dup := ErrRateLimited, ErrNotFound, ErrDuplicate
	for _, c := range []struct {
		name, resp string
		want       error
		forge      bool
	}{
		{"404", "HTTP/2.0 404 Not Found\n\n{\"message\":\"404 Not Found\"}", notFound, false},
		{"409", "HTTP/2.0 409 Conflict\n\n{\"message\":\"already exists\"}", dup, false},
		{"429", "HTTP/2.0 429 Too Many Requests\nRetry-After: 5\n\n", rateLimited, false},
		{"500", "HTTP/2.0 500 Internal Server Error\n\noops", nil, true},
		{"no status line", "glab: no such host", nil, true},
	} {
		fakeGlab(t, c.resp)
		_, err := CreateMR(proj, "b", "main", "t", "d", 0)
		var fe *Error
		switch {
		case c.want != nil && !errors.Is(err, c.want):
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		case c.forge && !errors.As(err, &fe):
			t.Errorf("%s: err = %v, want *Error", c.name, err)
		}
	}
}

func TestMissingGlabFailsLoudly(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := FindOpenMR(proj, "b"); err == nil || !strings.Contains(err.Error(), "glab") {
		t.Errorf("err = %v, want one naming glab", err)
	}
}

func TestNoCallEverMergesOrCancels(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n[]", "HTTP/2.0 201 Created\n\n{\"iid\":1}", "HTTP/2.0 200 OK\n\n{}")
	FindOpenMR(proj, "b")
	CreateMR(proj, "b", "main", "t", "d", 0)
	SetDescription(proj, 1, "d")
	b, _ := os.ReadFile(log)
	for _, bad := range []string{"/merge\n", "/merge ", "cancel_merge_when_pipeline_succeeds", "auto_merge"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("argv hit %q: %s", bad, b)
		}
	}
}

func TestValidateSettings(t *testing.T) {
	for _, c := range []struct {
		name, tmpl, squash, field string
	}{
		{"whole line", "%{title}\n\n%{description}", "always", ""},
		{"CRLF line ends", "%{title}\r\n\r\n%{description}\r\n", "always", ""},
		{"whole line, always", "%{description}", "always", ""},
		{"inline", "%{title} %{description}", "always", "squash_commit_template"},
		{"missing", "%{title}", "always", "squash_commit_template"},
		{"unset", "", "always", "squash_commit_template"},
		{"squash never", "%{title}\n\n%{description}", "never", "squash_option"},
		{"squash default_on", "%{title}\n\n%{description}", "default_on", "squash_option"},
		{"squash default_off", "%{title}\n\n%{description}", "default_off", "squash_option"},
		{"squash unset", "%{title}\n\n%{description}", "", "squash_option"},
	} {
		err := ValidateSettings(Settings{SquashCommitTemplate: c.tmpl, SquashOption: c.squash})
		var ref *ProjectRefusal
		switch {
		case c.field == "" && err != nil:
			t.Errorf("%s: %v, want ok", c.name, err)
		case c.field != "" && (!errors.As(err, &ref) || ref.Field != c.field || !strings.Contains(err.Error(), "GitLab land strategy")):
			t.Errorf("%s: %v, want a %s refusal naming the README", c.name, err, c.field)
		}
	}
}

func TestReadSettings(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n{\"squash_commit_template\":\"%{description}\",\"squash_option\":\"always\",\"merge_method\":\"merge\"}")
	s, err := ReadSettings(proj)
	if err != nil || s.SquashCommitTemplate != "%{description}" || s.SquashOption != "always" || s.MergeMethod != "merge" {
		t.Fatalf("ReadSettings = %+v, %v", s, err)
	}
	if b, _ := os.ReadFile(log); !strings.HasSuffix(strings.TrimSpace(string(b)), " projects/ns%2Fp") {
		t.Errorf("argv = %q", b)
	}
}

func TestTailStripsANSIAndKeepsTheLastLines(t *testing.T) {
	for _, c := range []struct {
		name, in string
		n        int
		want     string
	}{
		{"colour", "\x1b[31;1mFAIL\x1b[0m: boom", 5, "FAIL: boom"},
		{"section codes", "\x1b[0Ksection_start:1:x\r\x1b[0Krunning\n", 5, "running"},
		{"last n", "a\nb\nc\nd\n", 2, "c\nd"},
		{"fewer than n", "a\r\nb", 5, "a\nb"},
		{"empty", "", 3, ""},
	} {
		if got := Tail(c.in, c.n); got != c.want {
			t.Errorf("%s: Tail = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPipelineCalls(t *testing.T) {
	log := fakeGlab(t,
		"HTTP/2.0 201 Created\n\n{}",
		"HTTP/2.0 200 OK\n\n[{\"id\":5,\"name\":\"unit\",\"web_url\":\"https://gl/j/5\",\"failure_reason\":\"script_failure\"}]",
		"HTTP/2.0 200 OK\n\nl1\nl2\n\x1b[0mok\n",
		"HTTP/2.0 200 OK\n\n{\"parent_ids\":[\"base1\",\"tip1\"]}")
	if err := RetryPipeline(proj, 42); err != nil {
		t.Fatal(err)
	}
	jobs, err := FailedJobs(proj, 42)
	if err != nil || len(jobs) != 1 || jobs[0] != (Job{5, "unit", "https://gl/j/5", "script_failure"}) {
		t.Fatalf("FailedJobs = %+v, %v", jobs, err)
	}
	if tail, err := TraceTail(proj, 5, 2); err != nil || tail != "l2\nok" {
		t.Errorf("TraceTail = %q, %v", tail, err)
	}
	if base, err := TargetOf(proj, "m1"); err != nil || base != "base1" {
		t.Errorf("TargetOf = %q, %v", base, err)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"-X POST", "pipelines/42/retry\n", "pipelines/42/jobs?scope=failed\n", "jobs/5/trace\n", "repository/commits/m1\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("argv %q lacks %q", b, want)
		}
	}
	if !(Pipeline{Ref: "refs/merge-requests/3/merge"}).MergedResults() || (Pipeline{Ref: "refs/merge-requests/3/head"}).MergedResults() {
		t.Error("MergedResults must key on the /merge ref")
	}
}

// A Ctrl-C on the loop's process group must not kill glab mid-write (ADR 0050).
func TestGlabRunsInItsOwnProcessGroup(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\npg=$(ps -o pgid= -p $$ | tr -d ' '); echo $pg >> " + dir + "/pgid\necho 'HTTP/2.0 200 OK'; echo; echo '{}'\n"
	os.WriteFile(filepath.Join(dir, "glab"), []byte(script), 0o755)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	CheckAuth("gl.example.com")
	call(proj, "GET", "merge_requests", "")
	got, _ := os.ReadFile(filepath.Join(dir, "pgid"))
	fields := strings.Fields(string(got))
	if len(fields) != 2 {
		t.Fatalf("pgids = %q; want one per glab call", got)
	}
	for _, f := range fields {
		if f == strconv.Itoa(syscall.Getpgrp()) {
			t.Errorf("glab pgid %s equals the test's; want its own group", f)
		}
	}
}

func TestDiscussionsDecodesAcrossPages(t *testing.T) {
	full := "[" + strings.Repeat(`{"id":"d","resolvable":true,"notes":[{"id":1,"body":"b","author":{"id":5,"username":"u"},"position":{"new_path":"a.go","new_line":3,"head_sha":"h"}}]},`, discussionsPerPage-1) +
		`{"id":"last","individual_note":true,"notes":[]}]`
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n"+full, "HTTP/2.0 200 OK\n\n"+`[{"id":"p2","resolved":true,"notes":[{"id":2,"system":true}]}]`)
	ds, err := Discussions(proj, 7)
	if err != nil || len(ds) != discussionsPerPage+1 {
		t.Fatalf("Discussions = %d, %v; want %d", len(ds), err, discussionsPerPage+1)
	}
	if n := ds[0].Notes[0]; n.Author.ID != 5 || n.Author.Username != "u" || n.Position == nil || n.Position.NewLine != 3 {
		t.Errorf("note = %+v", n)
	}
	if last := ds[discussionsPerPage]; last.ID != "p2" || !last.Resolved || !last.Notes[0].System {
		t.Errorf("page 2 = %+v", last)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"projects/ns%2Fp/merge_requests/7/discussions?per_page=100&page=1", "page=2"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("argv %q lacks %q", b, want)
		}
	}
	fakeGlab(t, "HTTP/2.0 429 Too Many Requests\n\n{}")
	if _, err := Discussions(proj, 7); !errors.Is(err, ErrRateLimited) {
		t.Errorf("429 = %v, want ErrRateLimited", err)
	}
}

func TestCurrentUserReplyResolve(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n"+`{"id":1,"username":"jinbe-bot"}`,
		"HTTP/2.0 201 Created\n\n{}", "HTTP/2.0 200 OK\n\n{}", "HTTP/2.0 403 Forbidden\n\n{}")
	if name, err := CurrentUser(proj); err != nil || name != "jinbe-bot" {
		t.Errorf("CurrentUser = %q, %v", name, err)
	}
	if err := Reply(proj, 7, "abc", "line1\r\nline2"); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(proj, 7, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := Resolve(proj, 7, "abc"); !errors.Is(err, ErrForbidden) {
		t.Errorf("403 = %v, want ErrForbidden", err)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"-X GET user\n", "-X POST -F body=@", "projects/ns%2Fp/merge_requests/7/discussions/abc/notes\n", "-X PUT projects/ns%2Fp/merge_requests/7/discussions/abc?resolved=true\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("argv %q lacks %q", b, want)
		}
	}
	dir := filepath.Dir(log)
	if got, _ := os.ReadFile(filepath.Join(dir, "bodies.log")); string(got) != "line1\nline2" {
		t.Errorf("reply body = %q, want the LF text", got)
	}
}

func TestCloseMRSendsStateEventAndClassifies(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n{\"iid\":7,\"state\":\"closed\"}")
	if err := CloseMR(proj, 7); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	for _, want := range []string{"-X PUT", "state_event=close", "projects/ns%2Fp/merge_requests/7"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("argv %q lacks %q", b, want)
		}
	}
	fakeGlab(t, "HTTP/2.0 429 Too Many Requests\n\n{}")
	if err := CloseMR(proj, 7); !errors.Is(err, ErrRateLimited) {
		t.Errorf("429 = %v, want ErrRateLimited", err)
	}
}

func TestParseMRRef(t *testing.T) {
	forgeP := Project{Host: "gl.example.com", ID: "ns%2Fp"}
	other := Project{Host: "gl.example.com", ID: "a%2Fb%2Fc"}
	for ref, want := range map[string]Project{
		"https://gl.example.com/a/b/c/-/merge_requests/12":       other,
		"https://gl.example.com/a/b/c/-/merge_requests/12/diffs": other,
		"a/b/c!12": other,
		"!12":      forgeP,
	} {
		if got, iid, err := ParseMRRef(ref, forgeP); err != nil || got != want || iid != 12 {
			t.Errorf("ParseMRRef(%q) = %+v, %d, %v; want %+v, 12", ref, got, iid, err, want)
		}
	}
	if _, _, err := ParseMRRef("https://other.example.com/a/b/-/merge_requests/1", forgeP); err == nil || !strings.Contains(err.Error(), "other.example.com") {
		t.Errorf("wrong host err = %v", err)
	}
	for _, ref := range []string{"", "12", "!", "!0", "!x", "a!b!3", "https://gl.example.com/a/b", "https://gl.example.com/a/-/merge_requests/x"} {
		if _, _, err := ParseMRRef(ref, forgeP); err == nil {
			t.Errorf("ParseMRRef(%q) accepted", ref)
		}
	}
}

func TestMRCanonicalRoundTrips(t *testing.T) {
	p := Project{Host: "gl.example.com:8443", ID: "a%2Fb%2Fc"}
	c := MRCanonical(p, 9)
	if c != "gl.example.com:8443/a/b/c!9" {
		t.Fatalf("canonical = %q", c)
	}
	if got, iid, err := ParseMRCanonical(c); err != nil || got != p || iid != 9 {
		t.Errorf("ParseMRCanonical = %+v, %d, %v", got, iid, err)
	}
	for _, bad := range []string{"", "host", "host/p", "host/!3", "host/p!x", "/p!3"} {
		if _, _, err := ParseMRCanonical(bad); err == nil {
			t.Errorf("ParseMRCanonical(%q) accepted", bad)
		}
	}
}

func TestGetMRDecodesTitle(t *testing.T) {
	fakeGlab(t, "HTTP/2.0 200 OK\n\n"+`{"iid":7,"state":"opened","title":"Add thing","web_url":"https://gl/p/-/merge_requests/7"}`)
	if mr, err := GetMR(proj, 7); err != nil || mr.Title != "Add thing" {
		t.Errorf("GetMR = %+v, %v", mr, err)
	}
}

func TestCloseOpenMR(t *testing.T) {
	for _, tc := range []struct {
		name   string
		open   *MR
		want   int
		closed []int
	}{
		{"none open", nil, 0, nil},
		{"open", &MR{IID: 7}, 7, []int{7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var closed []int
			iid, err := CloseOpenMR(Project{},
				"b", func(Project, string) (*MR, error) { return tc.open, nil },
				func(_ Project, iid int) error { closed = append(closed, iid); return nil })
			if err != nil || iid != tc.want || !reflect.DeepEqual(closed, tc.closed) {
				t.Errorf("iid %d closed %v err %v, want %d %v", iid, closed, err, tc.want, tc.closed)
			}
		})
	}
}

func TestAwardEmoji(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 201 Created\n\n{}", "HTTP/2.0 404 Not Found\n\n{}", "HTTP/2.0 403 Forbidden\n\n{}")
	if err := AwardEmoji(proj, 7, 42, "eyes"); err != nil {
		t.Fatal(err)
	}
	if err := AwardEmoji(proj, 7, 42, "eyes"); !errors.Is(err, ErrNotFound) {
		t.Errorf("404 = %v, want ErrNotFound", err)
	}
	if err := AwardEmoji(proj, 7, 42, "eyes"); !errors.Is(err, ErrForbidden) {
		t.Errorf("403 = %v, want ErrForbidden", err)
	}
	b, _ := os.ReadFile(log)
	if want := "-X POST -f name=eyes projects/ns%2Fp/merge_requests/7/notes/42/award_emoji\n"; !strings.Contains(string(b), want) {
		t.Errorf("argv %q lacks %q", b, want)
	}
}

func TestGetPipelineReadsStatus(t *testing.T) {
	log := fakeGlab(t, "HTTP/2.0 200 OK\n\n"+`{"id":42,"status":"running","ref":"main"}`)
	pl, err := GetPipeline(proj, 42)
	if err != nil || pl.Status != "running" || pl.ID != 42 {
		t.Fatalf("GetPipeline = %+v, %v", pl, err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "projects/ns%2Fp/pipelines/42") {
		t.Errorf("argv %q lacks the pipeline endpoint", b)
	}
}

func TestMainPipelineReadsTheLatestOnTheDefaultBranch(t *testing.T) {
	log := fakeGlab(t,
		"HTTP/2.0 200 OK\n\n"+`{"default_branch":"trunk"}`,
		"HTTP/2.0 200 OK\n\n"+`[{"id":9,"status":"success"},{"id":8,"status":"failed"}]`,
		"HTTP/2.0 200 OK\n\n"+`{"default_branch":"trunk"}`,
		"HTTP/2.0 200 OK\n\n[]")
	pl, err := MainPipeline(proj)
	if err != nil || pl == nil || pl.ID != 9 {
		t.Fatalf("MainPipeline = %+v, %v", pl, err)
	}
	b, _ := os.ReadFile(log)
	if !strings.Contains(string(b), "pipelines?") || !strings.Contains(string(b), "ref=trunk") {
		t.Errorf("argv %q lacks the default-branch pipeline list", b)
	}
	if pl, err := MainPipeline(proj); err != nil || pl != nil {
		t.Errorf("no pipelines = %+v, %v; want nil, nil", pl, err)
	}
}
