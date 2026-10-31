package editfuzz

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ornew/zeroparse"
)

// A Mismatch is an operation after which the Document does not behave like a fresh parse: at a checkpoint
// or the initial text, its result differs from a fresh parse of its text; after an edit, undo or redo,
// its text is not the edited text; or an operation or a parse panicked.
type Mismatch struct {
	Backend zp.Backend
	Unit    zp.PositionUnit
	// Event is the index of the operation, or -1 for the initial text.
	Event int
	// Text is the Document's text at a checkpoint, or its text before the operation that changed it
	// wrongly or panicked.
	Text string
	// Path is the first differing location in the compared results, such as "value.value[1].end" or
	// "error.errors[0].expected" ("value_invalid_utf8[i]" for strings that are not valid UTF-8); "text" for
	// a wrong text or an undo or redo that the Document rejected; or "panic.edit", "panic.document",
	// "panic.fresh" or "panic.both" for a panic in an operation, the Document parse, the fresh parse or both.
	Path string
	// Document and Fresh are the differing values at Path as JSON: those of the Document's result and of
	// the fresh parse, the Document's text and the expected text, or the panic values ("" for none).
	Document, Fresh string
}

// Category is "value" when the parse values differ, "error" when the errors differ, "text" when an edit
// produced a wrong text and "panic" when an operation or a parse panicked.
func (m *Mismatch) Category() string {
	category, _, _ := strings.Cut(m.Path, ".")
	category, _, _ = strings.Cut(category, "[")
	if category == "value_invalid_utf8" {
		return "value"
	}
	return category
}

var pathIndexes = regexp.MustCompile(`\[[0-9]+\]`)

// Fingerprint identifies the kind of failure independently of the input: the category, backend, unit and
// first differing location without list indexes. Minimize keeps it unchanged.
func (m *Mismatch) Fingerprint() string {
	return fmt.Sprintf("%s %s %s %s", m.Category(), m.Backend, m.Unit, pathIndexes.ReplaceAllString(m.Path, "[]"))
}

func (m *Mismatch) Error() string {
	at := "initial text"
	if m.Event >= 0 {
		at = fmt.Sprintf("operation %d", m.Event)
	}
	what := "document result differs from a fresh parse"
	switch m.Category() {
	case "panic":
		what = "panicked"
	case "text":
		what = "document text differs from the edited text"
	}
	// Texts and panic values are quoted; result values are JSON already.
	quote := m.Path == "text" || m.Category() == "panic"
	return fmt.Sprintf("%s/%s: %s at %s (text %s) in %s:\ndocument: %s\nfresh:    %s",
		m.Backend, m.Unit, what, at, shorten(m.Text, true), m.Path, shorten(m.Document, quote), shorten(m.Fresh, quote))
}

// shorten returns s, quoted if quote is set, eliding its middle if it is long.
func shorten(s string, quote bool) string {
	const limit = 200
	q := func(s string) string {
		if quote {
			return strconv.Quote(s)
		}
		return s
	}
	if len(s) <= limit {
		return q(s)
	}
	return q(strings.ToValidUTF8(s[:limit/2], "")) + "…" + q(strings.ToValidUTF8(s[len(s)-limit/2:], ""))
}

// An InvalidError reports a sequence that cannot be applied: a grammar that does not match the recorded
// hash or does not compile, an unknown backend or unit, an edit outside the text or off a character
// boundary, or an undo or redo without a corresponding edit.
type InvalidError struct {
	// Event is the index of the operation, or -1 for the sequence as a whole.
	Event int
	Err   error
}

func (e *InvalidError) Error() string {
	if e.Event < 0 {
		return "invalid sequence: " + e.Err.Error()
	}
	return fmt.Sprintf("invalid sequence: operation %d: %v", e.Event, e.Err)
}

func (e *InvalidError) Unwrap() error { return e.Err }

// Replay applies seq to a Document of the grammar source src and compares results at the initial text and
// at each checkpoint. It returns nil if all of them match, a *Mismatch for the first that does not, and an
// *InvalidError if the sequence cannot be applied.
func Replay(src []byte, seq *Sequence) error {
	p, err := compileFor(src, seq)
	if err != nil {
		return err
	}
	return replay(p, seq)
}

func compileFor(src []byte, seq *Sequence) (*zp.Parser, error) {
	if GrammarHash(src) != seq.GrammarSHA256 {
		return nil, &InvalidError{-1, errors.New("grammar source does not match the recorded hash")}
	}
	if _, err := backendName(seq.Backend); err != nil {
		return nil, &InvalidError{-1, err}
	}
	if _, err := unitName(seq.Unit); err != nil {
		return nil, &InvalidError{-1, err}
	}
	p, err := zp.CompileSource(string(src), seq.Start)
	if err != nil {
		return nil, &InvalidError{-1, err}
	}
	return p, nil
}

