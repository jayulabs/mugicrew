package release

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// registry points the package at a server answering h and counts its requests.
func registry(t *testing.T, h http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	oldURL, oldCache, oldTimeout := RegistryURL, CachePath, Timeout
	cache := filepath.Join(t.TempDir(), "mugicrew", "release.json")
	RegistryURL, CachePath = srv.URL, func() string { return cache }
	t.Cleanup(func() { RegistryURL, CachePath, Timeout = oldURL, oldCache, oldTimeout })
	return &n
}

func latest(v string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"@jayulabs/mugicrew","dist-tags":{"latest":"` + v + `"}}`))
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, running string
		want            bool
	}{
		{"1.2.4", "1.2.3", true},
		{"1.3.0", "1.2.9", true},
		{"2.0.0", "1.9.9", true},
		{"1.10.0", "1.9.0", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.2", "1.2.3", false},
		{"1.3.0-rc.1", "1.2.3", false},
		{"banana", "1.2.3", false},
		{"1.2.4", "unknown", false},
		{"1.2", "1.1.0", false},
	} {
		if got := Newer(tc.latest, tc.running); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v; want %v", tc.latest, tc.running, got, tc.want)
		}
	}
}

func TestEligible(t *testing.T) {
	if !Eligible("1.0.0", "") {
		t.Error("npm build not eligible")
	}
	if Eligible("1.0.0", "/src") || Eligible("", "") {
		t.Error("source or unstamped build eligible")
	}
}

func TestCheckReadsLatestAndCaches(t *testing.T) {
	n := registry(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/@jayulabs/mugicrew" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("request %s accept %q", r.URL.Path, r.Header.Get("Accept"))
		}
		latest("1.4.0")(w, r)
	})
	got, err := Check(false)
	if err != nil || got != "1.4.0" {
		t.Fatalf("Check = %q, %v", got, err)
	}
	if Cached() != "1.4.0" {
		t.Errorf("Cached = %q", Cached())
	}
	if got, _ := Check(false); got != "1.4.0" || n.Load() != 1 {
		t.Errorf("fresh cache: got %q after %d requests; want 1", got, n.Load())
	}
	if _, err := Check(true); err != nil || n.Load() != 2 {
		t.Errorf("fresh ask: err %v, %d requests; want 2", err, n.Load())
	}
}

func TestCheckStaleCacheAsksOnce(t *testing.T) {
	n := registry(t, latest("1.5.0"))
	if _, err := Check(false); err != nil {
		t.Fatal(err)
	}
	old := Now
	t.Cleanup(func() { Now = old })
	Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	if got, _ := Check(false); got != "1.5.0" || n.Load() != 2 {
		t.Errorf("stale cache: got %q after %d requests; want one more", got, n.Load())
	}
}

func TestCheckFailuresWriteNothing(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"malformed": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("{nope")) },
		"404":       func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"no latest": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"dist-tags":{}}`)) },
		"slow": func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry(t, h)
			Timeout = 100 * time.Millisecond
			if got, err := Check(false); err == nil || got != "" {
				t.Errorf("Check = %q, %v; want an error", got, err)
			}
			if _, err := os.Stat(CachePath()); err == nil {
				t.Error("failed check wrote the cache")
			}
		})
	}
}

func TestPreReleaseLatestGivesNoNotice(t *testing.T) {
	registry(t, latest("2.0.0-beta.1"))
	got, err := Check(false)
	if err != nil || Newer(got, "1.0.0") {
		t.Errorf("Check = %q, %v; want no newer", got, err)
	}
}

func TestUnreadableCacheChecks(t *testing.T) {
	n := registry(t, latest("1.6.0"))
	os.MkdirAll(filepath.Dir(CachePath()), 0o755)
	os.WriteFile(CachePath(), []byte("garbage"), 0o644)
	if got, _ := Check(false); got != "1.6.0" || n.Load() != 1 {
		t.Errorf("got %q after %d requests", got, n.Load())
	}
}

