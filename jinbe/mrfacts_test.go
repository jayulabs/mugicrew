package jinbe

import (
	"errors"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/forge"
	"jayulabs.org/mugicrew/git"
)

const sha40 = "0123456789abcdef0123456789abcdef01234567"

func factsMR() forge.MR {
	return forge.MR{IID: 7, State: "opened", SHA: sha40, MergeStatus: "can_be_merged",
		HeadPipeline: &forge.Pipeline{ID: 42, Status: "success", Ref: "feature", SHA: sha40}}
}

// factWrites records the Watch bodies written by id; err fails every write.
func factWrites(t *testing.T, err error) *map[string][]string {
	t.Helper()
	writes := map[string][]string{}
	old := updateBody
	t.Cleanup(func() { updateBody = old })
	updateBody = func(id string, args []string) error {
		if !strings.Contains(args[1], "MR head:") && !strings.Contains(args[1], "MR pipeline:") && !strings.Contains(args[1], "MR merge status:") {
			return nil
		}
		if err != nil {
			return err
		}
		writes[id] = append(writes[id], args[1])
		return nil
	}
	return &writes
}

func factsWatchTick(t *testing.T, mr forge.MR, body string) (map[string][]string, error) {
	t.Helper()
	r := &jinbeSeamRec{mr: &mr}
	stubJinbeSeams(t, r)
	writes := factWrites(t, nil)
	w := openWatch()
	w.Description += body
	err := reconcileMRWatch(w)
	return *writes, err
}

func TestMRWatchTickRecordsTheThreeLinesOnceAndReplacesOnChange(t *testing.T) {
	writes, err := factsWatchTick(t, factsMR(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(writes["ws-watch"]) != 1 {
		t.Fatalf("writes = %q, want one", writes)
	}
	b := writes["ws-watch"][0]
	for _, want := range []string{"\nMR head: " + sha40 + "\n", "\nMR pipeline: success 42 " + sha40 + "\n", "\nMR merge status: can_be_merged\n"} {
		if !strings.Contains(b, want) {
			t.Errorf("body %q lacks %q", b, want)
		}
	}
	// a no-change tick makes no bd update
	if writes, _ := factsWatchTick(t, factsMR(), b[len("MR: !7\n"):]); len(writes) != 0 {
		t.Errorf("no-change tick wrote %q", writes)
	}
	// a change replaces one line, not appends
	mr := factsMR()
	mr.MergeStatus = "cannot_be_merged"
	writes, _ = factsWatchTick(t, mr, b[len("MR: !7\n"):])
	if got := writes["ws-watch"]; len(got) != 1 || strings.Count(got[0], "MR merge status:") != 1 || !strings.Contains(got[0], "MR merge status: cannot_be_merged") {
		t.Errorf("change wrote %q, want the line replaced", got)
	}
}

func TestMRPipelineLineIsNoneWithoutAHeadPipeline(t *testing.T) {
	mr := factsMR()
	mr.HeadPipeline = nil
	writes, _ := factsWatchTick(t, mr, "")
	if b := writes["ws-watch"]; len(b) != 1 || !strings.Contains(b[0], "\nMR pipeline: none\n") {
		t.Errorf("writes = %q, want MR pipeline: none", writes)
	}
}

func TestMRPipelineLineRecordsAStalePipelinesOldSHA(t *testing.T) {
	mr := factsMR()
	mr.HeadPipeline.SHA = "oldoldold"
	writes, _ := factsWatchTick(t, mr, "")
	if b := writes["ws-watch"]; len(b) != 1 || !strings.Contains(b[0], "MR pipeline: success 42 oldoldold\n") || !strings.Contains(b[0], "MR head: "+sha40) {
		t.Errorf("writes = %q, want the old ran-on beside the new head", writes)
	}
}

func TestMRMergeStatusRecordsTransients(t *testing.T) {
	for _, st := range []string{"unchecked", "checking", "cannot_be_merged_recheck"} {
		mr := factsMR()
		mr.MergeStatus = st
		writes, _ := factsWatchTick(t, mr, "")
		if b := writes["ws-watch"]; len(b) != 1 || !strings.Contains(b[0], "\nMR merge status: "+st+"\n") {
			t.Errorf("%s: writes = %q", st, writes)
		}
	}
}

func TestMRFactsWriteFailureDoesNotFailTheWatchTick(t *testing.T) {
	mr := factsMR()
	r := &jinbeSeamRec{mr: &mr}
	stubJinbeSeams(t, r)
	factWrites(t, errors.New("bd down"))
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Errorf("reconcile = %v, want nil", err)
	}
}

func pendingFactsTick(t *testing.T, mr forge.MR) map[string][]string {
	t.Helper()
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &mr, threadKids: []bd.Bead{openWatch()}}
	stubJinbeSeams(t, r)
	writes := factWrites(t, nil)
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\nChecked: forgesha\n"
	if err := reconcileLanding(l); err != nil {
		t.Fatal(err)
	}
	return *writes
}

