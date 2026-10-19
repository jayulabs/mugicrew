package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"jayulabs.org/mugicrew/cfglayer"
	"jayulabs.org/mugicrew/git"
)

const initWorkspaceUsage = "usage: mugicrew init workspace [--land-strategy local|gitlab] [--require-initiatives <bool>] [--external-ref <bool>] [--jira-site <host>] [--context name=[repo:|path:]dir]..."

// runInitWorkspace is `mugicrew init workspace`: write the shared settings the
// Workspace owns into .mugicrew/config.toml of the current linked worktree,
// through cfglayer's splicer. Like personal init it bootstraps state, so it needs
// no bd store; it runs no git write and never commits. It returns the exit code.
func runInitWorkspace(args []string, in io.Reader, w io.Writer) int {
	fs := flag.NewFlagSet("init workspace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	land := fs.String("land-strategy", "", "local or gitlab")
	req := fs.String("require-initiatives", "", "true or false")
	ext := fs.String("external-ref", "", "true or false")
	site := fs.String("jira-site", "", "bare Jira host; empty clears it")
	var specs []string
	fs.Func("context", "name=dir, name=repo:dir or name=path:dir; repeatable, the set replaces the declared one", func(v string) error {
		specs = append(specs, v)
		return nil
	})
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(w, initWorkspaceUsage)
		return 1
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })

	fail := func(format string, a ...any) int {
		fmt.Fprintf(w, "mugicrew init workspace: "+format+"\n", a...)
		return 1
	}
	root, err := git.WorktreeTop()
	if err != nil {
		return fail("%v", err)
	}
	if git.OnMainline() {
		return fail("this is the mainline worktree; init workspace writes a file, so run it in a linked worktree: git worktree add ../<dir> -b <branch> main, then re-run there")
	}

	var cur cfglayer.OwnedKeys
	if cfglayer.Decode(cfglayer.SharedPath(root), &cur) == cfglayer.Unusable {
		return fail("%s does not decode; fix it by hand first", cfglayer.SharedPath(root))
	}

	// Each answer: its flag, else (at a terminal) the prompt with the file's
	// current value or the built-in default, else the current value, else a
	// refusal. A flag's bad value refuses; a prompt's bad answer re-asks.
	tty := initIsTTY()
	setUp, _ := cfglayer.WorkspaceSetUp(root)
	pr := &wsPrompter{in: bufio.NewReader(in), w: w}
	var landV, siteV string
	var reqV, extV bool
	switch {
	case given["land-strategy"]:
		landV = *land
		if rule := checkLandStrategy(landV); rule != "" {
			return fail("--land-strategy %q: %s", landV, rule)
		}
	case tty:
		def := git.LandStrategyLocal
		if cur.LandStrategy != nil {
			def = *cur.LandStrategy
		}
		v, ok := pr.ask("land strategy ("+git.LandStrategyLocal+"|"+git.LandStrategyGitlab+")", def, checkLandStrategy)
		if !ok {
			return fail("input ended before a valid land strategy")
		}
		landV = v
	case cur.LandStrategy != nil:
		landV = *cur.LandStrategy
		if rule := checkLandStrategy(landV); rule != "" {
			return fail("current land_strategy %q: %s", landV, rule)
		}
	default:
		return fail("no flag and no current value; pass --land-strategy <local|gitlab>")
	}
	switch {
	case given["require-initiatives"]:
		var rule string
		if reqV, rule = parseBool(*req); rule != "" {
			return fail("--require-initiatives %q: %s", *req, rule)
		}
	case tty:
		def := false
		if cur.Initiatives.Required != nil {
			def = *cur.Initiatives.Required
		}
		v, ok := pr.askBool("Require Initiatives", def, func(b bool) string {
			if given["external-ref"] {
				if e, _ := parseBool(*ext); e {
					return checkExternalRef(b, true) // a given --external-ref true needs required
				}
			}
			return ""
		})
		if !ok {
			return fail("input ended before a valid answer for Require Initiatives")
		}
		reqV = v
	case cur.Initiatives.Required != nil:
		reqV = *cur.Initiatives.Required
	default:
		return fail("no flag and no current value; pass --require-initiatives <true|false>")
	}
	switch {
	case given["external-ref"]:
		var rule string
		if extV, rule = parseBool(*ext); rule != "" {
			return fail("--external-ref %q: %s", *ext, rule)
		}
	case !reqV: // not asked; a dropped requirement drops the enforcement
	case tty:
		def := false
		if cur.Initiatives.ExternalRef != nil {
			def = *cur.Initiatives.ExternalRef
		}
		v, ok := pr.askBool("Enforce external ID", def, nil)
		if !ok {
			return fail("input ended before a valid answer for Enforce external ID")
		}
		extV = v
	case cur.Initiatives.ExternalRef != nil:
		extV = *cur.Initiatives.ExternalRef
	default:
		return fail("no flag and no current value; pass --external-ref <true|false>")
	}
	if rule := checkExternalRef(reqV, extV); rule != "" {
		return fail("--external-ref: %s", rule)
	}
	switch {
	case given["jira-site"]:
		siteV = *site
		if rule := checkJiraSite(siteV); rule != "" {
			return fail("--jira-site %q: %s", siteV, rule)
		}
	case tty:
		def := ""
		if cur.Jira.Site != nil {
			def = *cur.Jira.Site
		}
		label := "Jira site (blank for no Jira)"
		if def != "" {
			label = "Jira site (" + wsClearWord + " for no Jira)"
		}
		v, ok := pr.ask(label, def, func(v string) string {
			if v == wsClearWord {
				return ""
			}
			return checkJiraSite(v)
		})
		if !ok {
			return fail("input ended before a valid Jira site")
		}
		if v != wsClearWord {
			siteV = v
		}
	case cur.Jira.Site != nil:
		siteV = *cur.Jira.Site
		if rule := checkJiraSite(siteV); rule != "" {
			return fail("current jira site %q: %s", siteV, rule)
		}
	case setUp: // a set-up Workspace with no [jira] site has answered "no Jira"
	default:
		return fail("no flag and no current value; pass --jira-site <host> (empty for no Jira)")
	}

	var ctxEdits []cfglayer.Edit
	if given["context"] {
		var want []declaredContext
		for _, spec := range specs {
			c, rule := parseContextSpec(spec)
			if rule == "" {
				rule = checkContext(root, c)
			}
			if rule != "" {
				return fail("--context %q: %s", spec, rule)
			}
			want = append(want, c)
		}
		if rule := checkContextSet(want); rule != "" {
			return fail("--context: %s", rule)
		}
		ctxEdits = contextEdits(cur.Context, want)
	} else if tty {
		want, ok := promptContexts(root, cur.Context, pr)
		if !ok {
			return fail("input ended before a valid Context answer")
		}
		ctxEdits = contextEdits(cur.Context, want)
	}
	if local := localContexts(root); local {
		fmt.Fprintf(w, "note: the local layer config.local.toml (%s) declares [context.*], which hides the shared Contexts on this machine; the shared file is still written\n", cfglayer.LocalPath(root))
	}

	edits := []cfglayer.Edit{
		{Op: cfglayer.SetKey, Key: "land_strategy", Value: landV},
		{Op: cfglayer.SetKey, Table: []string{"initiatives"}, Key: "required", Value: reqV},
		{Op: cfglayer.SetKey, Table: []string{"initiatives"}, Key: "external_ref", Value: extV},
	}
	if siteV != "" {
		edits = append(edits, cfglayer.Edit{Op: cfglayer.SetKey, Table: []string{"jira"}, Key: "site", Value: siteV})
	} else {
		edits = append(edits, cfglayer.Edit{Op: cfglayer.RemoveKey, Table: []string{"jira"}, Key: "site"})
	}
	edits = append(edits, ctxEdits...)
	if err := cfglayer.WriteOwned(cfglayer.SharedPath(root), edits); err != nil {
		if errors.Is(err, cfglayer.ErrEditByHand) {
			return fail("%v; edit %s by hand", err, cfglayer.SharedPath(root))
		}
		return fail("%v", err)
	}

	fmt.Fprintf(w, "wrote the shared settings to %s\n", cfglayer.SharedPath(root))
	fmt.Fprintln(w, "to land it:")
	fmt.Fprintln(w, "  1. commit .mugicrew/config.toml on this branch (init made no commit)")
	fmt.Fprintln(w, "  2. merge it through the Workspace's usual MR or land flow")
	fmt.Fprintln(w, "  3. tell the other humans to pull")
	fmt.Fprintln(w, "run mugicrew doctor next")
	return 0
}

