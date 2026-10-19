package cfglayer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2/unstable"
)

// ErrEditByHand is the refusal for an owned key found in a shape the splicer
// can't edit safely (a top-level dotted key, an inline table, a non-scalar
// value, an array table, a duplicate).
var ErrEditByHand = errors.New("edit by hand")

// EditOp is what an Edit does.
type EditOp int

const (
	// SetKey sets Table.Key to Value, inserting the key (and the table) when absent.
	SetKey EditOp = iota
	// RemoveKey removes Table.Key; absent is a no-op.
	RemoveKey
	// RemoveTable removes the [Table] header and its own keys; absent is a no-op.
	RemoveTable
)

// Edit is one change to an owned key of a config layer. Table is the dotted
// table path ("context","x"), empty for the top level; Value is a string, bool
// or int.
type Edit struct {
	Op    EditOp
	Table []string
	Key   string
	Value any
}

// WriteOwned applies edits to the layer at path and leaves every other byte
// identical. go-toml's unstable parser only finds byte ranges; the BurntSushi
// read path stays the only decoder: the spliced bytes are decoded through
// Decode and every owned value must read back as intended before a temp file
// is renamed over path, so a failure leaves the original untouched. A missing
// file is written fresh, its keys in edit order.
func WriteOwned(path string, edits []Edit) error {
	src, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, serr := os.Stat(path); serr == nil {
		mode = fi.Mode().Perm()
	}
	out, err := Splice(src, edits)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "config.toml.*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // a no-op once renamed
	_, err = f.Write(out)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), mode)
	}
	if err != nil {
		return err
	}
	if err := readBack(f.Name(), edits); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// readBack decodes the spliced layer at path and checks every edit landed.
func readBack(path string, edits []Edit) error {
	var got map[string]any
	if Decode(path, &got) != Set {
		return errors.New("spliced config does not decode; original left untouched")
	}
	for i, e := range edits {
		var cur any = got
		for _, p := range e.path() {
			m, _ := cur.(map[string]any)
			cur = m[p]
		}
		switch e.Op {
		case SetKey:
			if !sameValue(cur, e.Value) {
				return fmt.Errorf("spliced config reads %s back as %v, want %v; original left untouched", strings.Join(e.path(), "."), cur, e.Value)
			}
		case RemoveKey:
			if cur != nil {
				return fmt.Errorf("spliced config still has %s; original left untouched", strings.Join(e.path(), "."))
			}
		case RemoveTable:
			if m, ok := cur.(map[string]any); ok && len(m) > 0 {
				// A sub-table of the removed table may legitimately remain.
				// Keys a later SetKey re-creates are judged by that edit; any
				// other plain key is a leftover of the removed table.
				for k, v := range m {
					if _, sub := v.(map[string]any); !sub && !reSet(edits[i+1:], e.Table, k) {
						return fmt.Errorf("spliced config still has [%s]; original left untouched", strings.Join(e.path(), "."))
					}
				}
			}
		}
	}
	return nil
}

// reSet reports whether a SetKey in later puts table.key back.
func reSet(later []Edit, table []string, key string) bool {
	return slices.ContainsFunc(later, func(e Edit) bool {
		return e.Op == SetKey && e.Key == key && slices.Equal(e.Table, table)
	})
}

func sameValue(got, want any) bool {
	if n, ok := want.(int); ok {
		want = int64(n)
	}
	return got == want
}

func (e Edit) path() []string {
	if e.Op == RemoveTable {
		return e.Table
	}
	return append(slices.Clone(e.Table), e.Key)
}

// Splice applies edits to src in order, one at a time, each against the bytes
// the previous one produced.
func Splice(src []byte, edits []Edit) ([]byte, error) {
	for _, e := range edits {
		var err error
		if src, err = spliceOne(src, e); err != nil {
			return nil, err
		}
	}
	return src, nil
}

