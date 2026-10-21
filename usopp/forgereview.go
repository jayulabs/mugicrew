package usopp

import (
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"jayulabs.org/mugicrew/bd"
	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/eventlog"
	"jayulabs.org/mugicrew/git"
)

// forgeReviewPrompt (forge-review.md) is the Forge Review judge: the rubric and
// merging rules of ADR 0317 and ADR 0318, over the frozen snapshot Jinbe wrote
// into the bead and the live list of beads already filed from the MR. Pure.
//
//go:embed prompts/forge-review.md
var forgeReviewSrc string

var forgeReviewTmpl = template.Must(template.New("forge-review").Parse(forgeReviewSrc))

// forgeArgs names forge-review.md's slots.
type forgeArgs struct {
	ReviewID    string
	ContainerID string
	FiledKind   contract.Kind // critique on a Checkpoint, grilling on a Chart
	Branch      string        // the container's branch
	Snapshot    string        // the Forge Review body: key lines, then the tagged snapshot
	Filed       []filedBead
}

// filedBead is a container member already filed from this MR: status, close
// reason and the note ids its Source: lines carry.
type filedBead struct {
	ID, Status, CloseReason string
	Notes                   []string
}

// Draft is the shared Draft block.
func (a forgeArgs) Draft() string { return contract.DraftInstruction(a.ReviewID) }

func forgeReviewPrompt(a forgeArgs) string { return contract.Render(forgeReviewTmpl, a) }

// Seams over the graph legs the Forge Review tail drives.
var (
	childrenOfKind = bd.ChildrenOfKind
	draftsOf       = bd.Drafts
	updateBead     = bd.Update
	landingsOf     = bd.LandingsOf
)

var (
	judgeNote = regexp.MustCompile(`<note\s[^>]*\bid="([^"]+)"[^>]*\bstatus="JUDGE"`)
)

// judgeIDs is the id of every JUDGE-tagged note in the snapshot, in order.
func judgeIDs(body string) []string {
	var ids []string
	for _, m := range judgeNote.FindAllStringSubmatch(body, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// ledgerOf reads the per-note ledger lines off the bead's notes: note id → verdict.
func ledgerOf(notes string) map[string]string {
	l := map[string]string{}
	for _, line := range strings.Split(notes, "\n") {
		if note, verdict, ok := contract.LedgerLine(line); ok {
			l[note] = verdict
		}
	}
	return l
}

// filedFrom lists the container's beads of kind carrying a "Source: !<iid> …" line
// for this MR, with the note ids those lines name.
func filedFrom(containerID string, kind contract.Kind, iid int) ([]filedBead, error) {
	kids, err := childrenOfKind(containerID, kind)
	if err != nil {
		return nil, err
	}
	var out []filedBead
	for _, k := range kids {
		f := filedBead{ID: k.ID, Status: k.Status, CloseReason: k.CloseReason}
		for _, src := range contract.SourcesOf(k.Description + "\n" + k.Notes) {
			if src.IID == iid {
				f.Notes = append(f.Notes, src.Note)
			}
		}
		if len(f.Notes) > 0 {
			out = append(out, f)
		}
	}
	return out, nil
}

// runForgeReview is the Forge Review pass: render the frozen snapshot with the live
// filed beads, run the session, then drive the tail without parsing prose — the
// Drafts and the ledger only (ADR 0318). A gap in the ledger fails the pass, so the
// loop deletes the Drafts and the bead is retried with its frozen body.
func runForgeReview(ctx contract.Ctx, bead bd.Bead) error {
	if !bead.Open() {
		return nil
	}
	if bead.Parent == "" {
		return fmt.Errorf("usopp: forge-review bead %s has no parent container membership edge", bead.ID)
	}
	iid := git.RecordedMR(bead.Description)
	mark, ok := git.ParseLine(bead.Description, "Read through note:")
	if iid == 0 || !ok {
		return fmt.Errorf("usopp: forge-review bead %s body lacks its MR: or Read through note: line", bead.ID)
	}
	judge := judgeIDs(bead.Description)
	container, err := showBead(bead.Parent)
	if err != nil {
		return err
	}
	kind := contract.FiledKind(container.Kind)
	filed, err := filedFrom(bead.Parent, kind, iid)
	if err != nil {
		return err
	}
	a := forgeArgs{ReviewID: bead.ID, ContainerID: bead.Parent, FiledKind: kind, Snapshot: bead.Description, Filed: filed,
		Branch: git.BranchName(string(container.Kind), bead.Parent)}
	inv, err := contract.NewInvocation(bead.ID, forgeReviewPrompt(a))
	if err != nil {
		return err
	}
	if _, err := ctx.Harness().Run(ctx, inv); err != nil {
		return sweptOr(bead.ID, err)
	}
	if bead, err = showBead(bead.ID); err != nil || !bead.Open() {
		return err
	}
	return forgeReviewTail(ctx, bead, container, judge, mark)
}

// forgeReviewTail checks the ledger against the JUDGE notes and the Drafts, then
// publishes: Draft priorities to the container's, the pending Landing obsolete when
// anything was filed, and the close reason that counts the pass.
func forgeReviewTail(ctx contract.Ctx, bead, container bd.Bead, judge []string, mark string) error {
	ledger := ledgerOf(bead.Notes)
	var missing []string
	counts := map[string]int{}
	for _, id := range judge {
		v, ok := ledger[id]
		if !ok {
			missing = append(missing, id)
		}
		counts[v]++
	}
	if len(missing) > 0 {
		return fmt.Errorf("usopp: forge-review %s ledger misses JUDGE note(s) %s", bead.ID, strings.Join(missing, ", "))
	}
	drafts, err := draftsOf(bead.ID)
	if err != nil {
		return err
	}
	if counts["file"] > 0 && len(drafts) == 0 {
		return fmt.Errorf("usopp: forge-review %s ledger files notes but filed no Draft", bead.ID)
	}
	for _, d := range drafts {
		if err := updateBead(d.ID, []string{"--priority", strconv.Itoa(container.Priority)}); err != nil {
			return err
		}
	}
	if len(drafts) > 0 {
		landings, err := landingsOf(bead.Parent)
		if err != nil {
			return err
		}
		for _, l := range landings {
			if l.Open() {
				if err := closeBead(l.ID, "obsolete: review comment"); err != nil {
					return err
				}
			}
		}
	}
	reason := fmt.Sprintf("%s: read through note %s", contract.ForgeReviewCounts{Filed: len(drafts), Folded: counts["fold"], Skipped: counts["skip"]}, mark)
	event := "forge_review.filed"
	if len(drafts) == 0 && counts["fold"] == 0 {
		reason = fmt.Sprintf("nothing actionable: read %d notes through %s", len(judge), mark)
		event = "forge_review.empty"
	}
	slog.Info(event, append(eventlog.BeadAttrs(bead), "filed", len(drafts), "folded", counts["fold"], "skipped", counts["skip"], "through", mark)...)
	return closeOnPublish(ctx, bead.ID, reason, nil)
}
