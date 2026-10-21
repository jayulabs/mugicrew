package jinbe

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/forge"
)

const ledger = "11 file fix-a — real\n12 skip — nit\n13 fold ws-old — dup\n14 file fix-b,fix-c — two\n"

// awardedBodies are the body writes that set Awarded:.
func awardedBodies(r *jinbeSeamRec) []string {
	var out []string
	for _, b := range r.bodies {
		if strings.Contains(b, "Awarded:") {
			out = append(out, b)
		}
	}
	return out
}

func ledgered(id, notes string) bd.Bead {
	b := closedReview(id, 14, 4, 5)
	b.Notes = notes
	return b
}

func TestAwardsGoOnlyToFileAndFoldNotes(t *testing.T) {
	r := &jinbeSeamRec{}
	if err := forgeTick(t, r, 4, ledgered("ws-fr1", ledger)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"11 eyes", "13 eyes", "14 eyes"}; !reflect.DeepEqual(r.awards, want) {
		t.Errorf("awards %v, want %v (never the skip note 12)", r.awards, want)
	}
	if len(awardedBodies(r)) != 1 || !strings.Contains(awardedBodies(r)[0], "Awarded: 3\n") {
		t.Errorf("bodies %q, want Awarded: 3 set once every award succeeded", r.bodies)
	}
}

func TestAwardedStopsReawarding(t *testing.T) {
	b := ledgered("ws-fr1", ledger)
	b.Description += "Awarded: 3\n"
	r := &jinbeSeamRec{}
	if err := forgeTick(t, r, 4, b); err != nil || len(r.awards) != 0 {
		t.Errorf("err %v awards %v, want none", err, r.awards)
	}
}

func TestAwardAlreadyThereCountsAsDone(t *testing.T) {
	r := &jinbeSeamRec{awardErrs: map[int]error{11: forge.ErrNotFound}}
	if err := forgeTick(t, r, 4, ledgered("ws-fr1", ledger)); err != nil {
		t.Fatal(err)
	}
	if len(awardedBodies(r)) != 1 || !strings.Contains(awardedBodies(r)[0], "Awarded: 3\n") {
		t.Errorf("bodies %q, want Awarded set despite the 404", r.bodies)
	}
}

func TestAwardFailureRetriesWithoutAwarded(t *testing.T) {
	r := &jinbeSeamRec{awardErrs: map[int]error{13: errors.New("boom")}}
	if err := forgeTick(t, r, 4, ledgered("ws-fr1", ledger)); err != nil {
		t.Fatal(err)
	}
	if len(awardedBodies(r)) != 0 {
		t.Errorf("bodies %q, want no Awarded: after a failure", r.bodies)
	}
	r2 := &jinbeSeamRec{}
	if err := forgeTick(t, r2, 4, ledgered("ws-fr1", ledger)); err != nil || len(awardedBodies(r2)) != 1 {
		t.Errorf("retry: err %v bodies %q, want it to complete", err, r2.bodies)
	}
}

func TestAwardNotGatedByTheNotesCount(t *testing.T) {
	r := &jinbeSeamRec{}
	// count equals the closed review's Notes count: the discussions read is skipped, awards still run
	if err := forgeTick(t, r, 4, ledgered("ws-fr1", ledger)); err != nil || r.discussionsRead || len(r.awards) == 0 {
		t.Errorf("err %v read %v awards %v", err, r.discussionsRead, r.awards)
	}
}

func TestAwardRateLimitIsHeldNotRetriedPerNote(t *testing.T) {
	r := &jinbeSeamRec{awardErrs: map[int]error{11: forge.ErrRateLimited}}
	if err := forgeTick(t, r, 4, ledgered("ws-fr1", ledger)); err != nil {
		t.Errorf("err %v, want the 429 held by the Landing's arm", err)
	}
	if len(awardedBodies(r)) != 0 || len(r.awards) != 1 {
		t.Errorf("awards %v bodies %q, want the first award tried and no more", r.awards, r.bodies)
	}
}
