package tui

import "testing"

func TestMatchWords(t *testing.T) {
	for _, c := range []struct {
		query, text string
		want        bool
	}{
		{"", "anything", true},
		{"   ", "anything", true},
		{"foo", "a Foo bar", true},
		{"FOO bar", "a foo BAR", true},
		{"bar foo", "foo then bar", true},
		{"foo baz", "foo bar", false},
		{"foo.*", "foo bar", false}, // no regex
		{"a.c", "abc", false},
		{"a.c", "a.c", true},
		{"foo", "", false},
		{"\tfoo\n bar ", "foo bar", true},
	} {
		if got := matchWords(c.query, c.text); got != c.want {
			t.Errorf("matchWords(%q, %q) = %v, want %v", c.query, c.text, got, c.want)
		}
	}
}