// kv is one key = value expression.
type kv struct {
	table      []string
	key        []string
	first, end int // line span, end inclusive
	valOff     int
	valLen     int
	scalar     bool
}

func (k kv) full() []string { return append(slices.Clone(k.table), k.key...) }

// header is one [table] or [[array.table]] line.
type header struct {
	path  []string
	array bool
	line  int
}

// doc is a parsed layer: its lines, key/values and headers in file order.
type doc struct {
	src     []byte
	starts  []int // byte offset of each line
	kvs     []kv
	headers []header
}

func parseDoc(src []byte) (*doc, error) {
	d := &doc{src: src, starts: []int{0}}
	for i, b := range src {
		if b == '\n' && i+1 < len(src) {
			d.starts = append(d.starts, i+1)
		}
	}
	var p unstable.Parser
	p.KeepComments = true
	p.Reset(src)
	var table []string
	for p.NextExpression() {
		n := p.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			var path []string
			line := -1
			for it := n.Key(); it.Next(); {
				path = append(path, string(it.Node().Data))
				if line < 0 {
					line = d.lineOf(int(it.Node().Raw.Offset))
				}
			}
			d.headers = append(d.headers, header{path: path, array: n.Kind == unstable.ArrayTable, line: line})
			table = path
		case unstable.KeyValue:
			k := kv{table: table, first: d.lineOf(int(n.Raw.Offset)), end: d.lineOf(int(n.Raw.Offset) + int(n.Raw.Length) - 1)}
			for it := n.Key(); it.Next(); {
				k.key = append(k.key, string(it.Node().Data))
			}
			v := n.Value()
			switch v.Kind {
			case unstable.String, unstable.Bool, unstable.Integer:
				k.scalar = true
				k.valOff, k.valLen = int(v.Raw.Offset), int(v.Raw.Length)
			}
			d.kvs = append(d.kvs, k)
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("config does not parse: %w", err)
	}
	return d, nil
}

func (d *doc) lineOf(off int) int {
	i, _ := slices.BinarySearch(d.starts, off+1)
	return i - 1
}

func (d *doc) lineText(i int) string {
	end := len(d.src)
	if i+1 < len(d.starts) {
		end = d.starts[i+1]
	}
	return strings.TrimRight(string(d.src[d.starts[i]:end]), "\r\n")
}

func (d *doc) isComment(i int) bool { return strings.HasPrefix(strings.TrimSpace(d.lineText(i)), "#") }
func (d *doc) isBlank(i int) bool   { return strings.TrimSpace(d.lineText(i)) == "" }

// commentStart is the first line of the comment block directly above line i
// (no blank line between), or i when there is none.
func (d *doc) commentStart(i int) int {
	for i > 0 && d.isComment(i-1) {
		i--
	}
	return i
}

func (d *doc) nlines() int { return len(d.starts) }

func (d *doc) offset(line int) int {
	if line >= len(d.starts) {
		return len(d.src)
	}
	return d.starts[line]
}

func related(a, b []string) bool {
	n := min(len(a), len(b))
	return slices.Equal(a[:n], b[:n])
}

// guard refuses an edit whose target exists in a shape other than [table] key
// = scalar, and returns the plain key (if any) and table header.
func (d *doc) guard(e Edit) (found *kv, hdr *header, err error) {
	target := e.path()
	refuse := func(what string) error {
		return fmt.Errorf("%w: %s is %s in this config; the splicer only edits a plain [table] key = value", ErrEditByHand, strings.Join(target, "."), what)
	}
	for i, k := range d.kvs {
		f := k.full()
		if e.Op == RemoveTable {
			// Keys of the table itself, and of its sub-tables, are fine.
			if len(k.table) >= len(target) && slices.Equal(k.table[:len(target)], target) {
				continue
			}
			if related(f, target) {
				return nil, nil, refuse("defined by a dotted key or inline table")
			}
			continue
		}
		if !related(f, target) {
			continue
		}
		if slices.Equal(k.table, e.Table) && len(k.key) == 1 && k.key[0] == e.Key {
			if !k.scalar {
				return nil, nil, refuse("a non-scalar value")
			}
			if found != nil {
				return nil, nil, refuse("defined twice")
			}
			found = &d.kvs[i]
			continue
		}
		return nil, nil, refuse("defined by a dotted key or inline table")
	}
	for i, h := range d.headers {
		if h.array && related(h.path, target) {
			return nil, nil, refuse("under an array table")
		}
		if !h.array && slices.Equal(h.path, e.Table) || e.Op == RemoveTable && slices.Equal(h.path, target) {
			if hdr != nil {
				return nil, nil, refuse("a table declared twice")
			}
			hdr = &d.headers[i]
		}
	}
	return found, hdr, nil
}

