package contract

import "testing"

func TestParseForgeReviewCounts(t *testing.T) {
	want := ForgeReviewCounts{2, 1, 3}
	for name, c := range map[string]struct {
		reason string
		counts ForgeReviewCounts
		ok     bool
	}{
		"new filed form":     {"completed: filed 2, folded 1, skipped 3; read through note 9", want, true},
		"legacy form":        {"filed 2, folded 1, skipped 3: read through note 9", want, true},
		"nothing actionable": {"completed: nothing actionable; read 3 notes through 9", ForgeReviewCounts{}, false},
		"unrelated":          {"obsolete: container closed", ForgeReviewCounts{}, false},
	} {
		got, ok := ParseForgeReviewCounts(c.reason)
		if got != c.counts || ok != c.ok {
			t.Errorf("%s: %q = %v, %v; want %v, %v", name, c.reason, got, ok, c.counts, c.ok)
		}
	}
}
