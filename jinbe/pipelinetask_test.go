package jinbe

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
)

// pipelineTaskBody files the pipeline-failure Task for one arm and returns its body.
func pipelineTaskBody(t *testing.T, name, path string, kind contract.Kind, reason, plRef string) string {
	t.Helper()
	pl := forge.Pipeline{ID: 42, Status: "failed", WebURL: "https://gl/p/42", Ref: plRef}
	r := &jinbeSeamRec{head: "cafef00d", jobs: []forge.Job{{ID: 5, Name: "unit", WebURL: "https://gl/j/5", FailureReason: reason}}}
	stubJinbeSeams(t, r)
	var body string
	bd.SpawnBlockingTask = func(_, _, _, b string) (string, bool, error) { body = b; return "t", true, nil }
	var err error
	if name == "revert" || name == "revert-infra" {
		rb := bd.Bead{ID: "ws-rb", Title: "Ship", Description: "Retried: pipeline 42\nMR link: https://gl/mr/7\n"}
		err = revertPipelineFailed(&rb, rbLeg{mrPath: path}, 7, pl)
	} else {
		l := openLanding("cafef00d")
		l.Description += "\nMR: !7\nMR link: https://gl/mr/7\nRetried: pipeline 42\n"
		_, err = pipelineFailed(l, bd.Bead{ID: "ws-cp", Kind: kind, Title: "Ship"}, legs{mrPath: path}, pl)
	}
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func wantInOrder(t *testing.T, body string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(body[at:], p)
		if i < 0 {
			t.Fatalf("body lacks %q after offset %d:\n%s", p, at, body)
		}
		at += i + len(p)
	}
}

func TestPipelineTaskBodies(t *testing.T) {
	facts := []string{"Merge request: !7\n", "MR link: https://gl/mr/7\n", "Pipeline: 42\n", "Pipeline URL: https://gl/p/42\n\n## Steps\n"}
	diag := []string{"## Diagnostics\n", "Job: unit\n", "Job URL: https://gl/j/5\n", "failure_reason: "}
	t.Run("landing infra", func(t *testing.T) {
		body := pipelineTaskBody(t, "infra", "", contract.KindCheckpoint, "runner_system_failure", "refs/merge-requests/7/head")
		if !strings.HasPrefix(body, infraTaskLead) {
			t.Errorf("lead must open with infraTaskLead:\n%s", body)
		}
		wantInOrder(t, body, append(append(facts, "Retry the pipeline at the Pipeline URL", "Check: `mugicrew probe pipeline '!7' 42` prints `success`, or `mugicrew probe landed ws-cp` prints `landed`."), diag...)...)
	})
	t.Run("chart real, branch pipeline", func(t *testing.T) {
		body := pipelineTaskBody(t, "chart", "", contract.KindChart, "script_failure", "chart/x")
		wantInOrder(t, body, append(append(facts, "1. ", "Signal", "Check: `mugicrew probe pipeline '!7' --main` prints `success`.", "2. ", "Ask: is the Chart rebased onto a green main?"), diag...)...)
	})
	t.Run("chart real, merge pipeline has no rebase step", func(t *testing.T) {
		body := pipelineTaskBody(t, "chart", "", contract.KindChart, "script_failure", "refs/merge-requests/7/merge")
		if strings.Contains(body, "2. ") || strings.Contains(body, "rebased") {
			t.Errorf("a /merge ref leaves step 2 out:\n%s", body)
		}
	})
	t.Run("revert real", func(t *testing.T) {
		body := pipelineTaskBody(t, "revert", "grp/proj", contract.KindCheckpoint, "script_failure", "")
		wantInOrder(t, body, "Merge request: grp/proj!7\n", "MR link: https://gl/mr/7\n", "Pipeline: 42\n", "Pipeline URL: https://gl/p/42\n\n## Steps\n",
			"[irreversible]", "at the MR link", "Check: `mugicrew probe mr 'grp/proj!7'` prints `merged` or `closed`.", "## Diagnostics\n")
	})
	t.Run("revert infra", func(t *testing.T) {
		body := pipelineTaskBody(t, "revert-infra", "", contract.KindCheckpoint, "runner_system_failure", "")
		if !strings.HasPrefix(body, infraTaskLead) || strings.Count(body, "\n1. ")+strings.Count(body, "\n2. ") != 1 {
			t.Errorf("want infraTaskLead and exactly one step:\n%s", body)
		}
		wantInOrder(t, body, append(facts, "probe pipeline '!7' 42", "probe mr '!7'` prints `merged` or `closed`")...)
	})
}
