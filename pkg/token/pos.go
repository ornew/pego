package token

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Position struct {
	Filename string
	Line     int
	Column   int
}

func (p Position) String() string {
	var b strings.Builder
	if p.Filename != "" {
		b.WriteString(p.Filename)
		b.WriteString(":")
	}
	b.WriteString(strconv.Itoa(p.Line + 1))
	b.WriteString(":")
	b.WriteString(strconv.Itoa(p.Column + 1))
	return b.String()
}

type Pos int64

const NoPos Pos = 0

type Offset int

func (p Pos) Valid() bool {
	return p > 0
}

func (p Pos) Int64() int64 {
	return int64(p)
}

func (p Pos) Add(n int) Pos {
	return p + Pos(n)
}

func (p Pos) From(f Pos) Offset {
	return Offset(p - f)
}

func (p Pos) Range() Range {
	return Range{p, p}
}

type Range struct {
	pos, end Pos
}

func (r Range) Pos() Pos {
	return r.pos
}

func (r Range) End() Pos {
	return r.end
}

type File struct {
	name      string
	lines     []Pos
	base, end Pos
}

func (f *File) Name() string {
	return f.name
}

func (f *File) Lines() []Pos {
	return f.lines
}

func (f *File) Base() Pos {
	return f.base
}

func (f *File) End() Pos {
	return f.end
}

func (f *File) Position(pos Pos) Position {
	if pos < f.base || pos > f.end {
		panic(fmt.Errorf("invalid position in file %q [%d,%d]: pos %d", f.name, f.base, f.end, pos))
	}
	for i, l := range f.lines {
		if pos < l {
			return Position{
				Line:     i - 1,
				Column:   int(pos - f.lines[i-1]),
				Filename: f.name,
			}
		}
	}
	end := f.lines[len(f.lines)-1]
	return Position{
		Line:     len(f.lines) - 1,
		Column:   int(pos - end),
		Filename: f.name,
	}
}

type FileSet struct {
	files []*File
	base  Pos
	last  Pos
}

func NewFileSet() *FileSet {
	return &FileSet{
		files: []*File{},
		base:  1,
		last:  1,
	}
}

func (f *FileSet) AddFile(base int, name, src string) *File {
	size := len(src)
	s := []byte(src)
	b := Pos(base)
	if base < 0 {
		b = f.last
	}
	f.last += Pos(size) + 1 // +1 EOF
	file := &File{
		name:  name,
		base:  b,
		end:   b.Add(size),
		lines: []Pos{b},
	}
	var c rune
	var w int
	var o int
	for {
		c, w = utf8.DecodeRune(s[o:])
		o += w
		if c == '\n' {
			file.lines = append(file.lines, file.base.Add(o))
		}
		if o >= size {
			break
		}
	}
	file.lines = append(file.lines, file.base.Add(o))
	f.files = append(f.files, file)
	return file
}

func (f *FileSet) Position(pos Pos) Position {
	if pos < f.base || pos > f.last {
		panic(fmt.Errorf("invalid position in file set [%d,%d): pos %d", f.base, f.last, pos))
	}
	for _, file := range f.files {
		if pos < file.end {
			return file.Position(pos)
		}
	}
	return Position{
		Line:   -1,
		Column: -1,
	}
}
