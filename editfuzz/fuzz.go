package editfuzz

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/ornew/pego"
	"github.com/ornew/pego/sample"
)

// Defaults of Options.
const (
	DefaultSequences  = 100
	DefaultOperations = 32
)

// Options configure Fuzz.
type Options struct {
	// Start is the start rule. It must be set.
	Start string
	// Backend is the backend of the Documents and the fresh parses. The default backend is Closure.
	Backend pego.Backend
	// Unit is the position unit of the Documents, the fresh parses and the edits. The zero value is
	// CodePoints.
	Unit pego.Unit
	// Seed selects the generated sequences. The same seed, options and package version generate the same
	// sequences with the same Go release, but a saved Sequence, not a seed, is the reproducer.
	Seed uint64
	// Corpus holds the initial texts, in the order in which the seed selects them. Without a corpus, the
	// initial texts are inputs generated from the grammar with the seed, and the empty text. Some
	// sequences start from several texts joined into a text of a few kilobytes, which Documents store
	// differently from short ones.
	Corpus [][]byte
	// Sequences is the number of sequences to run (DefaultSequences if zero).
	Sequences int
	// Operations is the number of operations in each sequence (DefaultOperations if zero), the last of
	// which is a checkpoint.
	Operations int
	// SaveFailures is a directory where a failing sequence is saved; it is created if needed. Without
	// it, the error message of a failure includes the sequence as JSON.
	SaveFailures string
	// Minimize minimizes a sequence that fails with a *Mismatch, after saving the original, with at most
	// MaxAttempts replays (DefaultMaxAttempts if zero), and also saves the result.
	Minimize    bool
	MaxAttempts int
}

// A Failure is a generated sequence that failed: Err is a *Mismatch, or an *InvalidError for an edit
// that the Document rejected although its range was on character boundaries.
type Failure struct {
	Err      error
	Sequence *Sequence
	// Path is the file where the sequence was saved, or "".
	Path string
	// Minimized is the minimized sequence if Options.Minimize was set and Err is a *Mismatch, and
	// MinimizedPath the file where it was saved, or "".
	Minimized     *Sequence
	MinimizedPath string
}

func (f *Failure) Error() string {
	s := f.Err.Error()
	if _, ok := f.Err.(*Mismatch); !ok {
		s = fmt.Sprintf("%s/%s: %s", f.Sequence.Backend, f.Sequence.Unit, s)
	}
	if f.Path != "" {
		s += "\nsaved to " + f.Path
	} else if data, err := json.Marshal(f.Sequence); err == nil {
		s += "\nsequence: " + string(data)
	}
	if f.MinimizedPath != "" {
		s += fmt.Sprintf("\nminimized to %d operations in %s", len(f.Minimized.Operations), f.MinimizedPath)
	} else if f.Minimized != nil {
		if data, err := json.Marshal(f.Minimized); err == nil {
			s += fmt.Sprintf("\nminimized to %d operations: %s", len(f.Minimized.Operations), data)
		}
	}
	return s
}

func (f *Failure) Unwrap() error { return f.Err }

// Fuzz runs generated edit sequences on Documents of the grammar source src. It returns nil if every
// checkpoint matched a fresh parse and a *Failure for the first sequence that did not, after saving it to
// opts.SaveFailures if set. Other errors report an unusable grammar, options or failure directory.
func Fuzz(src []byte, opts Options) error {
	if opts.Backend == pego.DefaultBackend {
		opts.Backend = pego.Closure
	}
	if opts.Sequences <= 0 {
		opts.Sequences = DefaultSequences
	}
	if opts.Operations <= 0 {
		opts.Operations = DefaultOperations
	}
	if _, err := backendName(opts.Backend); err != nil {
		return err
	}
	if _, err := unitName(opts.Unit); err != nil {
		return err
	}
	if opts.Start == "" {
		return errors.New("no start rule")
	}
	p, err := pego.CompileSource(string(src), opts.Start)
	if err != nil {
		return err
	}
	texts := opts.Corpus
	if len(texts) == 0 {
		texts = [][]byte{{}}
		// A grammar without generable inputs is fuzzed from the empty text.
		generated, _ := sample.Generate(p, 16, sample.WithSeed(opts.Seed))
		for _, s := range generated {
			texts = append(texts, []byte(s))
		}
	}
	rng := rand.New(rand.NewPCG(opts.Seed, 0x9e3779b97f4a7c15))
	for range opts.Sequences {
		seq := &Sequence{GrammarSHA256: GrammarHash(src), Start: opts.Start, Backend: opts.Backend, Unit: opts.Unit,
			Seed: opts.Seed, Initial: initial(rng, texts)}
		err := run(p, seq, &generator{rng: rng, texts: texts}, opts.Operations)
		if sequenceDone != nil {
			sequenceDone(seq)
		}
		if err != nil {
			f := &Failure{Err: err, Sequence: seq}
			if opts.SaveFailures != "" {
				if f.Path, err = Save(opts.SaveFailures, seq); err != nil {
					return fmt.Errorf("%w\nsaving the sequence: %v", f, err)
				}
			}
			var m *Mismatch
			if opts.Minimize && errors.As(f.Err, &m) {
				if f.Minimized, _, _, err = minimize(p, seq, cmp.Or(max(opts.MaxAttempts, 0), DefaultMaxAttempts)); err != nil {
					return fmt.Errorf("%w\nminimizing the sequence: %v", f, err)
				}
				if opts.SaveFailures != "" {
					if f.MinimizedPath, err = Save(opts.SaveFailures, f.Minimized); err != nil {
						return fmt.Errorf("%w\nsaving the minimized sequence: %v", f, err)
					}
				}
			}
			return f
		}
	}
	return nil
}

