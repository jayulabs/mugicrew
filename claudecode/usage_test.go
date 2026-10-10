package claudecode

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// ReadUsage (Spec §2): reads the claude.ai token from the credentials
// file, GETs /api/oauth/usage, and returns the 5-hour and weekly Plan usage
// windows; every unavailable condition's error text is the band's UI reason.
func TestReadUsage(t *testing.T) {
	future := time.Now().Add(time.Hour).UnixMilli()
	past := time.Now().Add(-time.Hour).UnixMilli()
	creds := func(token string, expiresAt int64, scopes string) string {
		return fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d,"scopes":[%s],"subscriptionType":"max"},"other":1}`, token, expiresAt, scopes)
	}
	good := creds("tok-1", future, `"user:inference","user:profile"`)
	notSignedIn := "not signed in to claude.ai"
	if runtime.GOOS == "darwin" {
		notSignedIn += " (Keychain not read)"
	}
	const both = `{"five_hour":{"utilization":13.0,"resets_at":"2026-10-10T01:09:59.526215+00:00","x":1},
		"seven_day":{"utilization":3.5,"resets_at":"2026-10-16T10:59:59.526237+00:00"},
		"seven_day_opus":null,"extra_usage":{"is_enabled":true},"limits":[{"kind":"session"}]}`
	fiveReset := time.Date(2026, 10, 10, 1, 9, 59, 526215000, time.UTC)
	weekReset := time.Date(2026, 10, 16, 10, 59, 59, 526237000, time.UTC)

	for _, tc := range []struct {
		name      string
		creds     string // "" = no credentials file
		skip      string
		status    int
		header    map[string]string
		body      string
		want      contract.PlanUsage
		wantToken string
		wantErr   string
		wantAfter time.Duration // -1 = not a RetryAfterError
		wantSent  bool
	}{
		{name: "both windows", creds: good, status: 200, body: both, wantToken: "tok-1", wantAfter: -1, wantSent: true,
			want: contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 13, ResetsAt: fiveReset}, Weekly: &contract.UsageWindow{Pct: 3.5, ResetsAt: weekReset}}},
		{name: "one window", creds: good, status: 200, body: `{"five_hour":{"utilization":84,"resets_at":"garbage"},"seven_day":null}`, wantToken: "tok-1", wantAfter: -1, wantSent: true,
			want: contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 84}}},
		{name: "neither window", creds: good, status: 200, body: `{"seven_day_opus":null}`, wantToken: "tok-1", wantErr: "no plan windows", wantAfter: -1, wantSent: true},
		{name: "changed shape", creds: good, status: 200, body: `{"five_hour":{"utilization":"13%"}}`, wantToken: "tok-1", wantErr: "no plan windows", wantAfter: -1, wantSent: true},
		{name: "missing file", wantErr: notSignedIn, wantAfter: -1},
		{name: "missing entry", creds: `{"mcpOAuth":{}}`, wantErr: notSignedIn, wantAfter: -1},
		{name: "missing scope", creds: creds("tok-1", future, `"user:inference"`), wantToken: "tok-1", wantErr: "token lacks user:profile scope", wantAfter: -1},
		{name: "expired expiresAt", creds: creds("tok-1", past, `"user:profile"`), wantToken: "tok-1", wantErr: "token expired", wantAfter: -1},
		{name: "skipToken match", creds: good, skip: "tok-1", wantToken: "tok-1", wantErr: "token expired", wantAfter: -1},
		{name: "skipToken stale", creds: good, skip: "tok-0", status: 200, body: both, wantToken: "tok-1", wantAfter: -1, wantSent: true,
			want: contract.PlanUsage{FiveHour: &contract.UsageWindow{Pct: 13, ResetsAt: fiveReset}, Weekly: &contract.UsageWindow{Pct: 3.5, ResetsAt: weekReset}}},
		{name: "401", creds: good, status: 401, wantToken: "tok-1", wantErr: "token expired", wantAfter: -1, wantSent: true},
		{name: "429 with retry-after", creds: good, status: 429, header: map[string]string{"Retry-After": "120"}, wantToken: "tok-1", wantErr: "rate-limited", wantAfter: 120 * time.Second, wantSent: true},
		{name: "429 without retry-after", creds: good, status: 429, wantToken: "tok-1", wantErr: "rate-limited", wantAfter: 0, wantSent: true},
		{name: "403", creds: good, status: 403, header: map[string]string{"Retry-After": "7"}, wantToken: "tok-1", wantErr: "rate-limited", wantAfter: 7 * time.Second, wantSent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", dir)
			if tc.creds != "" {
				if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(tc.creds), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			sent := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sent = true
				if r.Method != http.MethodGet || r.URL.Path != "/api/oauth/usage" {
					t.Errorf("request = %s %s, want GET /api/oauth/usage", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
					t.Errorf("Authorization = %q, want Bearer tok-1", got)
				}
				if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
					t.Errorf("anthropic-beta = %q, want oauth-2025-04-20", got)
				}
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			setUsageBase(t, srv.URL)

			got, token, err := ReadUsage(tc.skip)
			if sent != tc.wantSent {
				t.Errorf("request sent = %v, want %v", sent, tc.wantSent)
			}
			if token != tc.wantToken {
				t.Errorf("token = %q, want %q", token, tc.wantToken)
			}
			if gotErr := errText(err); gotErr != tc.wantErr {
				t.Errorf("err = %q, want %q", gotErr, tc.wantErr)
			}
			var ra *RetryAfterError
			if errors.As(err, &ra) != (tc.wantAfter >= 0) {
				t.Errorf("errors.As RetryAfterError = %v, want %v", ra != nil, tc.wantAfter >= 0)
			} else if ra != nil && ra.After != tc.wantAfter {
				t.Errorf("After = %v, want %v", ra.After, tc.wantAfter)
			}
			if !sameWindow(got.FiveHour, tc.want.FiveHour) || !sameWindow(got.Weekly, tc.want.Weekly) {
				t.Errorf("usage = %s/%s, want %s/%s", fmtWindow(got.FiveHour), fmtWindow(got.Weekly), fmtWindow(tc.want.FiveHour), fmtWindow(tc.want.Weekly))
			}
		})
	}
}

// A transport failure (here, a closed server) reads as "network error".
func TestReadUsageNetworkError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	creds := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok-1","expiresAt":%d,"scopes":["user:profile"]}}`, time.Now().Add(time.Hour).UnixMilli())
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	setUsageBase(t, srv.URL)
	if _, _, err := ReadUsage(""); errText(err) != "network error" {
		t.Errorf("err = %q, want network error", errText(err))
	}
}

func setUsageBase(t *testing.T, url string) {
	old := usageBase
	usageBase = url
	t.Cleanup(func() { usageBase = old })
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func sameWindow(a, b *contract.UsageWindow) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Pct == b.Pct && a.ResetsAt.Equal(b.ResetsAt))
}

func fmtWindow(w *contract.UsageWindow) string {
	if w == nil {
		return "nil"
	}
	return fmt.Sprintf("{%v %v}", w.Pct, w.ResetsAt)
}
