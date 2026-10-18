package git

import (
	"slices"
	"testing"
)

func TestGlabHelperEnv(t *testing.T) {
	helper := func(origin string) []string {
		return []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=credential." + origin + ".helper", "GIT_CONFIG_VALUE_0=!glab auth git-credential"}
	}
	for url, want := range map[string][]string{
		"https://gitlab.com/ns/p.git":       helper("https://gitlab.com"),
		"http://gitlab.local:8080/ns/p.git": helper("http://gitlab.local:8080"),
		"git@gitlab.com:ns/p.git":           nil,
		"ssh://git@gitlab.com/ns/p.git":     nil,
	} {
		if got := GlabHelperEnv(url); !slices.Equal(got, want) {
			t.Errorf("GlabHelperEnv(%q) = %v, want %v", url, got, want)
		}
	}
}
