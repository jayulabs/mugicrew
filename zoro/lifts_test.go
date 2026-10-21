package zoro

import (
	"strings"
	"testing"
)

// The dirty-main rule sits beside the sweep re-run line.
func TestPivotPromptDirtyMainIsHumans(t *testing.T) {
	got := strings.Join(strings.Fields(pivotPrompt("p", "c", "s")), " ")
	if !strings.Contains(got, "is the human's to clean") {
		t.Error("pivot prompt lacks the dirty-main rule")
	}
}
