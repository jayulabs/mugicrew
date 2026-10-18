package contract

import "testing"

func TestChartSummary(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"both", "## Destination\n\nGo here.\n\n## Notes\nn\n## Decisions-so-far\n- a\n- b\n\n## Out-of-scope\nx\n", "Go here.\n\n- a\n- b"},
		{"empty decisions", "## Destination\n\nGo here.\n\n## Decisions-so-far\n\n## Notes\nn\n", "Go here."},
		{"no decisions section", "## Destination\nGo here.\n", "Go here."},
		{"neither", "just prose", ""},
	} {
		if got := ChartSummary(tc.body); got != tc.want {
			t.Errorf("%s: ChartSummary = %q, want %q", tc.name, got, tc.want)
		}
	}
}
