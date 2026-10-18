package bd

import (
	"slices"
	"testing"

	"jayulabs.org/mugicrew/contract"
)

func labelsValue(t *testing.T, args []string) string {
	t.Helper()
	i := slices.Index(args, "--labels")
	if i < 0 {
		t.Fatalf("no --labels in %v", args)
	}
	return args[i+1]
}

func TestLabelArgsCarryContextOnlyWhenSet(t *testing.T) {
	base := Successor{Kind: contract.KindSpec, Title: "t"}
	if got := labelsValue(t, labelArgs(base)); got != "kind:spec" {
		t.Errorf("plain labels = %q", got)
	}
	base.Context = "mugicrew"
	if got := labelsValue(t, labelArgs(base)); got != "kind:spec,context:mugicrew" {
		t.Errorf("context labels = %q", got)
	}
	base.DraftedBy = "mc-1"
	if got := labelsValue(t, labelArgs(base)); got != "kind:spec,context:mugicrew,drafted-by:mc-1" {
		t.Errorf("draft context labels = %q", got)
	}
}

func TestContextOf(t *testing.T) {
	if got := ContextOf(Bead{Labels: []string{"kind:chart", "context:apps-x"}}); got != "apps-x" {
		t.Errorf("ContextOf = %q", got)
	}
	if got := ContextOf(Bead{Labels: []string{"kind:chart"}}); got != "" {
		t.Errorf("unlabelled ContextOf = %q", got)
	}
}
