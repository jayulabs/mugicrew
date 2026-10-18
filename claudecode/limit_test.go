package claudecode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// limitJSON is a captured result blob (research note, §2/§4) with the varying columns.
func limitJSON(status int, apiErr, result string) string {
	ae := ""
	if apiErr != "" {
		ae = fmt.Sprintf(`"api_error":%q,`, apiErr)
	}
	return fmt.Sprintf(`{"subtype":"success","is_error":true,"terminal_reason":"api_error","api_error_status":%d,%s"result":%q,"type":"result","total_cost_usd":0}`, status, ae, result)
}

func runLimit(t *testing.T, blob string) (contract.Result, error) {
	t.Helper()
	stubExec(t, "cat <<'EOF_JSON'\n"+blob+"\nEOF_JSON\nexit 1")
	return New("").Run(nil, contract.Invocation{Prompt: "p", SessionID: "sess-1"})
}

func TestRunClassifiesPlanLimit(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	for _, tc := range []struct {
		name, blob string
		limited    bool
	}{
		{"session", limitJSON(429, "usage_limit_reached", "You've hit your session limit · resets 3:43pm (America/Los_Angeles)"), true},
		{"weekly", limitJSON(429, "usage_limit_reached", "You've hit your weekly limit · resets 3:44pm (America/Los_Angeles)"), true},
		{"weekly 3d", limitJSON(429, "usage_limit_reached", "You've hit your weekly limit · resets Oct 16, 1:44pm (America/Los_Angeles)"), true},
		{"opus", limitJSON(429, "usage_limit_reached", "You've hit your Opus limit · resets 3:44pm (America/Los_Angeles)"), true},
		{"no reset", limitJSON(429, "usage_limit_reached", "You've hit your session limit"), true},
		{"utc", limitJSON(429, "usage_limit_reached", "You've hit your session limit · resets 10:14pm (UTC)"), true},
		{"throttle 429", limitJSON(429, "", "API Error: Server is temporarily limiting requests (not your usage limit) · Rate limited"), false},
		{"529", limitJSON(529, "", "API Error: 529 Overloaded. This is a server-side issue"), false},
		{"spend cap", limitJSON(429, "usage_limit_reached", "You've reached your monthly spend limit · resets Nov 1"), false},
		{"credits", limitJSON(429, "usage_limit_reached", "You're out of usage credits · resets 3pm (UTC)"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := runLimit(t, tc.blob)
			var le *contract.LimitError
			if got := errors.As(err, &le); got != tc.limited {
				t.Fatalf("limited = %v (err %v), want %v", got, err, tc.limited)
			}
			if err == nil {
				t.Fatal("a limit hit is still an error")
			}
			if res.Summary.APIErrorStatus == 0 {
				t.Errorf("Summary lacks api_error_status: %+v", res.Summary)
			}
		})
	}
}

// A non-limit failure returns exactly the error it returned before.
func TestRunNonLimitErrorUnchanged(t *testing.T) {
	_, err := runLimit(t, limitJSON(529, "", "API Error: 529 Overloaded"))
	if err == nil || err.Error() != "claude -p: exit status 1" {
		t.Errorf("err = %v, want claude -p: exit status 1", err)
	}
	stubExec(t, `printf '%s' '{"is_error":true,"terminal_reason":"api_error","result":"boom"}'`)
	_, err = New("").Run(nil, contract.Invocation{Prompt: "p"})
	if err == nil || err.Error() != "claude -p: is_error (api_error): boom" {
		t.Errorf("err = %v", err)
	}
}

func TestLimitErrorUnwraps(t *testing.T) {
	inner := errors.New("inner")
	var err error = &contract.LimitError{Limit: contract.Limit{Source: "message"}, Err: inner}
	if !errors.Is(err, inner) || err.Error() != "inner" {
		t.Errorf("LimitError = %v, want unwrap to inner", err)
	}
}

// fixed clock: 2026-10-13 12:00 UTC.
var clock = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func fixClock(t *testing.T) {
	t.Helper()
	oldNow, oldUsage := now, readUsage
	now = func() time.Time { return clock }
	readUsage = func(string) (contract.PlanUsage, string, error) {
		return contract.PlanUsage{}, "", errors.New("no usage")
	}
	t.Cleanup(func() { now, readUsage = oldNow, oldUsage })
}

