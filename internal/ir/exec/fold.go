package exec

// Fold returns the value of x if x is an operator or a conversion whose operands are constants,
// evaluated as Run evaluates it. A division by zero is not folded. Backends print such an
// expression as its value, because target languages evaluate constant expressions in their own
// arithmetic (Go rejects one that overflows).
func Fold(x Expr) (*Const, bool) {
	switch x := x.(type) {
	case *Const:
		return x, true
	case *Unary:
		c, ok := Fold(x.X)
		if !ok {
			return nil, false
		}
		if x.Op == "!" {
			return &Const{Type: TBool, Value: !c.Value.(bool)}, true
		}
		return &Const{Type: c.Type, Value: Apply("-", ConvertValue(int64(0), c.Type), c.Value)}, true
	case *Binary:
		a, ok1 := Fold(x.X)
		b, ok2 := Fold(x.Y)
		if !ok1 || !ok2 || a.Type.Kind == KHandle {
			return nil, false
		}
		switch x.Op {
		case "&&":
			return &Const{Type: TBool, Value: a.Value.(bool) && b.Value.(bool)}, true
		case "||":
			return &Const{Type: TBool, Value: a.Value.(bool) || b.Value.(bool)}, true
		case "/", "%":
			if b.Value == ConvertValue(int64(0), b.Type) {
				return nil, false
			}
		}
		v := Apply(x.Op, a.Value, b.Value)
		t := a.Type
		if _, isBool := v.(bool); isBool {
			t = TBool
		}
		return &Const{Type: t, Value: v}, true
	case *Convert:
		c, ok := Fold(x.X)
		if !ok {
			return nil, false
		}
		return &Const{Type: x.To, Value: ConvertValue(c.Value, x.To)}, true
	}
	return nil, false
}