func TestPendingLandingTickWritesTheFactsOntoTheWatchNotTheLanding(t *testing.T) {
	writes := pendingFactsTick(t, factsMR())
	if len(writes["ws-watch"]) != 1 || len(writes["ws-land"]) != 0 {
		t.Errorf("writes = %q, want one onto the Watch and none onto the Landing", writes)
	}
}

func TestPendingLandingTickThatClosesTheLandingWritesNoFacts(t *testing.T) {
	for name, mr := range map[string]forge.MR{
		"conflict": {IID: 7, State: "opened", HasConflicts: true, SHA: sha40, MergeStatus: "cannot_be_merged"},
		"closed":   {IID: 7, State: "closed", SHA: sha40, MergeStatus: "can_be_merged"},
		"locked":   {IID: 7, State: "locked", SHA: sha40, MergeStatus: "can_be_merged"},
	} {
		if writes := pendingFactsTick(t, mr); len(writes) != 0 {
			t.Errorf("%s: wrote %q", name, writes)
		}
	}
}

func TestPendingLandingFailureWritingFactsDoesNotFailTheTick(t *testing.T) {
	mr := factsMR()
	r := &jinbeSeamRec{head: "cafef00d", landErr: &git.LandPending{MR: 7}, mr: &mr, threadKids: []bd.Bead{openWatch()}}
	stubJinbeSeams(t, r)
	factWrites(t, errors.New("bd down"))
	l := openLanding("cafef00d")
	l.Description += "\nMR: !7\nChecked: forgesha\n"
	if err := reconcileLanding(l); err != nil {
		t.Errorf("reconcile = %v, want nil", err)
	}
}

func TestOutlivingWatchLatePassWritesNoFacts(t *testing.T) {
	closed := openCheckpoint()
	closed.Status = "closed"
	for name, c := range map[string]struct {
		cp    bd.Bead
		state string
	}{"merged": {openCheckpoint(), "merged"}, "mr closed": {openCheckpoint(), "closed"}, "container closed": {closed, "opened"}} {
		held, err := lateWatchBodies(t, &jinbeSeamRec{}, c.cp, c.state)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(held["ws-watch"], "MR head:") || strings.Contains(held["ws-watch"], "MR merge status:") {
			t.Errorf("%s: late pass wrote %q", name, held["ws-watch"])
		}
	}
}

func mergedFactsMR(ref string) forge.MR {
	mr := factsMR()
	mr.HeadPipeline = &forge.Pipeline{ID: 42, Status: "success", Ref: ref, SHA: "mergecommit"}
	return mr
}