func writeTranscript(t *testing.T, sid, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	p := filepath.Join(dir, "projects", "proj")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, sid+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResetTime(t *testing.T) {
	la, _ := time.LoadLocation("America/Los_Angeles")
	res := func(s string) contract.Summary { return contract.Summary{Result: s} }
	for _, tc := range []struct {
		name   string
		sum    contract.Summary
		usage  contract.PlanUsage
		want   time.Time
		source string
	}{
		{"today", res("You've hit your session limit · resets 10:14pm (UTC)"), contract.PlanUsage{},
			time.Date(2026, 10, 13, 22, 14, 0, 0, time.UTC), "message"},
		{"hour only", res("You've hit your session limit · resets 3pm (UTC)"), contract.PlanUsage{},
			time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC), "message"},
		{"rolled +24h", res("You've hit your session limit · resets 9am (UTC)"), contract.PlanUsage{},
			time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC), "message"},
		{"zone", res("You've hit your session limit · resets 3:43pm (America/Los_Angeles)"), contract.PlanUsage{},
			time.Date(2026, 10, 13, 15, 43, 0, 0, la), "message"},
		{"month day", res("You've hit your weekly limit · resets Oct 16, 1:44pm (America/Los_Angeles)"), contract.PlanUsage{},
			time.Date(2026, 10, 16, 13, 44, 0, 0, la), "message"},
		{"month day hour", res("You've hit your weekly limit · resets Oct 16, 1pm (UTC)"), contract.PlanUsage{},
			time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC), "message"},
		{"jan 2 next year", res("You've hit your weekly limit · resets Jan 2, 9am (UTC)"), contract.PlanUsage{},
			time.Date(2027, 1, 2, 9, 0, 0, 0, time.UTC), "message"},
		{"year-bearing", res("You've hit your weekly limit · resets Jan 2, 2027, 9am (UTC)"), contract.PlanUsage{},
			time.Date(2027, 1, 2, 9, 0, 0, 0, time.UTC), "message"},
		{"year-bearing minutes", res("You've hit your weekly limit · resets Jan 2, 2027, 9:05am (UTC)"), contract.PlanUsage{},
			time.Date(2027, 1, 2, 9, 5, 0, 0, time.UTC), "message"},
		{"unknown zone, no usage", res("You've hit your session limit · resets 3pm (Mars/Base)"), contract.PlanUsage{},
			clock.Add(time.Hour), "fallback"},
		{"no reset text, no usage", res("You've hit your session limit"), contract.PlanUsage{},
			clock.Add(time.Hour), "fallback"},
		{"usage latest of windows at 100", res("You've hit your session limit"),
			contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 100, ResetsAt: clock.Add(2 * time.Hour)},
				Weekly: &contract.UsageWindow{Pct: 100, ResetsAt: clock.Add(50 * time.Hour)}},
			clock.Add(50 * time.Hour), "usage"},
		{"usage ignores window under 100", res("You've hit your session limit"),
			contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 100, ResetsAt: clock.Add(2 * time.Hour)},
				Weekly: &contract.UsageWindow{Pct: 40, ResetsAt: clock.Add(50 * time.Hour)}},
			clock.Add(2 * time.Hour), "usage"},
		{"usage zero ResetsAt is none", res("You've hit your session limit"),
			contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 100}}, clock.Add(time.Hour), "fallback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixClock(t)
			t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // no transcript
			readUsage = func(string) (contract.PlanUsage, string, error) { return tc.usage, "", nil }
			got := resetTime("none", tc.sum)
			if !got.ResetsAt.Equal(tc.want) || got.Source != tc.source {
				t.Errorf("resetTime = %v/%s, want %v/%s", got.ResetsAt, got.Source, tc.want, tc.source)
			}
		})
	}
}

func TestResetTimeFromTranscriptWinsOverProse(t *testing.T) {
	fixClock(t)
	writeTranscript(t, "s1", `{"type":"user"}
{"isApiErrorMessage":true,"apiErrorParams":{"rate_limit_info":{"status":"rejected","resetsAt":1000}}}
{"isApiErrorMessage":true,"apiErrorStatus":429,"apiErrorParams":{"rate_limit_info":{"status":"rejected","resetsAt":1791931435,"rateLimitType":"five_hour"}}}
{"type":"cost-state"}
`)
	got := resetTime("s1", contract.Summary{Result: "You've hit your session limit · resets 3pm (UTC)"})
	if want := time.Unix(1791931435, 0); !got.ResetsAt.Equal(want) || got.Source != "message" {
		t.Errorf("resetTime = %v/%s, want %v/message", got.ResetsAt, got.Source, want)
	}
}

func TestResetTimeTranscriptWithoutResetFallsToProse(t *testing.T) {
	fixClock(t)
	writeTranscript(t, "s1", `{"isApiErrorMessage":true,"apiErrorParams":{}}`+"\n")
	got := resetTime("s1", contract.Summary{Result: "You've hit your session limit · resets 3pm (UTC)"})
	if want := time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC); !got.ResetsAt.Equal(want) || got.Source != "message" {
		t.Errorf("resetTime = %v/%s, want %v/message", got.ResetsAt, got.Source, want)
	}
}
