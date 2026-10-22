package jinbe

import (
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

// A nested hold names the MR by its project in title and body while the idempotency key
// stays "Merged MR: !N"; a Workspace one is unchanged.
func TestHoldTrailerlessNamesNestedMR(t *testing.T) {
	o := bd.SpawnBlockingTask
	t.Cleanup(func() { bd.SpawnBlockingTask = o })
	defer git.SetGraphID("g1")()
	oClash := postLandClashes
	t.Cleanup(func() { postLandClashes = oClash })
	postLandClashes = func(string, string) ([]git.PostLandClash, error) { return nil, nil }
	var key, title, body string
	bd.SpawnBlockingTask = func(_, k, ti, b string) (string, bool, error) { key, title, body = k, ti, b; return "t", true, nil }
	holdTrailerless(bd.Bead{ID: "cp", Title: "C"}, "", mrNaming{noun: "merge request", label: "MR"}, "Spec: x", 7, "abc")
	if wantTitle := "C: merge request !7 merged without its trailer"; title != wantTitle ||
		!strings.HasPrefix(body, "MR !7 for cp was merged at abc, but no \"Spec: x\" trailer reached mainline.\n") {
		t.Errorf("workspace hold reworded: title %q body %q", title, body)
	}
	for path, want := range map[string]string{"grp/proj": "grp/proj!7", "": " !7"} {
		holdTrailerless(bd.Bead{ID: "cp", Title: "C"}, "", mrNaming{path: path, noun: "merge request", label: "MR"}, "Spec: x", 7, "abc")
		if key != "Merged MR: !7\n" || !strings.Contains(title, want) || !strings.Contains(body, want) {
			t.Errorf("path %q: key %q title %q body %q", path, key, title, body)
		}
	}
}

// Every pipeline-failure Task and Critique names a nested MR by project in title and
// body; a Workspace one reads "pipeline N failed" with no MR line (Spec FR 28).
func TestPipelineFailureNamesNestedMR(t *testing.T) {
	pl := forge.Pipeline{ID: 42, Status: "failed", WebURL: "https://gl/p/42"}
	for _, path := range []string{"grp/proj", ""} {
		want := "grp/proj!7"
		if path == "" {
			want = ""
		}
		for _, c := range []struct {
			name string
			kind contract.Kind
			jobs []forge.Job
		}{
			{"infra", contract.KindCheckpoint, []forge.Job{{ID: 5, FailureReason: "runner_system_failure"}}},
			{"chart", contract.KindChart, []forge.Job{{ID: 6, FailureReason: "script_failure"}}},
			{"critique", contract.KindCheckpoint, []forge.Job{{ID: 6, FailureReason: "script_failure"}}},
			{"revert", contract.KindCheckpoint, []forge.Job{{ID: 6, FailureReason: "script_failure"}}},
		} {
			t.Run(c.name+"/"+path, func(t *testing.T) {
				r := &jinbeSeamRec{head: "cafef00d", jobs: c.jobs, targetSHA: "base9"}
				stubJinbeSeams(t, r)
				var title, body string
				bd.SpawnBlockingTask = func(_, _, ti, b string) (string, bool, error) { title, body = ti, b; return "t", true, nil }
				l := openLanding("cafef00d")
				l.Description += "\nMR: !7\nRetried: pipeline 42\n"
				cp := bd.Bead{ID: "ws-cp", Kind: c.kind, Title: "Ship"}
				var err error
				if c.name == "revert" {
					rb := bd.Bead{ID: "ws-rb", Title: "Ship", Description: "Retried: pipeline 42\n"}
					err = revertPipelineFailed(&rb, rbLeg{mrPath: path}, 7, pl)
				} else {
					_, err = pipelineFailed(l, cp, legs{mrPath: path}, pl)
				}
				if err != nil {
					t.Fatal(err)
				}
				if c.name == "critique" {
					s := r.spawned[0]
					title, body = s.Title, s.Description
				}
				if c.name != "critique" {
					if !strings.Contains(body, "Merge request: "+path+"!7\n") {
						t.Errorf("Task body %q lacks its Merge request: fact", body)
					}
				} else if want == "" {
					if strings.Contains(title, " on merge request") || strings.Contains(body, "Merge Request:") {
						t.Errorf("Workspace title %q body %q must not name an MR", title, body)
					}
				} else if !strings.Contains(title, " on merge request "+want) || !strings.Contains(body, "Merge Request: "+want+"\n") {
					t.Errorf("title %q body %q lack %q", title, body, want)
				}
			})
		}
	}
}

// A Workspace's pipeline-failure Critique body is byte-identical to the pre-Task-body
// string for the same pipeline and two failing jobs (Spec Testing Decisions).
func TestPipelineFailureCritiqueBodyGolden(t *testing.T) {
	o := forgeTrace
	t.Cleanup(func() { forgeTrace = o })
	forgeTrace = func(_ forge.Project, job int) (string, error) { return map[int]string{1: "boom one", 2: "boom two"}[job], nil }
	pl := forge.Pipeline{ID: 42, WebURL: "https://gl/p/42"}
	jobs := []forge.Job{
		{ID: 1, Name: "build", WebURL: "https://gl/j/1", FailureReason: "script_failure"},
		{ID: 2, Name: "test", WebURL: "https://gl/j/2", FailureReason: "runner_system_failure"},
	}
	facts, tails, err := pipelineParts(forge.Project{}, pl, jobs)
	if err != nil {
		t.Fatal(err)
	}
	got := mrLead("", 7) + joinPipelineParts(facts, tails)
	want := "Pipeline: 42\nPipeline URL: https://gl/p/42\n\n" +
		"Job: build\nJob URL: https://gl/j/1\nfailure_reason: script_failure\n```\nboom one\n```\n\n" +
		"Job: test\nJob URL: https://gl/j/2\nfailure_reason: runner_system_failure\n```\nboom two\n```\n"
	if got != want {
		t.Errorf("Critique details = %q; want %q", got, want)
	}
}