func spliceOne(src []byte, e Edit) ([]byte, error) {
	d, err := parseDoc(src)
	if err != nil {
		return nil, err
	}
	found, hdr, err := d.guard(e)
	if err != nil {
		return nil, err
	}
	nl := "\n"
	if bytes.Contains(src, []byte("\r\n")) {
		nl = "\r\n"
	}
	switch e.Op {
	case SetKey:
		val, err := renderValue(e.Value)
		if err != nil {
			return nil, err
		}
		if found != nil {
			return slices.Concat(src[:found.valOff], []byte(val), src[found.valOff+found.valLen:]), nil
		}
		return d.insertKey(e, hdr, e.Key+" = "+val, nl), nil
	case RemoveKey:
		if found == nil {
			return src, nil
		}
		return d.cut(d.commentStart(found.first), found.end+1), nil
	default:
		if hdr == nil {
			return src, nil
		}
		start := d.commentStart(hdr.line)
		end := d.nlines()
		for _, h := range d.headers {
			if h.line > hdr.line {
				end = d.commentStart(h.line)
				break
			}
		}
		if end == d.nlines() {
			for start > 0 && d.isBlank(start-1) {
				start--
			}
		}
		return d.cut(start, end), nil
	}
}

// cut removes lines [from, to).
func (d *doc) cut(from, to int) []byte {
	return slices.Concat(d.src[:d.offset(from)], d.src[d.offset(to):])
}

// insertAt puts text (lines, no trailing newline) before line at.
func (d *doc) insertAt(at int, text, nl string) []byte {
	head := d.src[:d.offset(at)]
	if len(head) > 0 && head[len(head)-1] != '\n' {
		head = append(slices.Clone(head), nl...)
	}
	return slices.Concat(head, []byte(text+nl), d.src[d.offset(at):])
}

func (d *doc) insertKey(e Edit, hdr *header, line, nl string) []byte {
	// After the last key line of the table, or its header when it has none.
	last := -1
	for _, k := range d.kvs {
		if slices.Equal(k.table, e.Table) {
			last = k.end
		}
	}
	switch {
	case last >= 0:
		return d.insertAt(last+1, line, nl)
	case hdr != nil:
		return d.insertAt(hdr.line+1, line, nl)
	case len(e.Table) == 0:
		if len(d.headers) == 0 {
			return d.insertAt(d.nlines(), line, nl)
		}
		// Above the comment block that heads the first table (or at file top).
		return d.insertAt(d.commentStart(d.headers[0].line), line+nl, nl)
	}
	text := "[" + strings.Join(quoteKeys(e.Table), ".") + "]" + nl + line
	if len(d.src) > 0 {
		text = nl + text
		if d.isBlank(d.nlines() - 1) {
			text = text[len(nl):]
		}
	}
	return d.insertAt(d.nlines(), text, nl)
}

func quoteKeys(path []string) []string {
	out := make([]string, len(path))
	for i, p := range path {
		out[i] = p
		if p == "" || strings.Trim(p, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
			out[i] = strconv.Quote(p)
		}
	}
	return out
}

func renderValue(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return strconv.Quote(v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	}
	return "", fmt.Errorf("cfglayer: unsupported owned value %T", v)
}