func sourceGETs(t *testing.T, mr forge.MR, body string) (gets int, writes map[string][]string) {
	t.Helper()
	r := &jinbeSeamRec{mr: &mr, sourceSHA: sha40}
	stubJinbeSeams(t, r)
	w := factWrites(t, nil)
	o := forgeSource
	forgeSource = func(p forge.Project, s string) (string, error) { gets++; return o(p, s) }
	t.Cleanup(func() { forgeSource = o })
	wb := openWatch()
	wb.Description += body
	if err := reconcileMRWatch(wb); err != nil {
		t.Fatal(err)
	}
	return gets, *w
}

func TestMergedResultsPipelineRecordsSecondParentOncePerPipelineID(t *testing.T) {
	gets, writes := sourceGETs(t, mergedFactsMR("refs/merge-requests/7/merge"), "")
	b := writes["ws-watch"][0]
	if gets != 1 || !strings.Contains(b, "MR pipeline: success 42 "+sha40+"\n") {
		t.Fatalf("gets = %d, body %q, want one GET recording the parent", gets, b)
	}
	// same pipeline id: no second GET
	if gets, _ := sourceGETs(t, mergedFactsMR("refs/merge-requests/7/merge"), b[len("MR: !7\n"):]); gets != 0 {
		t.Errorf("same id: %d GETs, want none", gets)
	}
	// a new pipeline id looks the parent up again
	mr := mergedFactsMR("refs/merge-requests/7/merge")
	mr.HeadPipeline.ID = 43
	gets, w2 := sourceGETs(t, mr, b[len("MR: !7\n"):])
	if gets != 1 || !strings.Contains(w2["ws-watch"][0], "MR pipeline: success 43 "+sha40+"\n") {
		t.Errorf("new id: %d GETs, writes %q", gets, w2)
	}
}

func TestTrainPipelineRecordsDashAndMakesNoParentGET(t *testing.T) {
	gets, writes := sourceGETs(t, mergedFactsMR("refs/merge-requests/7/train"), "")
	if gets != 0 || !strings.Contains(writes["ws-watch"][0], "MR pipeline: success 42 -\n") {
		t.Errorf("gets = %d, writes %q", gets, writes)
	}
}

func TestFailedParentLookupWarnsAndLeavesThePipelineLine(t *testing.T) {
	mr := mergedFactsMR("refs/merge-requests/7/merge")
	r := &jinbeSeamRec{mr: &mr}
	stubJinbeSeams(t, r)
	writes := factWrites(t, nil)
	o := forgeSource
	forgeSource = func(forge.Project, string) (string, error) { return "", errors.New("404") }
	t.Cleanup(func() { forgeSource = o })
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Fatal(err)
	}
	if b := (*writes)["ws-watch"]; len(b) != 1 || strings.Contains(b[0], "MR pipeline:") {
		t.Errorf("writes = %q, want head and merge status only", *writes)
	}
}

func threadsTick(t *testing.T, mr forge.MR, discs []forge.Discussion, body string) (map[string][]string, int) {
	t.Helper()
	r := &jinbeSeamRec{mr: &mr, discussions: discs}
	stubJinbeSeams(t, r)
	writes := factWrites(t, nil)
	w := openWatch()
	w.Description += body
	if err := reconcileMRWatch(w); err != nil {
		t.Fatal(err)
	}
	return *writes, r.discussionGETs
}

func threadDiscs(open, resolved int) []forge.Discussion {
	var ds []forge.Discussion
	for i := 0; i < open; i++ {
		ds = append(ds, forge.Discussion{ID: "o", Resolvable: true})
	}
	for i := 0; i < resolved; i++ {
		ds = append(ds, forge.Discussion{ID: "r", Resolvable: true, Resolved: true})
	}
	return append(ds, forge.Discussion{ID: "i", IndividualNote: true})
}

func TestMRThreadsRecordedOnACandidateGreenTickWithOneRead(t *testing.T) {
	writes, gets := threadsTick(t, factsMR(), threadDiscs(1, 2), "")
	if gets != 1 {
		t.Errorf("discussions GETs = %d, want 1", gets)
	}
	if b := writes["ws-watch"]; len(b) != 1 || !strings.Contains(b[0], "\nMR threads: 1/3\n") {
		t.Errorf("writes = %q, want MR threads: 1/3", writes)
	}
}

