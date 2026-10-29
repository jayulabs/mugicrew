package bd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/contract"
	"jayulabs.org/mugicrew/git"
)

// EditChartBody is `mugicrew chart append|drop` (Spec D9, ADR 0103 §4):
// under the land lock it re-reads the Chart's description, appends line to (or
// drops it from) the named "## <section>" of the Chart body, and writes it back,
// so concurrent defog sessions never lose each other's edit. A human's TUI edit
// takes no lock and stays last-write-wins. An unknown section or a line to drop
// that isn't there is a loud error with nothing written; appending a line the
// section already holds writes nothing, so a re-run session can't double it.
func EditChartBody(chartID, section, line string, drop bool) error {
	if !slices.Contains(strings.Split(contract.WayfinderMapFields, " / "), section) {
		return fmt.Errorf("chart: unknown section %q (one of %s)", section, contract.WayfinderMapFields)
	}
	if strings.TrimSpace(line) == "" || strings.Contains(line, "\n") {
		return fmt.Errorf("chart: line must be one non-empty line, got %q", line)
	}
	return git.WithLandLock(func() error {
		b, err := Show(chartID)
		if err != nil {
			return err
		}
		body, changed, err := editSection(b.Description, section, line, drop)
		if err != nil || !changed {
			return err
		}
		_, err = run("update", chartID, "--description", body)
		return err
	})
}

// editSection edits one line of the "## <section>" block of a Chart body; every
// byte outside that block is untouched. An append lands after the block's last
// non-blank line (after a blank under the heading when it is empty).
func editSection(body, section, line string, drop bool) (string, bool, error) {
	lines := strings.Split(body, "\n")
	secs := contract.ChartSections(lines)
	i := slices.IndexFunc(secs, func(s contract.Section) bool { return s.Name == section })
	if i < 0 {
		return "", false, fmt.Errorf("chart: body has no ## %s section", section)
	}
	head, end := secs[i].Head, secs[i].End
	at := slices.Index(lines[head+1:end], line)
	if drop {
		if at < 0 {
			return "", false, fmt.Errorf("chart: ## %s has no line %q to drop", section, line)
		}
		at += head + 1
		cut := at + 1
		if lines[at-1] == "" && cut < len(lines) && lines[cut] == "" {
			cut++ // don't leave a double blank where the line was
		}
		return strings.Join(slices.Delete(lines, at, cut), "\n"), true, nil
	}
	if at >= 0 {
		return body, false, nil
	}
	last := end - 1
	for last > head && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last > head {
		return strings.Join(slices.Insert(lines, last+1, line), "\n"), true, nil
	}
	if head+1 == len(lines) || lines[head+1] != "" {
		lines = slices.Insert(lines, head+1, "")
	}
	return strings.Join(slices.Insert(lines, head+2, line, ""), "\n"), true, nil
}

// FinishChart is `mugicrew chart finish <chart> --survey <survey>` (ADR 0357): the
// guarded stamp of a Chart whose Survey found no fog. Under the land lock it
// re-reads the beads and refuses, loudly and writing nothing, unless the Survey is
// the Chart's own claimed in_progress kind:survey child with non-empty notes, the
// Chart is open with no other unclosed child and an empty Not-yet-specified, and
// some Decisions-so-far line names the Survey. The guards run first, so a Chart
// already stamped chart:defogged then succeeds without a write and a relaunched
// session can re-check. It never writes notes or decisions, and records no assent.
func FinishChart(chartID, surveyID string) error {
	return git.WithLandLock(func() error {
		chart, err := Show(chartID)
		if err != nil {
			return err
		}
		survey, err := Show(surveyID)
		if err != nil {
			return err
		}
		switch {
		case survey.Kind != contract.KindSurvey:
			return fmt.Errorf("chart finish: %s is not a survey", surveyID)
		case survey.Parent != chartID:
			return fmt.Errorf("chart finish: survey %s is not a child of %s", surveyID, chartID)
		case survey.Status != "in_progress" || survey.Assignee == "":
			return fmt.Errorf("chart finish: survey %s is not open and claimed", surveyID)
		case chart.Kind != contract.KindChart || !chart.Open():
			return fmt.Errorf("chart finish: %s is not an open chart", chartID)
		case strings.TrimSpace(survey.Notes) == "":
			return fmt.Errorf("chart finish: survey %s has no notes; write the settled decisions with bd update --notes first", surveyID)
		}
		kids, err := ListChildren(chartID)
		if err != nil {
			return err
		}
		var others []Bead
		for _, k := range kids {
			if k.ID != surveyID {
				others = append(others, k)
			}
		}
		if open, _ := DrainBlockersOver(others, nil); open > 0 || slices.ContainsFunc(others, func(k Bead) bool { return k.Status == "blocked" }) {
			return fmt.Errorf("chart finish: %s still has unclosed children besides the survey", chartID)
		}
		lines := strings.Split(chart.Description, "\n")
		secs := contract.ChartSections(lines)
		section := func(name string) (string, bool) {
			i := slices.IndexFunc(secs, func(s contract.Section) bool { return s.Name == name })
			if i < 0 {
				return "", false
			}
			return strings.Join(lines[secs[i].Head+1:secs[i].End], "\n"), true
		}
		if fog, ok := section("Not-yet-specified"); !ok {
			return fmt.Errorf("chart finish: body has no ## Not-yet-specified section")
		} else if strings.TrimSpace(fog) != "" {
			return fmt.Errorf("chart finish: ## Not-yet-specified is not empty")
		}
		if decided, _ := section("Decisions-so-far"); !namesID(decided, surveyID) {
			return fmt.Errorf("chart finish: no ## Decisions-so-far line names survey %s; mugicrew chart append one first", surveyID)
		}
		if chart.HasLabel(contract.LabelDefogged) {
			return nil
		}
		_, err = run("update", chartID, "--add-label", contract.LabelDefogged)
		return err
	})
}

// namesID reports whether text names id as a whole token, so a line naming mc-s1
// does not name mc-s.
func namesID(text, id string) bool {
	return regexp.MustCompile(`(?:^|[^A-Za-z0-9._-])` + regexp.QuoteMeta(id) + `(?:$|[^A-Za-z0-9_.-]|\.(?:[^A-Za-z0-9]|$))`).MatchString(text)
}
