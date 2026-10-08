package golang

import (
	"go/constant"
	"go/token"
	"strconv"
)

// Value returns the string the literal denotes, with its quotes removed and its escapes decoded.
func (l *StringLit) Value() (string, error) { return strconv.Unquote(l.Text) }

// Value returns the rune the literal denotes.
func (l *CharLit) Value() (rune, error) {
	if len(l.Text) < 3 || l.Text[0] != '\'' || l.Text[len(l.Text)-1] != '\'' {
		return 0, strconv.ErrSyntax
	}
	r, _, tail, err := strconv.UnquoteChar(l.Text[1:len(l.Text)-1], '\'')
	if err != nil {
		return 0, err
	}
	if tail != "" {
		return 0, strconv.ErrSyntax
	}
	return r, nil
}

// Constant returns the value of the literal as go/constant does (a big integer if it does not
// fit an int64), or an error if the literal is not valid.
func (l *IntLit) Constant() (constant.Value, error) { return literal(token.INT, l.Text) }

// Constant returns the value of the literal as go/constant does (an exact rational number if it
// is finite), or an error if the literal is not valid.
func (l *FloatLit) Constant() (constant.Value, error) { return literal(token.FLOAT, l.Text) }

// Constant returns the value of the literal as go/constant does (a complex number with a zero
// real part), or an error if the literal is not valid.
func (l *ImagLit) Constant() (constant.Value, error) { return literal(token.IMAG, l.Text) }

func literal(kind token.Token, text string) (constant.Value, error) {
	v := constant.MakeFromLiteral(text, kind, 0)
	if v.Kind() == constant.Unknown {
		return nil, &strconv.NumError{Func: "Constant", Num: text, Err: strconv.ErrSyntax}
	}
	return v, nil
}