func TestMRThreadsNoReadOnANonCandidateTickWithTheGateClosed(t *testing.T) {
	for name, mut := range map[string]func(*forge.MR){
		"pipeline failed": func(m *forge.MR) { m.HeadPipeline.Status = "failed" },
		"not on head":     func(m *forge.MR) { m.HeadPipeline.SHA = "old" },
		"no pipeline":     func(m *forge.MR) { m.HeadPipeline = nil },
		"unsettled":       func(m *forge.MR) { m.MergeStatus = "checking" },
	} {
		mr := factsMR()
		mr.UserNotesCount = 3
		mut(&mr)
		r := &jinbeSeamRec{mr: &mr, discussions: threadDiscs(1, 0), threadKids: []bd.Bead{closedReview("fr", 5, 3, 1)}}
		stubJinbeSeams(t, r)
		factWrites(t, nil)
		if err := reconcileMRWatch(openWatch()); err != nil || r.discussionGETs != 0 {
			t.Errorf("%s: err %v, discussions GETs = %d, want none", name, err, r.discussionGETs)
		}
	}
}

func TestMRThreadsGateOpenTickReadsOnceSharedWithTheArms(t *testing.T) {
	mr := factsMR()
	mr.MergeStatus = "checking" // not candidate: only the gate opens the read
	mr.UserNotesCount = 4
	r := &jinbeSeamRec{mr: &mr, discussions: threadDiscs(2, 0), threadKids: []bd.Bead{closedReview("fr", 5, 3, 1)}}
	stubJinbeSeams(t, r)
	writes := factWrites(t, nil)
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Fatal(err)
	}
	if r.discussionGETs != 1 {
		t.Errorf("discussions GETs = %d, want 1", r.discussionGETs)
	}
	if b := (*writes)["ws-watch"]; len(b) != 1 || !strings.Contains(b[0], "MR threads: 2/2") {
		t.Errorf("writes = %q, want MR threads: 2/2", *writes)
	}
}

func TestMRThreadsFailedWriteWarnsAndDoesNotFailTheTick(t *testing.T) {
	mr := factsMR()
	r := &jinbeSeamRec{mr: &mr, discussions: threadDiscs(1, 0)}
	stubJinbeSeams(t, r)
	factWrites(t, errors.New("bd down"))
	if err := reconcileMRWatch(openWatch()); err != nil {
		t.Errorf("err = %v, want the failed write to only warn", err)
	}
}

func TestMRThreadsReplacedOnResolveAndUnresolve(t *testing.T) {
	writes, _ := threadsTick(t, factsMR(), threadDiscs(1, 0), "")
	b := writes["ws-watch"][0]
	rest := b[len("MR: !7\n"):]
	writes, gets := threadsTick(t, factsMR(), threadDiscs(0, 1), rest) // resolved
	if got := writes["ws-watch"]; gets != 1 || len(got) != 1 || strings.Count(got[0], "MR threads:") != 1 || !strings.Contains(got[0], "MR threads: 0/1") {
		t.Errorf("resolve wrote %q (GETs %d)", got, gets)
	}
	rest = strings.Replace(rest, "MR threads: 1/1", "MR threads: 0/1", 1)
	writes, _ = threadsTick(t, factsMR(), threadDiscs(1, 0), rest) // unresolved
	if got := writes["ws-watch"]; len(got) != 1 || !strings.Contains(got[0], "MR threads: 1/1") {
		t.Errorf("unresolve wrote %q", got)
	}
	// unchanged: no write
	rest = strings.Replace(rest, "MR threads: 0/1", "MR threads: 1/1", 1)
	if writes, _ = threadsTick(t, factsMR(), threadDiscs(1, 0), rest); len(writes) != 0 {
		t.Errorf("no-change tick wrote %q", writes)
	}
}
