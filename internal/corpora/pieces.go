package corpora

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// A Piece is a part of a text that is made from a range of a fetched file: a copy of a part of the range, or text of its
// own. Reference data that is about third-party text records where the text is in the corpus (file, offset and size) and,
// when the text is not that range as it is, the pieces that make it from the range. In JSON a copy is [offset, size] (of
// the range) and a text is a string.
type Piece struct {
	Off, Size int    // a copy of base[Off:Off+Size]
	Text      string // the text of a piece that is not a copy
	IsText    bool
}

// UnmarshalJSON reads a piece: a string, or a pair [offset, size].
func (p *Piece) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		p.IsText = true
		return json.Unmarshal(data, &p.Text)
	}
	var pair [2]int
	if err := json.Unmarshal(data, &pair); err != nil {
		return fmt.Errorf("piece %.40s: %w", data, err)
	}
	p.Off, p.Size = pair[0], pair[1]
	return nil
}

// Assemble makes the text of pieces from base. A copy that is not inside base is an error.
func Assemble(base []byte, pieces []Piece) ([]byte, error) {
	var out bytes.Buffer
	for _, p := range pieces {
		if p.IsText {
			out.WriteString(p.Text)
			continue
		}
		if p.Off < 0 || p.Size < 0 || p.Off+p.Size > len(base) {
			return nil, fmt.Errorf("piece [%d, %d] is outside the %d bytes of its range", p.Off, p.Size, len(base))
		}
		out.Write(base[p.Off : p.Off+p.Size])
	}
	return out.Bytes(), nil
}

// A Place says where a text is: the range [Off, Off+Size) of the file File, and the pieces that make the text from it
// (none if the range is the text).
type Place struct {
	File   string  `json:"f"`
	Off    int     `json:"o"`
	Size   int     `json:"n"`
	Pieces []Piece `json:"p"`
}

// Files reads the files of a fetched corpus, each once.
type Files struct {
	Dir   string
	cache map[string][]byte
}

// Read returns the content of a file of the corpus.
func (f *Files) Read(name string) ([]byte, error) {
	if data, ok := f.cache[name]; ok {
		return data, nil
	}
	data, err := os.ReadFile(filepath.Join(f.Dir, filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	if f.cache == nil {
		f.cache = map[string][]byte{}
	}
	f.cache[name] = data
	return data, nil
}

// Text returns the text that p says is in the corpus.
func (f *Files) Text(p Place) ([]byte, error) {
	data, err := f.Read(p.File)
	if err != nil {
		return nil, err
	}
	if p.Off < 0 || p.Size < 0 || p.Off+p.Size > len(data) {
		return nil, fmt.Errorf("%s: range [%d, %d] is outside the file of %d bytes", p.File, p.Off, p.Off+p.Size, len(data))
	}
	base := data[p.Off : p.Off+p.Size]
	if p.Pieces == nil {
		return base, nil
	}
	return Assemble(base, p.Pieces)
}

// Hash returns the first n hexadecimal digits of the SHA-256 of text: the check that reference data keeps of each text.
func Hash(text []byte, n int) string {
	sum := sha256.Sum256(text)
	return hex.EncodeToString(sum[:])[:n]
}
