package bd

import (
	"fmt"
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