// wsClearWord clears a defaulted Jira site at the prompt, where blank keeps it.
const wsClearWord = "none"

// wsPrompter asks Workspace init's questions on one shared line reader.
type wsPrompter struct {
	in *bufio.Reader
	w  io.Writer
}

// ask prints "label [def]: ", reads a line and returns it (def on a blank),
// re-asking with the broken rule until check passes. ok is false when the input
// ends while the answer is still unusable.
func (p *wsPrompter) ask(label, def string, check func(string) string) (string, bool) {
	for {
		if def != "" {
			fmt.Fprintf(p.w, "%s [%s]: ", label, def)
		} else {
			fmt.Fprintf(p.w, "%s: ", label)
		}
		line, err := p.in.ReadString('\n')
		v := strings.TrimSpace(line)
		if v == "" {
			v = def
		}
		rule := check(v)
		if rule == "" {
			return v, true
		}
		if err != nil {
			return "", false
		}
		fmt.Fprintln(p.w, rule)
	}
}

// askBool is ask over a yes/no answer; extra vets the parsed value.
func (p *wsPrompter) askBool(label string, def bool, extra func(bool) string) (bool, bool) {
	defS := "no"
	if def {
		defS = "yes"
	}
	var got bool
	_, ok := p.ask(label+" (yes|no)", defS, func(v string) string {
		switch strings.ToLower(v) {
		case "y", "yes", "true":
			v = "true"
		case "n", "no", "false":
			v = "false"
		}
		b, rule := parseBool(v)
		if rule != "" {
			return "answer yes or no"
		}
		if extra != nil {
			if rule = extra(b); rule != "" {
				return rule
			}
		}
		got = b
		return ""
	})
	return got, ok
}

