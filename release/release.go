// Package release is the npm Release notice's leaf: whether a build checks at
// all, the registry read of the @jayulabs/mugicrew packument (the latest dist-tag,
// the published versions, and which declare a needs-a-human-step marker), its 24h
// cache, the strict newer-than test, and the safe Upgrade target. It reads no Bead
// Graph. Every failure is silent: a check that cannot finish gives no answer, never
// an error to print.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RegistryURL is the npm registry's base URL; a var so tests point it at an
// httptest server.
var RegistryURL = "https://registry.npmjs.org"

// Timeout bounds one registry read.
var Timeout = 3 * time.Second

// TTL is how long a cached answer stands before a check runs again.
const TTL = 24 * time.Hour

// Info is what one registry read learns: the latest dist-tag, every published
// strict-semver version (sorted), and those that carry an honoured
// needs-a-human-step marker (sorted).
type Info struct {
	Latest    string
	Versions  []string
	HumanStep []string
}

// Fetch is the registry read of the @jayulabs/mugicrew packument. A var so the
// upgrade verb and the TUI stub it; they ask fresh through Check(true).
var Fetch = fetchInfo

// Now is the clock; a var so tests age the cache.
var Now = time.Now

// CachePath is the cache file under the user cache dir; a var so tests move it.
// "" when the user has no cache dir.
var CachePath = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mugicrew", "release.json")
}

// Eligible reports whether a build checks at all: an npm build is stamped with a
// version and not with a sourceRoot. A from-source build never checks or notices.
func Eligible(version, sourceRoot string) bool {
	return version != "" && sourceRoot == ""
}

type cacheFile struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	// Versions and HumanStep are nil when an older binary wrote the cache.
	Versions  []string `json:"versions,omitempty"`
	HumanStep []string `json:"human_step"`
}

func readCache() (cacheFile, bool) {
	var c cacheFile
	p := CachePath()
	if p == "" {
		return c, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &c) != nil || c.Latest == "" {
		return cacheFile{}, false
	}
	return c, true
}

// Cached is the last answer the cache holds, however old; "" when there is none.
func Cached() string {
	c, _ := readCache()
	return c.Latest
}

// Facts is the cached registry facts, however old. known is false when there is no
// cache or an older binary wrote it without human_step: unattended Upgrade must not
// act on such a cache, though it still answers the Release notice.
func Facts() (info Info, known bool) {
	c, ok := readCache()
	if !ok {
		return Info{}, false
	}
	return Info{Latest: c.Latest, Versions: c.Versions, HumanStep: c.HumanStep}, c.HumanStep != nil
}

func writeCache(info Info) {
	p := CachePath()
	if p == "" {
		return
	}
	b, err := json.Marshal(cacheFile{CheckedAt: Now().UTC(), Latest: info.Latest, Versions: info.Versions, HumanStep: nonNil(info.HumanStep)})
	if err != nil || os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "release-*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), p) != nil {
		os.Remove(tmp.Name())
	}
}

// Check returns the latest release. Unless fresh, a cache younger than TTL answers
// with no request; otherwise it asks the registry and, on success only, caches the
// answer. A failed read writes nothing and returns the error for a caller that
// wants it; the Release notice drops it.
func Check(fresh bool) (string, error) {
	if !fresh {
		if c, ok := readCache(); ok {
			if age := Now().Sub(c.CheckedAt); age >= 0 && age < TTL {
				return c.Latest, nil
			}
		}
	}
	info, err := Fetch()
	if err != nil {
		return "", err
	}
	writeCache(info)
	return info.Latest, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func fetchInfo() (Info, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(RegistryURL, "/")+"/@jayulabs/mugicrew", nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Info{}, errors.New("registry: " + resp.Status)
	}
	var doc struct {
		DistTags struct {
			Latest string `json:"latest"`
		} `json:"dist-tags"`
		Versions map[string]struct {
			Version  string `json:"version"`
			Mugicrew struct {
				HumanStep struct {
					Version string `json:"version"`
				} `json:"humanStep"`
			} `json:"mugicrew"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return Info{}, err
	}
	if doc.DistTags.Latest == "" {
		return Info{}, errors.New("registry: no latest dist-tag")
	}
	info := Info{Latest: doc.DistTags.Latest, Versions: []string{}, HumanStep: []string{}}
	for v, obj := range doc.Versions {
		if _, ok := parse(v); !ok {
			continue
		}
		info.Versions = append(info.Versions, v)
		// A marker is honoured only for the version it names, so a stale one is inert.
		if obj.Mugicrew.HumanStep.Version == v && obj.Version == v {
			info.HumanStep = append(info.HumanStep, v)
		}
	}
	sortVersions(info.Versions)
	sortVersions(info.HumanStep)
	return info, nil
}

func sortVersions(vs []string) {
	sort.Slice(vs, func(i, j int) bool { return Newer(vs[j], vs[i]) })
}

// Target picks the unattended Upgrade target: the highest strict v with
// running < v <= latest and no marked version in (running, v]. blockedBy names the
// first marked version above running, or "". Target is "" when nothing is newer or
// the first newer version is marked.
func Target(running, latest string, versions, humanStep []string) (target, blockedBy string) {
	marked := map[string]bool{}
	for _, v := range humanStep {
		marked[v] = true
	}
	cands := []string{}
	for _, v := range versions {
		if Newer(v, running) && !Newer(v, latest) {
			cands = append(cands, v)
		}
	}
	sortVersions(cands)
	for _, v := range cands {
		if marked[v] {
			return target, v
		}
		target = v
	}
	return target, ""
}

// parse reads a strict MAJOR.MINOR.PATCH; a pre-release or anything else is not ok.
func parse(v string) (p [3]int, ok bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return p, false
	}
	for i, s := range parts {
		if s == "" || strings.TrimLeft(s, "0123456789") != "" {
			return p, false
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return p, false
		}
		p[i] = n
	}
	return p, true
}

// Newer reports whether latest is strictly greater than running, both strict
// MAJOR.MINOR.PATCH. A pre-release or unparsable version on either side is false.
func Newer(latest, running string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	r, ok := parse(running)
	if !ok {
		return false
	}
	for i := range l {
		if l[i] != r[i] {
			return l[i] > r[i]
		}
	}
	return false
}

// Strict reports whether v is a strict MAJOR.MINOR.PATCH release.
func Strict(v string) bool {
	_, ok := parse(v)
	return ok
}