// A step is an applied edit with the observed texts around it.
type step struct {
	edit          Operation
	before, after string
}

type replayer struct {
	p          *zp.Parser
	seq        *Sequence
	opts       []zp.ParseOption
	doc        *zp.Document
	undo, redo []step
}

func replay(p *zp.Parser, seq *Sequence) error {
	r, err := start(p, seq)
	if err != nil {
		return err
	}
	for i, op := range seq.Operations {
		if err := r.applyAt(op, i); err != nil {
			return err
		}
		if op.Kind == Checkpoint {
			if m := r.check(i); m != nil {
				return m
			}
		}
	}
	return nil
}

// start creates the Document of seq's initial text and checks it.
func start(p *zp.Parser, seq *Sequence) (r *replayer, err error) {
	r = &replayer{p: p, seq: seq, opts: []zp.ParseOption{zp.WithExpectations(), zp.WithBackend(seq.Backend), zp.WithUnit(seq.Unit), zp.WithLimits(seq.Limits)}}
	initial := string(seq.Initial)
	defer func() {
		if x := recover(); x != nil {
			err = r.mismatch(-1, initial, "panic.edit", fmt.Sprint(x), "")
		}
	}()
	if r.doc, err = p.NewDocument(initial, r.opts...); err != nil {
		return nil, &InvalidError{-1, err}
	}
	if want := r.textOf(initial); r.doc.Text() != want {
		return nil, r.mismatch(-1, initial, "text", r.doc.Text(), want)
	}
	if m := r.check(-1); m != nil {
		return nil, m
	}
	return r, nil
}

func (r *replayer) mismatch(event int, text, path, document, fresh string) *Mismatch {
	return &Mismatch{Backend: r.seq.Backend, Unit: r.seq.Unit, Event: event, Text: text, Path: path, Document: document, Fresh: fresh}
}

// textOf returns the text that a Document holds for s: s itself in Bytes, and s with each invalid byte
// replaced by U+FFFD in CodePoints, where a Document holds decoded code points.
func (r *replayer) textOf(s string) string {
	if r.seq.Unit == zp.Bytes {
		return s
	}
	var b strings.Builder
	for _, c := range s {
		b.WriteRune(c)
	}
	return b.String()
}

// errText reports that the Document's text is not the one an operation should have produced.
type errText struct{ got, want string }

func (e *errText) Error() string { return fmt.Sprintf("text %q, want %q", e.got, e.want) }

// applyAt applies the operation at index i, returning an *InvalidError if it does not apply and a
// *Mismatch if it produces a wrong text or panics.
func (r *replayer) applyAt(op Operation, i int) (err error) {
	text := r.doc.Text()
	defer func() {
		if x := recover(); x != nil {
			err = r.mismatch(i, text, "panic.edit", fmt.Sprint(x), "")
		}
	}()
	err = r.apply(op)
	if e, ok := err.(*errText); ok {
		return r.mismatch(i, text, "text", e.got, e.want)
	}
	if err != nil {
		return &InvalidError{i, err}
	}
	return nil
}

func (r *replayer) apply(op Operation) error {
	switch op.Kind {
	case Edit:
		before := r.doc.Text()
		if editHook != nil {
			editHook(op)
		}
		if err := r.doc.Edit(op.Start, op.End, string(op.Text)); err != nil {
			return err
		}
		if want := r.spliced(before, op); r.doc.Text() != want {
			return &errText{r.doc.Text(), want}
		}
		r.undo = append(r.undo, step{op, before, r.doc.Text()})
		r.redo = r.redo[:0]
	case Undo:
		if len(r.undo) == 0 {
			return errors.New("nothing to undo")
		}
		s := r.undo[len(r.undo)-1]
		r.undo = r.undo[:len(r.undo)-1]
		if err := r.restore(s.before); err != nil {
			return err
		}
		r.redo = append(r.redo, s)
	case Redo:
		if len(r.redo) == 0 {
			return errors.New("nothing to redo")
		}
		s := r.redo[len(r.redo)-1]
		r.redo = r.redo[:len(r.redo)-1]
		// The text is the one the edit was first applied to, so the edit applies again.
		if err := r.doc.Edit(s.edit.Start, s.edit.End, string(s.edit.Text)); err != nil {
			return &errText{"edit rejected: " + err.Error(), s.after}
		}
		if r.doc.Text() != s.after {
			return &errText{r.doc.Text(), s.after}
		}
		r.undo = append(r.undo, s)
	case Checkpoint:
	default:
		return fmt.Errorf("unknown operation kind %q", op.Kind)
	}
	return nil
}

