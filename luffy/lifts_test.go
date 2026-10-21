package luffy

import (
	"strings"
	"testing"
)

// The dirty-main rule sits in the reconcile fast lane.
func TestTriagePromptDirtyMainIsHumans(t *testing.T) {
	got := strings.Join(strings.Fields(triagePrompt("ws-sig", "", "", nil, nil)), " ")
	i := strings.Index(got, "### FAST LANE")
	if i < 0 {
		t.Fatal("triage prompt lacks the FAST LANE section")
	}
	sec := got[i+len("### FAST LANE"):]
	if j := strings.Index(sec, "###"); j >= 0 {
		sec = sec[:j]
	}
	if !strings.Contains(sec, "is the human's to clean") {
		t.Error("FAST LANE section lacks the dirty-main rule")
	}
}