// contextEdits turns the declared --context set into edits against the shared
// layer's current [context.*] tables: an entry whose name and dir already match
// is left as it is (its gate included), any other current one loses its whole
// table, and a new or changed one is written.
func contextEdits(cur map[string]cfglayer.Context, want []declaredContext) []cfglayer.Edit {
	keep := map[string]bool{}
	var adds []cfglayer.Edit
	for _, c := range want {
		key, have := "path", cur[c.Name].Path
		if c.Repo {
			key, have = "repo", cur[c.Name].Repo
		}
		if _, declared := cur[c.Name]; declared && have == c.Dir {
			keep[c.Name] = true
			continue
		}
		adds = append(adds, cfglayer.Edit{Op: cfglayer.SetKey, Table: []string{"context", c.Name}, Key: key, Value: c.Dir})
	}
	var edits []cfglayer.Edit
	for _, name := range slices.Sorted(maps.Keys(cur)) {
		if !keep[name] {
			edits = append(edits, cfglayer.Edit{Op: cfglayer.RemoveTable, Table: []string{"context", name}})
		}
	}
	return append(edits, adds...)
}

// localContexts reports whether the machine-local layer declares [context.*].
func localContexts(root string) bool {
	var l struct{ Context map[string]cfglayer.Context }
	return cfglayer.Decode(cfglayer.LocalPath(root), &l) == cfglayer.Set && len(l.Context) > 0
}