// restore changes the Document's text to target with one edit of the range that differs, which applies
// even when an edit joined bytes into a character across its old boundaries.
func (r *replayer) restore(target string) error {
	start, end, text := diff(r.doc.Text(), target, r.seq.Unit)
	// The range is on character boundaries, so the edit applies.
	if err := r.doc.Edit(start, end, text); err != nil {
		return &errText{"edit rejected: " + err.Error(), target}
	}
	if r.doc.Text() != target {
		return &errText{r.doc.Text(), target}
	}
	return nil
}

// spliced returns the text that the edit op should make of before.
func (r *replayer) spliced(before string, op Operation) string {
	start, end := op.Start, op.End
	if r.seq.Unit != zp.Bytes {
		b := boundaries(before)
		start, end = b[start], b[end]
	}
	return before[:start] + r.textOf(string(op.Text)) + before[end:]
}

// editHook lets tests act before each edit.
var editHook func(Operation)

// boundaries returns the byte offsets of the character boundaries of s, where each invalid UTF-8 byte is a
// character, as in parsing.
func boundaries(s string) []int {
	b := []int{0}
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		b = append(b, i)
	}
	return b
}

// diff returns the edit, in unit, that changes cur to target: the range of cur between their common
// prefix and suffix of whole characters, and the replacement from target.
func diff(cur, target string, unit zp.PositionUnit) (start, end int, text string) {
	bc, bt := boundaries(cur), boundaries(target)
	nc, nt := len(bc)-1, len(bt)-1
	char := func(s string, b []int, i int) string { return s[b[i]:b[i+1]] }
	pre := 0
	for pre < nc && pre < nt && char(cur, bc, pre) == char(target, bt, pre) {
		pre++
	}
	suf := 0
	for suf < nc-pre && suf < nt-pre && char(cur, bc, nc-1-suf) == char(target, bt, nt-1-suf) {
		suf++
	}
	text = target[bt[pre]:bt[nt-suf]]
	if unit == zp.Bytes {
		return bc[pre], bc[nc-suf], text
	}
	return pre, nc - suf, text
}

// corruptDocumentResult lets tests inject mismatches into the Document's result tree.
var corruptDocumentResult func(text string, tree map[string]any)

// check compares the Document's parse with a fresh parse of its text.
func (r *replayer) check(event int) *Mismatch {
	// Encode the Document's result before any other parse can move the nodes it shares.
	var docErr, freshErr error
	doc, docPanic := result(func() (n *zp.Node, err error) { n, docErr = r.doc.Parse(); return n, docErr })
	text := r.doc.Text()
	fresh, freshPanic := result(func() (*zp.Node, error) { n, err := r.p.Parse(text, r.opts...); freshErr = err; return n, err })
	if docPanic != "" || freshPanic != "" {
		// A panic in the Document parse alone points at incremental parsing; in both, at the grammar or
		// the engine.
		path := "panic.both"
		if freshPanic == "" {
			path = "panic.document"
		} else if docPanic == "" {
			path = "panic.fresh"
		}
		return r.mismatch(event, text, path, docPanic, freshPanic)
	}
	// A Document reuses earlier work and spends less budget than a fresh parse, so an exhausted
	// budget on either side says nothing about the results: only parses that both complete compare.
	if budgetExhausted(docErr) || budgetExhausted(freshErr) {
		return nil
	}
	if corruptDocumentResult != nil {
		expand(doc)
		corruptDocumentResult(text, doc)
	}
	// Most results are equal: compare their JSON before decoding it.
	if a, ok := doc["value"].(json.RawMessage); ok {
		if b, ok := fresh["value"].(json.RawMessage); ok && bytes.Equal(a, b) {
			doc["value"], fresh["value"] = nil, nil
		}
	}
	expand(doc)
	expand(fresh)
	path, a, b := firstDifference("", doc, fresh)
	if path == "" {
		return nil
	}
	encode := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return r.mismatch(event, text, path, encode(a), encode(b))
}

// result parses and returns the value and error as a JSON-like tree: {"value": ..., "error": ...}, or the
// panic value if the parse panicked. Strings that are not valid UTF-8, which JSON cannot hold, are also
// listed quoted under "value_invalid_utf8", in tree order.
func result(parse func() (*zp.Node, error)) (tree map[string]any, panicked string) {
	defer func() {
		if x := recover(); x != nil {
			tree, panicked = nil, fmt.Sprint(x)
			if panicked == "" {
				panicked = "panic"
			}
		}
	}()
	n, err := parse()
	value := any(nil)
	if n != nil {
		b, merr := json.Marshal(n)
		if merr != nil {
			value = map[string]any{"unencodable": merr.Error()}
		} else {
			// Decoded by expand only if the results differ.
			value = json.RawMessage(b)
		}
	}
	tree = map[string]any{"value": value, "error": errorTree(err)}
	if invalid := invalidStrings(n); len(invalid) > 0 {
		// Named to sort after "value", so that a difference in the value is reported first.
		tree["value_invalid_utf8"] = invalid
	}
	return tree, ""
}

