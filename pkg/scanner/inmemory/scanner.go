package inmemoryscanner

import (
	"errors"
	"fmt"
	"io"

	"github.com/ornew/pego/pkg/scanner"
	"github.com/ornew/pego/pkg/token"
)

var (
	_ scanner.Scanner = (*Scanner)(nil)
	_ io.ReaderAt     = (*Scanner)(nil)
)

type Scanner struct {
	src   []byte
	pos   token.Pos
	depth int
}

func New(src []byte) *Scanner {
	return &Scanner{
		src: src,
		pos: 0,
	}
}

func (s *Scanner) Pos() token.Pos {
	return s.pos
}

func (s *Scanner) IsEnd() bool {
	return int64(len(s.src)) <= s.pos.Int64()
}

func (s *Scanner) Peek() byte {
	return s.src[s.pos]
}

func (s *Scanner) Seek(pos int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		s.pos = token.Pos(pos)
	case io.SeekEnd:
		s.pos = token.Pos(int64(len(s.src)) + pos)
	case io.SeekCurrent:
		s.pos += token.Pos(pos)
	default:
		return 0, errors.New("invalid whence")
	}
	return int64(s.pos), nil
}

func (s *Scanner) ReadAt(dst []byte, off int64) (n int, err error) {
	// | src               |
	// | off               : off-over | dst | => NG
	// | off               | dst |            => OK (but not copy)
	// | off | dst         |                  => OK
	// | off | dst | rem   |                  => OK
	// | off | dst         : dst-over |       => NG (but not copy all)
	start := int(off)
	eof := len(s.src)
	if start >= eof {
		return 0, fmt.Errorf("offset out of range: %d not in [0:%d]", start, eof)
	}
	end := start + len(dst)
	if end > eof {
		end = len(s.src)
	}
	n = copy(dst, s.src[start:end])
	return n, nil
}