// packument answers the full packument: latest plus the given version objects, each
// a raw JSON body.
func packument(latest string, versions map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var vs []string
		for v, body := range versions {
			vs = append(vs, `"`+v+`":`+body)
		}
		w.Write([]byte(`{"dist-tags":{"latest":"` + latest + `"},"versions":{` + strings.Join(vs, ",") + `}}`))
	}
}

func marker(v string) string {
	return `{"version":"` + v + `","mugicrew":{"humanStep":{"version":"` + v + `","doc":"docs/upgrading.md#x"}}}`
}

func TestFetchDecodesVersionsAndHonouredMarkers(t *testing.T) {
	registry(t, packument("1.3.0", map[string]string{
		"1.0.0":      `{"version":"1.0.0"}`,
		"1.1.0":      marker("1.1.0"),
		"1.2.0":      `{"version":"1.2.0","mugicrew":{"humanStep":{"version":"1.1.0","doc":"docs/upgrading.md#x"}}}`, // stale
		"1.3.0":      `{"version":"1.3.0"}`,
		"2.0.0-beta": marker("2.0.0-beta"),
		"junk":       `{"version":"junk"}`,
	}))
	info, err := Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if info.Latest != "1.3.0" {
		t.Errorf("latest = %q", info.Latest)
	}
	if want := []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0"}; !reflect.DeepEqual(info.Versions, want) {
		t.Errorf("versions = %v, want %v", info.Versions, want)
	}
	if want := []string{"1.1.0"}; !reflect.DeepEqual(info.HumanStep, want) {
		t.Errorf("human step = %v, want %v", info.HumanStep, want)
	}
}

func TestCacheRoundTripsFacts(t *testing.T) {
	registry(t, packument("1.2.0", map[string]string{"1.1.0": marker("1.1.0"), "1.2.0": `{"version":"1.2.0"}`}))
	if _, err := Check(false); err != nil {
		t.Fatal(err)
	}
	info, known := Facts()
	if !known || info.Latest != "1.2.0" || !reflect.DeepEqual(info.Versions, []string{"1.1.0", "1.2.0"}) ||
		!reflect.DeepEqual(info.HumanStep, []string{"1.1.0"}) {
		t.Errorf("Facts = %+v, %v", info, known)
	}
}

func TestOldShapeCacheAnswersNoticeButIsUnknown(t *testing.T) {
	n := registry(t, latest("9.9.9"))
	os.MkdirAll(filepath.Dir(CachePath()), 0o755)
	b := `{"checked_at":"` + time.Now().UTC().Format(time.RFC3339) + `","latest":"1.7.0"}`
	os.WriteFile(CachePath(), []byte(b), 0o644)
	if got, err := Check(false); err != nil || got != "1.7.0" || n.Load() != 0 {
		t.Errorf("notice: %q, %v after %d requests", got, err, n.Load())
	}
	if _, known := Facts(); known {
		t.Error("old-shape cache known for unattended Upgrade")
	}
}

func TestTarget(t *testing.T) {
	all := []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0"}
	for _, c := range []struct {
		name, running, latest string
		marked                []string
		target, blocked       string
	}{
		{"none newer", "1.3.0", "1.3.0", nil, "", ""},
		{"all safe", "1.0.0", "1.3.0", nil, "1.3.0", ""},
		{"latest marked", "1.0.0", "1.3.0", []string{"1.3.0"}, "1.2.0", "1.3.0"},
		{"middle marked", "1.0.0", "1.3.0", []string{"1.2.0"}, "1.1.0", "1.2.0"},
		{"first newer marked", "1.0.0", "1.3.0", []string{"1.1.0"}, "", "1.1.0"},
		{"running marked", "1.1.0", "1.3.0", []string{"1.1.0"}, "1.3.0", ""},
		{"unparsable running", "dev", "1.3.0", nil, "", ""},
		{"latest caps", "1.0.0", "1.2.0", nil, "1.2.0", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			target, blocked := Target(c.running, c.latest, all, c.marked)
			if target != c.target || blocked != c.blocked {
				t.Errorf("Target = %q, %q; want %q, %q", target, blocked, c.target, c.blocked)
			}
		})
	}
}