// expand decodes the JSON value of tree.
func expand(tree map[string]any) {
	raw, ok := tree["value"].(json.RawMessage)
	if !ok {
		return
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		value = map[string]any{"unencodable": err.Error()}
	}
	tree["value"] = value
}

// invalidStrings returns the quoted texts and string values of the tree at n that are not valid UTF-8.
func invalidStrings(n *zp.Node) []any {
	if n == nil {
		return nil
	}
	var l []any
	add := func(s string) {
		if !utf8.ValidString(s) {
			l = append(l, strconv.Quote(s))
		}
	}
	err := zp.Walk(n, func(v zp.WalkVisit) (zp.WalkAction, error) {
		if v.Node != nil {
			add(v.Node.Text())
		} else if s, ok := v.Value.(string); ok {
			add(s)
		}
		return zp.WalkContinue, nil
	})
	if err != nil {
		l = append(l, "walk: "+err.Error())
	}
	return l
}

// errorTree describes an error by its fields; errors without known fields are described by type and
// message.
func errorTree(err error) any {
	if err == nil {
		return nil
	}
	syntax := func(e *zp.SyntaxError) any {
		return map[string]any{"pos": e.Pos, "line": e.Line, "col": e.Col, "expected": anyList(e.Expected), "messages": anyList(e.Messages)}
	}
	var all zp.SyntaxErrors
	if errors.As(err, &all) {
		errs := make([]any, len(all))
		for i, e := range all {
			errs[i] = syntax(e)
		}
		return map[string]any{"kind": "syntax errors", "errors": errs}
	}
	var one *zp.SyntaxError
	if errors.As(err, &one) {
		return map[string]any{"kind": "syntax error", "error": syntax(one)}
	}
	return map[string]any{"kind": fmt.Sprintf("%T", err), "message": err.Error()}
}

func anyList(s []string) []any {
	l := make([]any, len(s))
	for i, x := range s {
		l[i] = x
	}
	return l
}

// firstDifference returns the path of the first difference between a and b, visiting map keys in sorted
// order, with the values there, or "" if they are equal.
func firstDifference(path string, a, b any) (string, any, any) {
	join := func(key string) string {
		if path == "" {
			return key
		}
		return path + "." + key
	}
	switch a := a.(type) {
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok {
			return orRoot(path), a, b
		}
		keys := make([]string, 0, len(a)+len(b))
		for k := range a {
			keys = append(keys, k)
		}
		for k := range b {
			if _, ok := a[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			av, aok := a[k]
			bv, bok := b[k]
			if !aok {
				av = absent{}
			}
			if !bok {
				bv = absent{}
			}
			if aok != bok {
				return join(k), av, bv
			}
			if p, x, y := firstDifference(join(k), av, bv); p != "" {
				return p, x, y
			}
		}
		return "", nil, nil
	case []any:
		b, ok := b.([]any)
		if !ok {
			return orRoot(path), a, b
		}
		for i := range min(len(a), len(b)) {
			if p, x, y := firstDifference(fmt.Sprintf("%s[%d]", path, i), a[i], b[i]); p != "" {
				return p, x, y
			}
		}
		if len(a) != len(b) {
			i := min(len(a), len(b))
			x, y := any(absent{}), any(absent{})
			if i < len(a) {
				x = a[i]
			}
			if i < len(b) {
				y = b[i]
			}
			return fmt.Sprintf("%s[%d]", path, i), x, y
		}
		return "", nil, nil
	}
	if fmt.Sprintf("%T %v", a, a) != fmt.Sprintf("%T %v", b, b) {
		return orRoot(path), a, b
	}
	return "", nil, nil
}

// absent stands for a missing list element or map entry in a difference.
type absent struct{}

func (absent) MarshalJSON() ([]byte, error) { return []byte(`"(absent)"`), nil }

func orRoot(path string) string {
	if path == "" {
		return "result"
	}
	return path
}

// budgetExhausted reports whether err is an exhausted budget.
func budgetExhausted(err error) bool {
	var l *zp.LimitError
	return errors.As(err, &l) && l.Kind == zp.LimitBudget
}