// largeText is the size that joined initial texts reach.
const largeText = 6 << 10

// initial returns a copy of a random text, or one in eight times, of random texts joined until they reach
// largeText bytes.
func initial(rng *rand.Rand, texts [][]byte) []byte {
	t := bytes.Clone(texts[rng.IntN(len(texts))])
	if rng.IntN(8) > 0 {
		return t
	}
	for i := 0; len(t) < largeText && i < largeText; i++ {
		t = append(t, texts[rng.IntN(len(texts))]...)
	}
	return t
}

// sequenceDone lets tests observe the generated sequences.
var sequenceDone func(*Sequence)

// run applies n generated operations to a Document of seq's initial text, appending them to seq, and
// stops at the first failure.
func run(p *pego.Parser, seq *Sequence, g *generator, n int) error {
	r, err := start(p, seq)
	if err != nil {
		return err
	}
	for i := range n {
		op := g.next(r, i == n-1)
		seq.Operations = append(seq.Operations, op)
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

type generator struct {
	rng   *rand.Rand
	texts [][]byte
}

// pieces are replacements that join, split and complete characters with their neighbors.
var pieces = []string{"\n", " ", "é", "日", "😀", "\x80", "\xa9", "\xac", "\xc3", "\xe2\x82", "\xf0\x9f\x98", "\xff", "\xed\xa0\x80"}

// next returns an operation that applies to the replayer's current state.
func (g *generator) next(r *replayer, last bool) Operation {
	if last {
		return Operation{Kind: Checkpoint}
	}
	switch x := g.rng.IntN(20); {
	case x < 5:
		return Operation{Kind: Checkpoint}
	case x < 7 && len(r.undo) > 0:
		return Operation{Kind: Undo}
	case x < 9 && len(r.redo) > 0:
		return Operation{Kind: Redo}
	}
	text := r.doc.Text()
	b := boundaries(text)
	n := len(b) - 1
	var start int
	switch g.rng.IntN(4) {
	case 0:
		start = 0
	case 1:
		start = n
	default:
		start = g.rng.IntN(n + 1)
	}
	end := start
	if g.rng.IntN(3) > 0 {
		end += g.rng.IntN(min(n-start, 8) + 1)
	}
	op := Operation{Kind: Edit, Start: start, End: end, Text: g.replacement()}
	if r.seq.Unit == pego.Bytes {
		op.Start, op.End = b[start], b[end]
	}
	return op
}

// replacement returns nothing, a part of an initial text, pieces of characters or random bytes.
func (g *generator) replacement() []byte {
	switch g.rng.IntN(8) {
	case 0, 1:
		return []byte{}
	case 2, 3, 4:
		s := g.texts[g.rng.IntN(len(g.texts))]
		if len(s) == 0 {
			return []byte{}
		}
		i := g.rng.IntN(len(s))
		return append([]byte{}, s[i:min(len(s), i+1+g.rng.IntN(8))]...)
	case 5, 6:
		var s []byte
		for range 1 + g.rng.IntN(3) {
			s = append(s, pieces[g.rng.IntN(len(pieces))]...)
		}
		return s
	default:
		s := make([]byte, 1+g.rng.IntN(4))
		for i := range s {
			s[i] = byte(g.rng.Uint32())
		}
		return s
	}
}

// Save writes seq to a file in dir named after a hash of its contents and returns the file's path. The
// file appears complete or not at all (unless the file system lacks hard links), and an existing file is
// never replaced: if the file exists with the
// same contents, Save returns its path, and otherwise an error.
func Save(dir string, seq *Sequence) (string, error) {
	data, err := json.MarshalIndent(seq, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "editfuzz-"+GrammarHash(data)[:16]+".json")
	if old, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(old, data) {
			return "", fmt.Errorf("%s exists with other contents", path)
		}
		return path, nil
	}
	tmp, err := os.CreateTemp(dir, ".editfuzz-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	// A link, unlike a rename, fails rather than replace a file created meanwhile.
	if err := os.Link(tmp.Name(), path); err != nil {
		if old, rerr := os.ReadFile(path); rerr == nil {
			if bytes.Equal(old, data) {
				return path, nil
			}
			return "", fmt.Errorf("%s exists with other contents", path)
		}
		if errors.Is(err, os.ErrExist) {
			return "", err
		}
		// Without hard links, create the file exclusively; it may be seen before it is complete.
		if err := writeNew(path, data); err != nil {
			return "", err
		}
	}
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return path, nil
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

// Load reads a sequence saved by Save.
func Load(path string) (*Sequence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var seq Sequence
	if err := json.Unmarshal(data, &seq); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &seq, nil
}
