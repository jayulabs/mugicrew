package claudecode

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"time"

	"jayulabs.org/mugicrew/contract"
)

// usageBase is the OAuth usage endpoint's host, indirected for tests.
var usageBase = "https://api.anthropic.com"

// ErrTokenExpired is the reason when the token's expiresAt has passed, the
// endpoint answered 401, or the token is the skipToken that last got a 401.
var ErrTokenExpired = errors.New("token expired")

// RetryAfterError is a 429/403 from the usage endpoint: back off for After (0 =
// the endpoint gave no retry-after; the caller picks its default).
type RetryAfterError struct{ After time.Duration }

func (*RetryAfterError) Error() string { return "rate-limited" }

// ReadUsage reads the account-wide Plan usage the way Claude Code's /usage does
// (Spec §2): the claude.ai OAuth token from <config dir>/.credentials.json,
// then GET /api/oauth/usage. Blocking and stateless; it never writes or refreshes the
// credentials. token is the access token it found (empty when none), so the
// caller can pass one that got a 401 back as skipToken, which then sends nothing.
// Each error's text is the human reason Plan usage is unavailable.
func ReadUsage(skipToken string) (usage contract.PlanUsage, token string, err error) {
	var creds struct {
		OAuth *struct {
			AccessToken string   `json:"accessToken"`
			ExpiresAt   int64    `json:"expiresAt"` // unix ms
			Scopes      []string `json:"scopes"`
		} `json:"claudeAiOauth"`
	}
	dir, err := configDir()
	if err == nil {
		var b []byte
		if b, err = os.ReadFile(filepath.Join(dir, ".credentials.json")); err == nil {
			err = json.Unmarshal(b, &creds)
		}
	}
	if err != nil || creds.OAuth == nil || creds.OAuth.AccessToken == "" {
		reason := "not signed in to claude.ai"
		if runtime.GOOS == "darwin" {
			reason += " (Keychain not read)" // ponytail: the macOS Keychain is out of scope
		}
		return usage, "", errors.New(reason)
	}
	token = creds.OAuth.AccessToken
	switch {
	case !slices.Contains(creds.OAuth.Scopes, "user:profile"):
		return usage, token, errors.New("token lacks user:profile scope")
	case creds.OAuth.ExpiresAt != 0 && time.Now().UnixMilli() >= creds.OAuth.ExpiresAt, token == skipToken:
		return usage, token, ErrTokenExpired
	}

	req, err := http.NewRequest(http.MethodGet, usageBase+"/api/oauth/usage", nil)
	if err != nil {
		return usage, token, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return usage, token, errors.New("network error")
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return usage, token, ErrTokenExpired
	case http.StatusTooManyRequests, http.StatusForbidden:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After")) // ponytail: delta-seconds only; an HTTP-date reads as absent
		return usage, token, &RetryAfterError{After: time.Duration(max(secs, 0)) * time.Second}
	default:
		return usage, token, errors.New("network error") // ponytail: a 5xx is the server's, so it reads as the network's
	}

	// The endpoint is undocumented: take only what /usage itself depends on, and
	// read any other shape as no plan windows.
	type window struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	}
	var body struct {
		FiveHour *window `json:"five_hour"`
		SevenDay *window `json:"seven_day"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return usage, token, errors.New("no plan windows")
	}
	conv := func(w *window) *contract.UsageWindow {
		if w == nil || w.Utilization == nil {
			return nil
		}
		at, _ := time.Parse(time.RFC3339Nano, w.ResetsAt) // unparseable = zero = unknown
		return &contract.UsageWindow{Pct: *w.Utilization, ResetsAt: at}
	}
	usage = contract.PlanUsage{FiveHour: conv(body.FiveHour), Weekly: conv(body.SevenDay)}
	if usage.FiveHour == nil && usage.Weekly == nil {
		return usage, token, errors.New("no plan windows")
	}
	return usage, token, nil
}
