package bd

import (
	"slices"
	"strings"
	"testing"
)

// e builds a row "from depends on to": ids carry their kind and status as
// "id:kind:status" so each table case reads as a graph.
func e(typ, from, to string) spineRow {
	f, t := parseEnd(from), parseEnd(to)
	return spineRow{f[0], t[0], typ, f[2], t[2], "kind:" + f[1], "kind:" + t[1]}
}

func parseEnd(s string) [3]string {
	var out [3]string
	copy(out[:], strings.Split(s, ":"))
	return out
}

func TestSpineOf(t *testing.T) {
	for _, c := range []struct {
		name   string
		target string
		rows   []spineRow
		want   []string
		open   bool
	}{
		{"fast lane", "sig", []spineRow{
			e("caused-by", "spec:spec:closed", "sig:signal:closed"),
			e("caused-by", "cp:checkpoint:closed", "spec:spec:closed"),
			e("parent-child", "b1:build:closed", "cp:checkpoint:closed"),
		}, []string{"b1", "cp", "sig", "spec"}, false},
		{"charted, open build holds", "sig", []spineRow{
			e("caused-by", "ch:chart:closed", "sig:signal:closed"),
			e("caused-by", "spec:spec:closed", "ch:chart:closed"),
			e("parent-child", "b1:build:open", "cp:checkpoint:closed"),
			e("caused-by", "cp:checkpoint:closed", "spec:spec:closed"),
		}, []string{"b1", "ch", "cp", "sig", "spec"}, true},
		{"open offshoot signal excluded", "sig", []spineRow{
			e("caused-by", "ch:chart:closed", "sig:signal:closed"),
			e("discovered-from", "off:signal:open", "ch:chart:closed"),
		}, []string{"ch", "sig"}, false},
		{"supersedes duplicate excluded", "sig", []spineRow{
			e("caused-by", "ch:chart:closed", "sig:signal:closed"),
			e("supersedes", "dup:signal:open", "sig:signal:closed"),
		}, []string{"ch", "sig"}, false},
		{"rollback and open successor held", "cp", []spineRow{
			e("related", "rb:rollback:closed", "cp:checkpoint:closed"),
			e("caused-by", "succ:checkpoint:open", "rb:rollback:closed"),
		}, []string{"cp", "rb", "succ"}, true},
		{"pivot via discovered-from and related", "cp", []spineRow{
			e("parent-child", "b:build:closed", "cp:checkpoint:closed"),
			e("discovered-from", "pv:pivot:closed", "b:build:closed"),
			e("related", "pv:pivot:closed", "cp2:checkpoint:closed"),
		}, []string{"b", "cp", "cp2", "pv"}, false},
		{"initiative end never crossed", "b", []spineRow{
			e("parent-child", "b:build:closed", "cp:checkpoint:closed"),
			e("parent-child", "cp:checkpoint:closed", "in:initiative:open"),
			e("parent-child", "other:signal:open", "in:initiative:open"),
		}, []string{"b", "cp"}, false},
		{"initiative: union, own bead, caused-by signal; open task holds", "in", []spineRow{
			e("parent-child", "s1:signal:closed", "in:initiative:closed"),
			e("parent-child", "t:task:open", "in:initiative:closed"),
			e("parent-child", "pk:signal:closed", "in:initiative:closed"),
			e("caused-by", "in:initiative:closed", "isig:signal:closed"),
			e("caused-by", "ch:chart:closed", "s1:signal:closed"),
			e("caused-by", "isig:signal:closed", "pre:signal:open"),
		}, []string{"ch", "in", "isig", "pk", "s1", "t"}, true},
		{"initiative: parked member does not hold", "in", []spineRow{
			e("parent-child", "s1:signal:closed", "in:initiative:closed"),
			e("parent-child", "pk:signal:closed", "in:initiative:closed"),
		}, []string{"in", "pk", "s1"}, false},
		{"draft counts open", "sig", []spineRow{
			e("caused-by", "d:chart:deferred", "sig:signal:closed"),
		}, []string{"d", "sig"}, true},
		{"blocked counts open", "sig", []spineRow{
			e("caused-by", "d:chart:blocked", "sig:signal:closed"),
		}, []string{"d", "sig"}, true},
		{"surgery checkpoint: itself and members", "cp", []spineRow{
			e("parent-child", "sg:surgery:closed", "cp:checkpoint:closed"),
			e("parent-child", "rv:review:closed", "cp:checkpoint:closed"),
			e("parent-child", "ld:landing:closed", "cp:checkpoint:closed"),
		}, []string{"cp", "ld", "rv", "sg"}, false},
		{"discussion related edge not followed", "sig", []spineRow{
			e("caused-by", "ch:chart:closed", "sig:signal:closed"),
			e("related", "rt:discussion:open", "ch:chart:closed"),
			e("discovered-from", "rt:discussion:open", "sig:signal:closed"),
		}, []string{"ch", "sig"}, false},
		{"two kind labels keyed once", "sig", []spineRow{
			// build is labelled kind:build first, then kind:initiative on a duplicate row
			e("caused-by", "x:build:closed", "sig:signal:closed"),
			{"x", "sig", "caused-by", "closed", "closed", "kind:initiative", "kind:signal"},
		}, []string{"sig", "x"}, false},
		{"lone target", "sig", []spineRow{e("self", "sig:signal:open", "sig:signal:open")}, []string{"sig"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, open := spineOf(c.target, c.rows)
			if !slices.Equal(got, c.want) || open != c.open {
				t.Errorf("spineOf = %v open=%v, want %v open=%v", got, open, c.want, c.open)
			}
		})
	}
}

func TestSpineReadsCSV(t *testing.T) {
	orun := run
	t.Cleanup(func() { run = orun })
	run = func(args ...string) (string, error) {
		return "issue_id,depends_on_issue_id,type,status,status,label,label\n" +
			"ch,sig,caused-by,closed,open,kind:chart,kind:signal\n" +
			"sig,sig,self,open,open,kind:signal,kind:signal\n", nil
	}
	ids, open, err := Spine("sig")
	if err != nil || open != true || !slices.Equal(ids, []string{"ch", "sig"}) {
		t.Errorf("Spine = %v %v %v", ids, open, err)
	}
}
