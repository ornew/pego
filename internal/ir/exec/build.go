package exec

// This file holds the builders: Go functions that construct a Module and its functions. A
// builder records what it is given; Verify checks it.

// NewModule returns an empty module.
func NewModule(name string, features ...string) *Module {
	return &Module{Name: name, Features: features}
}

// AddComponent declares a state component.
func (m *Module) AddComponent(name, doc string) *Component {
	c := &Component{Name: name, Doc: doc}
	m.Components = append(m.Components, c)
	return c
}

// AddRegister declares a register.
func (m *Module) AddRegister(name string, t Type, doc string) *Register {
	r := &Register{Name: name, Type: t, Doc: doc}
	m.Registers = append(m.Registers, r)
	return r
}

// AddHandle declares a handle type made by the operations of component.
func (m *Module) AddHandle(name, component, doc string) Type {
	m.Handles = append(m.Handles, &HandleType{Name: name, Component: component, Doc: doc})
	return THandle(name)
}

// AddOp declares an operation.
func (m *Module) AddOp(o *Op) *Op {
	m.Ops = append(m.Ops, o)
	return o
}

// P returns a parameter.
func P(name string, t Type) Param { return Param{Name: name, Type: t} }

// AddFunc builds a function: body receives a Builder positioned in the function's body.
func (m *Module) AddFunc(name string, params []Param, results []Type, features []string, body func(b *Builder)) *Func {
	f := &Func{Name: name, Params: params, Results: results, Features: features, Body: &Block{}}
	body(&Builder{f: f, block: f.Body})
	m.Funcs = append(m.Funcs, f)
	return f
}

// Builder appends statements to a block of a function.
type Builder struct {
	f     *Func
	block *Block
}

func (b *Builder) add(s Stmt) { b.block.Stmts = append(b.block.Stmts, s) }

func (b *Builder) nested(label string, body func(b *Builder)) *Block {
	blk := &Block{Label: label}
	body(&Builder{f: b.f, block: blk})
	return blk
}

// Append appends the statement s as it is.
func (b *Builder) Append(s Stmt) { b.add(s) }

// Var declares a local of the function at its entry (Func.Locals) and returns a reference to it.
func (b *Builder) Var(name string, t Type) Expr {
	b.f.Locals = append(b.f.Locals, P(name, t))
	return &Local{Name: name}
}

// Block appends a block labeled label (empty for none).
func (b *Builder) Block(label string, body func(b *Builder)) {
	b.add(b.nested(label, body))
}

// Loop appends a loop labeled label.
func (b *Builder) Loop(label string, body func(b *Builder)) {
	b.add(&Loop{Label: label, Body: b.nested("", body)})
}

// For appends a loop labeled label with the clauses init, cond and post, each of which may be nil
// (see Loop).
func (b *Builder) For(label string, init *Set, cond Expr, post *Set, body func(b *Builder)) {
	b.add(&Loop{Label: label, Init: init, Cond: cond, Post: post, Body: b.nested("", body)})
}

// If appends an if without else.
func (b *Builder) If(cond Expr, then func(b *Builder)) {
	b.add(&If{Cond: cond, Then: b.nested("", then)})
}

// IfElse appends an if with an else.
func (b *Builder) IfElse(cond Expr, then, els func(b *Builder)) {
	b.add(&If{Cond: cond, Then: b.nested("", then), Else: b.nested("", els)})
}

// Let declares one local with the value of x and returns a reference to it.
func (b *Builder) Let(name string, x Expr) Expr {
	b.add(&Let{Names: []string{name}, Value: x})
	return &Local{Name: name}
}

// LetN declares one local per result of the call c and returns references to them.
func (b *Builder) LetN(names []string, c *Call) []Expr {
	b.add(&Let{Names: names, Value: c})
	refs := make([]Expr, len(names))
	for i, n := range names {
		refs[i] = &Local{Name: n}
	}
	return refs
}

// Set assigns the local name.
func (b *Builder) Set(name string, x Expr) { b.add(Assign(name, x)) }

// SetReg assigns the register name.
func (b *Builder) SetReg(name string, x Expr) {
	b.add(&Set{Dst: []Place{{Name: name, Reg: true}}, Values: []Expr{x}})
}

// SetN assigns the results of the call c to the places dst.
func (b *Builder) SetN(dst []Place, c *Call) { b.add(&Set{Dst: dst, Values: []Expr{c}}) }

// Comment appends a comment.
func (b *Builder) Comment(text string) { b.add(&Comment{Text: text}) }

// Assign returns the assignment of x to the local name.
func Assign(name string, x Expr) *Set {
	return &Set{Dst: []Place{{Name: name}}, Values: []Expr{x}}
}

// Do calls callee for its effects.
func (b *Builder) Do(callee string, args ...Expr) {
	b.add(&Do{Call: &Call{Callee: callee, Args: args}})
}

// Exit appends an exit to the block labeled label.
func (b *Builder) Exit(label string) { b.add(&Exit{Label: label}) }

// Break appends a break of the loop labeled label.
func (b *Builder) Break(label string) { b.add(&Break{Label: label}) }

// Continue appends a continue of the loop labeled label.
func (b *Builder) Continue(label string) { b.add(&Continue{Label: label}) }

// Return appends a return.
func (b *Builder) Return(values ...Expr) { b.add(&Return{Values: values}) }

// Assert appends an assertion of a conformance case.
func (b *Builder) Assert(cond Expr, msg string) { b.add(&Assert{Cond: cond, Msg: msg}) }

// Expression builders.

// Int returns an int constant.
func Int(v int64) Expr { return &Const{Type: TInt, Value: v} }

// U64 returns a u64 constant.
func U64(v uint64) Expr { return &Const{Type: TU64, Value: v} }

// U32 returns a u32 constant.
func U32(v uint32) Expr { return &Const{Type: TU32, Value: v} }

// I32 returns an i32 constant.
func I32(v int32) Expr { return &Const{Type: TI32, Value: v} }

// Null returns the null handle of the handle type t.
func Null(t Type) Expr { return &Const{Type: t} }

// BoolC returns a bool constant.
func BoolC(v bool) Expr { return &Const{Type: TBool, Value: v} }

// L reads a local.
func L(name string) Expr { return &Local{Name: name} }

// R reads a register.
func R(name string) Expr { return &Reg{Name: name} }

// F reads a feature constant.
func F(name string) Expr { return &Feature{Name: name} }

// C calls an operation or a function as an expression.
func C(callee string, args ...Expr) *Call { return &Call{Callee: callee, Args: args} }

// CI calls an operation with the immediate operand imm.
func CI(callee string, imm any, args ...Expr) *Call {
	return &Call{Callee: callee, Args: args, Imm: imm}
}

// Not negates a bool.
func Not(x Expr) Expr { return &Unary{Op: "!", X: x} }

// Bin applies a binary operator.
func Bin(op string, x, y Expr) Expr { return &Binary{Op: op, X: x, Y: y} }

// Eq, Lt, Add and And are shorthands of Bin.
func Eq(x, y Expr) Expr  { return Bin("==", x, y) }
func Lt(x, y Expr) Expr  { return Bin("<", x, y) }
func Add(x, y Expr) Expr { return Bin("+", x, y) }
func And(x, y Expr) Expr { return Bin("&&", x, y) }

// Conv converts an integer to type t.
func Conv(t Type, x Expr) Expr { return &Convert{To: t, X: x} }
