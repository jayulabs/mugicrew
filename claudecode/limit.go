package claudecode

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// now and readUsage are the clock and the usage read, indirected for tests.
var (
	now       = time.Now
	readUsage = ReadUsage
)

// isPlanLimit classifies a parsed result as a Plan usage limit hit (research note
// "What claude -p emits on a Plan usage limit hit", §6): api_error alone cannot
// tell a Plan window from a spend cap, so the wording must be a window's. The exit
// code and error text are never consulted: claude exits 1.
func isPlanLimit(s contract.Summary) bool {
	return s.IsError && s.APIError == "usage_limit_reached" && strings.HasPrefix(s.Result, "You've hit your ")
}

// resetTime is the limit's reset from the first source that yields one: the
// session transcript, then the result prose (both "message"), then ReadUsage
// ("usage"), else now + 1h ("fallback").
func resetTime(sessionID string, s contract.Summary) contract.Limit {
	if at := transcriptReset(sessionID); !at.IsZero() {
		return contract.Limit{ResetsAt: at, Source: "message"}
	}
	if at := proseReset(s.Result, now()); !at.IsZero() {
		return contract.Limit{ResetsAt: at, Source: "message"}
	}
	if u, _, err := readUsage(""); err == nil {
		var at time.Time
		for _, w := range []*contract.UsageWindow{u.FiveHour, u.Weekly} {
			if w != nil && w.Pct >= 100 && w.ResetsAt.After(at) {
				at = w.ResetsAt
			}
		}
		if !at.IsZero() {
			return contract.Limit{ResetsAt: at, Source: "usage"}
		}
	}
	return contract.Limit{ResetsAt: now().Add(time.Hour), Source: "fallback"}
}

// transcriptReset is rate_limit_info.resetsAt (epoch seconds) of the session
// transcript's last API-error line; zero when there is none.
func transcriptReset(sessionID string) time.Time {
	if sessionID == "" {
		return time.Time{}
	}
	path, err := transcript(sessionID)
	if err != nil {
		return time.Time{}
	}
	b, _ := os.ReadFile(path)
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"isApiErrorMessage":true`)) {
			continue
		}
		var l struct {
			Params struct {
				Info struct {
					ResetsAt int64 `json:"resetsAt"`
				} `json:"rate_limit_info"`
			} `json:"apiErrorParams"`
		}
		if json.Unmarshal(lines[i], &l) != nil || l.Params.Info.ResetsAt <= 0 {
			return time.Time{}
		}
		return time.Unix(l.Params.Info.ResetsAt, 0)
	}
	return time.Time{}
}

var resetsRE = regexp.MustCompile(`· resets (.+) \(([^)]+)\)$`)

// proseReset parses the result's "· resets <time> (<Zone>)" tail (research §3);
// zero when it has none or the zone is unknown. The minutes are truncated, so it
// can be up to 59s early; the loop's resume at reset + 1m absorbs that.
func proseReset(result string, at time.Time) time.Time {
	m := resetsRE.FindStringSubmatch(result)
	if m == nil {
		return time.Time{}
	}
	loc, err := time.LoadLocation(m[2])
	if err != nil {
		return time.Time{}
	}
	for _, layout := range []string{"3pm", "3:04pm"} { // within 24h: today in that zone, tomorrow if past
		if t, err := time.ParseInLocation(layout, m[1], loc); err == nil {
			y, mo, d := at.In(loc).Date()
			t = time.Date(y, mo, d, t.Hour(), t.Minute(), 0, 0, loc)
			if !t.After(at) {
				t = t.AddDate(0, 0, 1)
			}
			return t
		}
	}
	for _, layout := range []string{"Jan 2, 3pm", "Jan 2, 3:04pm"} { // no year: this year, next if long past
		if t, err := time.ParseInLocation(layout, m[1], loc); err == nil {
			t = time.Date(at.In(loc).Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
			if at.Sub(t) > 24*time.Hour {
				t = t.AddDate(1, 0, 0)
			}
			return t
		}
	}
	for _, layout := range []string{"Jan 2, 2006, 3pm", "Jan 2, 2006, 3:04pm"} {
		if t, err := time.ParseInLocation(layout, m[1], loc); err == nil {
			return t
		}
	}
	return time.Time{}
}
