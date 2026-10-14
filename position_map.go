package pego

import "github.com/ornew/pego/internal/sourcepos"

// PositionEncoding selects the coordinate unit, independently of parser Unit.
type PositionEncoding = sourcepos.Encoding

const (
	PositionBytes      = sourcepos.Bytes
	PositionCodePoints = sourcepos.CodePoints
	PositionUTF16      = sourcepos.UTF16
)

// Position is a zero-based line and character coordinate.
type Position = sourcepos.Position

// PositionMap maps an immutable UTF-8 source snapshot to editor coordinates.
// LF, CRLF and lone CR terminate lines. Malformed UTF-8 bytes each count as one
// replacement code point and one UTF-16 unit. Conversions reject out-of-range
// coordinates, scalar interiors in Unicode modes and UTF-16 surrogate interiors.
// Line positions exclude terminators and cannot address the CRLF interior;
// flat conversions retain both terminator scalars. Byte mode permits UTF-8
// scalar interiors. The map is safe for concurrent reads.
type PositionMap = sourcepos.Map

// NewPositionMap indexes text without changing its bytes.
func NewPositionMap(text string) *PositionMap { return sourcepos.New(text) }
