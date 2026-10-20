package compiler

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"gosm/internal/wasm"
)

// Stack entry kinds.
const (
	kConst = iota // constant literal (numeric or typed null)
	kLocal        // a local variable reference
	kSlot         // a stack slot variable at the entry's own height
	kExpr         // a pure (side-effect-free, non-trapping) lazy expression
)

type entry struct {
	t      wasm.ValType
	expr   string
	kind   int
	locals []int    // locals referenced by the expression
	slots  []string // slot variables referenced
	maxH   int      // highest slot height referenced (-1 if none)
	bits   uint64   // constant value (numeric constants)
	isNum  bool     // numeric constant with valid bits
}

const (
	ctrlFunc = iota
	ctrlBlock
	ctrlLoop
	ctrlIf
	ctrlTry    // try_table
	ctrlLegacy // legacy try
	ctrlTail   // pseudo target: tail call performed at function level
)

type exitTarget struct {
	target *ctrl
	code   int
}

type ctrl struct {
	kind        int
	params      []wasm.ValType
	results     []wasm.ValType
	base        int // stack height below the block's params
	label       string
	used        bool
	unreachable bool
	endReached  bool
	closure     int // closure level where this construct's label lives
	parentW     *strings.Builder
	paramEnts   []entry
	elseSeen    bool
	// try_table
	catches []wasm.Catch
	exits   []exitTarget
	// legacy try
	legacy *legacyTry
	// tail call pseudo target
	tailCall string
	// exception handler (try_table / legacy try) is targeted
	handlerUsed bool
}

func (t *ctrl) labelTypes() []wasm.ValType {
	if t.kind == ctrlLoop {
		return t.params
	}
	return t.results
}

type slotKey struct {
	h  int
	gt string
}

type fn struct {
	c       *compiler
	name    string
	idx     int // function index, -1 for synthetic functions
	params  []wasm.ValType
	results []wasm.ValType
	locals  []wasm.ValType
	lname   []string
	stack   []entry
	ctrls   []*ctrl
	w       *strings.Builder

	slots    map[slotKey]string
	perH     map[int]int
	decls    []string // declared variable names, in order
	declType map[string]string
	read     map[string]bool
	nlabel   int
	closure  int
	tryStack []*ctrl // enclosing try_table ctrls (closures), innermost last

	retVars    []string
	startLabel string
	startUsed  bool
	skip       int

	needDelegateDefer bool
	ntail             int
	nguard            int
	dropped           map[string]bool
}

func (f *fn) emit(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if lhs, rhs, ok := strings.Cut(line, " = "); ok && lhs == rhs {
		return // self-assignment
	}
	if lhs, rhs, ok := strings.Cut(line, " = "); ok && strings.Contains(lhs, ", ") && lhs == rhs {
		return
	}
	f.w.WriteString(line)
	f.w.WriteByte('\n')
}

func (f *fn) newLabel(prefix string) string {
	f.nlabel++
	return fmt.Sprintf("%s%d", prefix, f.nlabel)
}

func (f *fn) declare(name, gt string) {
	if _, ok := f.declType[name]; ok {
		return
	}
	f.declType[name] = gt
	f.decls = append(f.decls, name)
}

// slot returns the variable used for stack height h holding Go type gt.
func (f *fn) slot(h int, gt string) string {
	k := slotKey{h, gt}
	if s, ok := f.slots[k]; ok {
		return s
	}
	n := f.perH[h]
	f.perH[h] = n + 1
	var s string
	if n == 0 {
		s = fmt.Sprintf("s%d", h)
	} else {
		s = fmt.Sprintf("s%d_%d", h, n)
	}
	f.slots[k] = s
	f.declare(s, gt)
	return s
}

// ex returns the expression of e, marking referenced variables as read.
func (f *fn) ex(e entry) string {
	return e.expr
}

func (f *fn) height() int { return len(f.stack) }

func (f *fn) push(e entry) { f.stack = append(f.stack, e) }

func (f *fn) pop() entry {
	if len(f.stack) == 0 {
		panic(fmt.Sprintf("%s: stack underflow", f.name))
	}
	e := f.stack[len(f.stack)-1]
	f.stack = f.stack[:len(f.stack)-1]
	return e
}

// popN pops n entries, returning them in stack order.
func (f *fn) popN(n int) []entry {
	out := make([]entry, n)
	for i := n - 1; i >= 0; i-- {
		out[i] = f.pop()
	}
	return out
}

// popAs pops a value and converts it to the Go representation of t.
func (f *fn) popAs(t wasm.ValType) string {
	e := f.pop()
	return f.c.conv(f.ex(e), e.t, t)
}

func (f *fn) pushSlotVar(t wasm.ValType, name string) {
	h := f.height()
	f.push(entry{t: t, expr: name, kind: kSlot, slots: []string{name}, maxH: h})
}

// pushEager evaluates expr now into the slot for the new top of stack.
func (f *fn) pushEager(t wasm.ValType, expr string) {
	h := f.height()
	s := f.slot(h, f.c.goType(t))
	f.emit("%s = %s", s, expr)
	f.pushSlotVar(t, s)
}

// pushPure pushes a lazy pure expression built from operands.
func (f *fn) pushPure(t wasm.ValType, expr string, ops ...entry) {
	h := f.height()
	e := entry{t: t, expr: expr, kind: kExpr, maxH: -1}
	for _, o := range ops {
		e.locals = append(e.locals, o.locals...)
		e.slots = append(e.slots, o.slots...)
		if o.maxH > e.maxH {
			e.maxH = o.maxH
		}
	}
	f.push(e)
	if e.maxH > h || len(expr) > 400 {
		f.materialize(h)
	}
}

func (f *fn) pushConst(t wasm.ValType, expr string, bits uint64) {
	f.push(entry{t: t, expr: expr, kind: kConst, maxH: -1, bits: bits, isNum: true})
}

func (f *fn) pushNull(t wasm.ValType) {
	f.push(entry{t: t, expr: f.c.nullOf(t), kind: kConst, maxH: -1})
}

// materialize stores stack entry i into its slot variable.
func (f *fn) materialize(i int) {
	e := f.stack[i]
	s := f.slot(i, f.c.goType(e.t))
	if e.kind == kSlot && e.expr == s {
		return
	}
	f.emit("%s = %s", s, f.ex(e))
	f.stack[i] = entry{t: e.t, expr: s, kind: kSlot, slots: []string{s}, maxH: i}
}

// spillAll materializes every non-constant entry (used at block boundaries).
func (f *fn) spillAll(includeConsts bool) {
	for i := range f.stack {
		e := f.stack[i]
		if e.kind == kExpr || e.kind == kLocal || (includeConsts && e.kind == kConst) {
			f.materialize(i)
		}
	}
}

// spillLocal materializes entries referencing local l (before it changes).
func (f *fn) spillLocal(l int) {
	for i := range f.stack {
		for _, x := range f.stack[i].locals {
			if x == l {
				f.materialize(i)
				break
			}
		}
	}
}

// constOperand spills the first operand if all operands are constants, so
// that generated Go expressions never become constant expressions (which
// would be folded, and rejected on overflow, by the Go compiler).
func (f *fn) constGuard(n int) {
	top := len(f.stack)
	all := true
	for i := top - n; i < top; i++ {
		if f.stack[i].kind != kConst {
			all = false
		}
	}
	if all && n > 0 {
		f.materialize(top - n)
	}
}

// Typed conversions of operands that are safe for constants.

func (f *fn) u32(e entry) string {
	if e.kind == kConst && e.isNum {
		return fmt.Sprintf("uint32(%d)", uint32(e.bits))
	}
	return "uint32(" + f.ex(e) + ")"
}

func (f *fn) u64(e entry) string {
	if e.kind == kConst && e.isNum {
		if e.t.Kind == wasm.I32 {
			return fmt.Sprintf("uint64(%d)", uint64(uint32(e.bits)))
		}
		return fmt.Sprintf("uint64(%d)", e.bits)
	}
	if e.t.Kind == wasm.I32 {
		return "uint64(uint32(" + f.ex(e) + "))"
	}
	return "uint64(" + f.ex(e) + ")"
}

func (f *fn) u8(e entry) string {
	if e.kind == kConst && e.isNum {
		return fmt.Sprintf("uint8(%d)", uint8(e.bits))
	}
	return "uint8(" + f.ex(e) + ")"
}

func (f *fn) u16(e entry) string {
	if e.kind == kConst && e.isNum {
		return fmt.Sprintf("uint16(%d)", uint16(e.bits))
	}
	return "uint16(" + f.ex(e) + ")"
}

func (f *fn) u32of64(e entry) string {
	if e.kind == kConst && e.isNum {
		return fmt.Sprintf("uint32(%d)", uint32(e.bits))
	}
	return "uint32(" + f.ex(e) + ")"
}

// i32 returns an int32-typed expression (for int index arguments).
func (f *fn) asInt(e entry) string {
	return "int(" + f.ex(e) + ")"
}

func i32Lit(v int32) string { return fmt.Sprintf("int32(%d)", v) }
func i64Lit(v int64) string { return fmt.Sprintf("int64(%d)", v) }

func f32Lit(bits uint32) string {
	v := math.Float32frombits(bits)
	if math.IsInf(float64(v), 0) || v != v || (v == 0 && bits != 0) {
		return fmt.Sprintf("math.Float32frombits(0x%08x)", bits)
	}
	return "float32(" + strconv.FormatFloat(float64(v), 'g', -1, 32) + ")"
}

func f64Lit(bits uint64) string {
	v := math.Float64frombits(bits)
	if math.IsInf(v, 0) || v != v || (v == 0 && bits != 0) {
		return fmt.Sprintf("math.Float64frombits(0x%016x)", bits)
	}
	return "float64(" + strconv.FormatFloat(v, 'g', -1, 64) + ")"
}

// cond turns an i32 expression into a Go boolean condition.
func cond(e string) string {
	if strings.HasPrefix(e, "rt.B2I(") && strings.HasSuffix(e, ")") {
		inner := e[len("rt.B2I(") : len(e)-1]
		if balanced(inner) {
			return inner
		}
	}
	return e + " != 0"
}

func negCond(e string) string {
	if strings.HasPrefix(e, "rt.B2I(") && strings.HasSuffix(e, ")") {
		inner := e[len("rt.B2I(") : len(e)-1]
		if balanced(inner) {
			return "!(" + inner + ")"
		}
	}
	return e + " == 0"
}

func balanced(s string) bool {
	d := 0
	for _, c := range s {
		switch c {
		case '(':
			d++
		case ')':
			d--
			if d < 0 {
				return false
			}
		}
	}
	return d == 0
}

func (f *fn) blockSig(bt wasm.BlockType) (params, results []wasm.ValType) {
	switch bt.Kind {
	case 1:
		return nil, []wasm.ValType{bt.Val}
	case 2:
		ft := &f.c.m.Types[bt.Idx].Comp.Func
		return ft.Params, ft.Results
	}
	return nil, nil
}

func (f *fn) top() *ctrl { return f.ctrls[len(f.ctrls)-1] }

// assignTo moves the given entries into the slots for heights base.. with
// the label types ts.
func (f *fn) assignTo(base int, ts []wasm.ValType, vals []entry) {
	for i, t := range ts {
		s := f.slot(base+i, f.c.goType(t))
		e := vals[i]
		if e.kind == kSlot && e.expr == s {
			continue
		}
		f.emit("%s = %s", s, f.c.conv(f.ex(e), e.t, t))
	}
}

// branch emits a branch to the label at relative depth, using the top
// stack values (which are not popped).
func (f *fn) branch(depth uint32) {
	t := f.ctrls[len(f.ctrls)-1-int(depth)]
	ts := t.labelTypes()
	vals := f.stack[len(f.stack)-len(ts):]
	if t.kind == ctrlFunc {
		if f.closure == 0 {
			var rs []string
			for i, r := range ts {
				rs = append(rs, f.c.conv(f.ex(vals[i]), vals[i].t, r))
			}
			if len(rs) == 0 {
				f.emit("return")
			} else {
				f.emit("return %s", strings.Join(rs, ", "))
			}
			return
		}
		f.ensureRetVars()
		for i, r := range ts {
			f.emit("%s = %s", f.retVars[i], f.c.conv(f.ex(vals[i]), vals[i].t, r))
		}
		f.jump(t)
		return
	}
	if t.kind != ctrlLoop {
		t.endReached = true
	}
	f.assignTo(t.base, ts, vals)
	f.jump(t)
}

func (f *fn) ensureRetVars() {
	if f.retVars != nil || len(f.results) == 0 {
		return
	}
	for i, r := range f.results {
		n := fmt.Sprintf("ret%d", i)
		f.declare(n, f.c.goType(r))
		f.read[n] = true
		f.retVars = append(f.retVars, n)
	}
}

// jump transfers control to t's label (values already in place).
func (f *fn) jump(t *ctrl) {
	if t.kind == ctrlTail && f.closure == 0 {
		f.emit("%s", t.tailCall)
		return
	}
	if t.kind == ctrlFunc && f.closure == 0 {
		if len(f.results) == 0 {
			f.emit("return")
		} else {
			f.emit("return %s", strings.Join(f.retVars, ", "))
		}
		return
	}
	if t.kind != ctrlFunc && t.closure == f.closure {
		t.used = true
		if t.kind == ctrlLoop {
			f.emit("continue %s", t.label)
		} else {
			f.emit("goto %s", t.label)
		}
		return
	}
	// leave the current closure with an exit code
	tc := f.tryStack[len(f.tryStack)-1]
	for _, x := range tc.exits {
		if x.target == t {
			f.emit("return %d", x.code)
			return
		}
	}
	code := len(tc.exits) + 1
	tc.exits = append(tc.exits, exitTarget{t, code})
	f.emit("return %d", code)
}

func (f *fn) setUnreachable() {
	t := f.top()
	t.unreachable = true
	f.stack = f.stack[:t.base]
}

// compileBody compiles a function body (or constant expression) into f.w.
func (f *fn) compileBody(body []wasm.Instr) {
	fc := &ctrl{kind: ctrlFunc, results: f.results, base: 0}
	f.ctrls = []*ctrl{fc}
	for pc := 0; pc < len(body); pc++ {
		in := &body[pc]
		if f.top().unreachable {
			if f.skipDead(in) {
				continue
			}
		}
		f.instr(in)
		if len(f.ctrls) == 0 {
			return
		}
	}
}

// skipDead handles an instruction in unreachable code. It returns true if
// the instruction was consumed.
func (f *fn) skipDead(in *wasm.Instr) bool {
	switch in.Op {
	case wasm.OpBlock, wasm.OpLoop, wasm.OpIf, wasm.OpTry, wasm.OpTryTable:
		f.skip++
		return true
	case wasm.OpEnd:
		if f.skip > 0 {
			f.skip--
			return true
		}
		return false
	case wasm.OpDelegate:
		if f.skip > 0 {
			f.skip--
			return true
		}
		return false
	case wasm.OpElse, wasm.OpCatch, wasm.OpCatchAll:
		return f.skip > 0
	}
	return true
}

func (f *fn) instr(in *wasm.Instr) {
	c := f.c
	switch in.Op {
	case wasm.OpUnreachable:
		f.emit("panic(rt.ErrUnreachable)")
		f.setUnreachable()
	case wasm.OpNop:
	case wasm.OpBlock, wasm.OpLoop, wasm.OpIf:
		f.beginBlock(in)
	case wasm.OpElse:
		f.elseBlock()
	case wasm.OpEnd:
		f.endBlock()
	case wasm.OpTryTable:
		f.beginTryTable(in)
	case wasm.OpTry:
		f.beginLegacyTry(in)
	case wasm.OpCatch, wasm.OpCatchAll:
		f.legacyCatch(in)
	case wasm.OpDelegate:
		f.legacyDelegate(in)
	case wasm.OpRethrow:
		f.legacyRethrow(in)
	case wasm.OpThrow:
		f.throw(in.A)
	case wasm.OpThrowRef:
		f.throwRef()
	case wasm.OpBr:
		f.branch(in.A)
		f.setUnreachable()
	case wasm.OpBrIf:
		cv := f.pop()
		f.emit("if %s {", cond(f.ex(cv)))
		f.branch(in.A)
		f.emit("}")
	case wasm.OpBrTable:
		f.brTable(in)
	case wasm.OpReturn:
		f.branch(uint32(len(f.ctrls) - 1))
		f.setUnreachable()
	case wasm.OpCall:
		f.call(in.A, false)
	case wasm.OpReturnCall:
		f.call(in.A, true)
	case wasm.OpCallIndirect:
		f.callIndirect(in.A, in.B, false)
	case wasm.OpReturnCallIndirect:
		f.callIndirect(in.A, in.B, true)
	case wasm.OpCallRef:
		f.callRef(in.A, false)
	case wasm.OpReturnCallRef:
		f.callRef(in.A, true)
	case wasm.OpDrop:
		f.pop()
	case wasm.OpSelect, wasm.OpSelectT:
		cv := f.pop()
		b := f.pop()
		a := f.pop()
		t := a.t
		if len(in.Types) > 0 {
			t = in.Types[0]
		}
		h := f.height()
		s := f.slot(h, c.goType(t))
		f.emit("if %s {", cond(f.ex(cv)))
		if !(a.kind == kSlot && a.expr == s) {
			f.emit("%s = %s", s, c.conv(f.ex(a), a.t, t))
		}
		f.emit("} else {")
		f.emit("%s = %s", s, c.conv(f.ex(b), b.t, t))
		f.emit("}")
		f.pushSlotVar(t, s)
	case wasm.OpLocalGet:
		l := int(in.A)
		n := f.lname[l]
		f.push(entry{t: f.locals[l], expr: n, kind: kLocal, locals: []int{l}, maxH: -1})
	case wasm.OpLocalSet, wasm.OpLocalTee:
		l := int(in.A)
		e := f.pop()
		f.spillLocal(l)
		f.emit("%s = %s", f.lname[l], c.conv(f.ex(e), e.t, f.locals[l]))
		if in.Op == wasm.OpLocalTee {
			f.push(entry{t: f.locals[l], expr: f.lname[l], kind: kLocal, locals: []int{l}, maxH: -1})
		}
	case wasm.OpGlobalGet:
		gt := c.m.GlobalType(in.A)
		if gt.Mutable {
			f.pushEager(gt.Type, c.globalRef(in.A))
		} else {
			f.pushPure(gt.Type, c.globalRef(in.A))
		}
	case wasm.OpGlobalSet:
		gt := c.m.GlobalType(in.A)
		f.emit("%s = %s", c.globalRef(in.A), f.popAs(gt.Type))
	case wasm.OpI32Const:
		f.pushConst(wasm.TI32, i32Lit(int32(in.I64)), uint64(uint32(in.I64)))
	case wasm.OpI64Const:
		f.pushConst(wasm.TI64, i64Lit(in.I64), uint64(in.I64))
	case wasm.OpF32Const:
		f.pushConst(wasm.TF32, f32Lit(uint32(in.I64)), uint64(uint32(in.I64)))
	case wasm.OpF64Const:
		f.pushConst(wasm.TF64, f64Lit(uint64(in.I64)), uint64(in.I64))
	default:
		if f.numeric(in) || f.memoryOp(in) || f.refOp(in) || f.gcOp(in) || f.tableOp(in) {
			return
		}
		if in.Op.Prefix() == wasm.PrefixSIMD {
			f.simdOp(in)
			return
		}
		panic(fmt.Sprintf("unsupported instruction 0x%x", uint32(in.Op)))
	}
}

func (f *fn) beginBlock(in *wasm.Instr) {
	params, results := f.blockSig(in.Block)
	var cv entry
	if in.Op == wasm.OpIf {
		cv = f.pop()
	}
	f.spillAll(false)
	t := &ctrl{
		params:  params,
		results: results,
		base:    f.height() - len(params),
		closure: f.closure,
	}
	// params live in slots of their declared types
	for i, p := range params {
		h := t.base + i
		e := f.stack[h]
		s := f.slot(h, f.c.goType(p))
		if !(e.kind == kSlot && e.expr == s) {
			f.emit("%s = %s", s, f.c.conv(f.ex(e), e.t, p))
			f.stack[h] = entry{t: p, expr: s, kind: kSlot, slots: []string{s}, maxH: h}
		}
	}
	switch in.Op {
	case wasm.OpBlock:
		t.kind = ctrlBlock
		t.label = f.newLabel("B")
	case wasm.OpLoop:
		t.kind = ctrlLoop
		t.label = f.newLabel("L")
		t.parentW = f.w
		f.w = &strings.Builder{}
	case wasm.OpIf:
		t.kind = ctrlIf
		t.label = f.newLabel("B")
		t.paramEnts = append([]entry(nil), f.stack[t.base:]...)
		f.emit("if %s {", cond(f.ex(cv)))
	}
	f.ctrls = append(f.ctrls, t)
}

// fallthroughEnd moves the block results into their slots on normal exit.
func (f *fn) fallthroughEnd(t *ctrl) {
	if t.unreachable {
		return
	}
	vals := f.stack[len(f.stack)-len(t.results):]
	f.assignTo(t.base, t.results, vals)
	t.endReached = true
}

func (f *fn) elseBlock() {
	t := f.top()
	if t.kind == ctrlLegacy {
		panic("else in try")
	}
	f.fallthroughEnd(t)
	t.elseSeen = true
	f.emit("} else {")
	f.stack = append(f.stack[:t.base], t.paramEnts...)
	t.unreachable = false
}

func (f *fn) endBlock() {
	t := f.top()
	switch t.kind {
	case ctrlFunc:
		if !t.unreachable {
			f.branch(0)
		} else if len(f.results) > 0 {
			f.emit("panic(rt.ErrUnreachable)")
		}
		f.ctrls = nil
		return
	case ctrlTry:
		f.endTryTable(t)
		return
	case ctrlLegacy:
		f.endLegacyTry(t)
		return
	}
	f.ctrls = f.ctrls[:len(f.ctrls)-1]
	switch t.kind {
	case ctrlBlock:
		f.fallthroughEnd(t)
		if t.used {
			f.emit("%s:", t.label)
		}
	case ctrlIf:
		f.fallthroughEnd(t)
		if !t.elseSeen {
			// implicit else passes params through as results (same slots)
			t.endReached = true
		}
		f.emit("}")
		if t.used {
			f.emit("%s:", t.label)
		}
	case ctrlLoop:
		if !t.unreachable {
			vals := f.stack[len(f.stack)-len(t.results):]
			f.assignTo(t.base, t.results, vals)
			t.endReached = true
			t.used = true
			f.emit("break %s", t.label)
		}
		body := f.w.String()
		f.w = t.parentW
		if t.used {
			f.emit("%s:", t.label)
		}
		f.emit("for {")
		f.w.WriteString(body)
		f.emit("}")
	}
	f.finishBlock(t)
}

// finishBlock restores the stack after a block ends.
func (f *fn) finishBlock(t *ctrl) {
	f.stack = f.stack[:t.base]
	if !t.endReached {
		f.top().unreachable = true
		return
	}
	for i, r := range t.results {
		s := f.slot(t.base+i, f.c.goType(r))
		f.pushSlotVar(r, s)
	}
}

func (f *fn) brTable(in *wasm.Instr) {
	idx := f.pop()
	targets := map[uint32][]int{}
	var order []uint32
	def := in.Labels[len(in.Labels)-1]
	for i, l := range in.Labels[:len(in.Labels)-1] {
		if l == def {
			continue
		}
		if _, ok := targets[l]; !ok {
			order = append(order, l)
		}
		targets[l] = append(targets[l], i)
	}
	if len(order) == 0 {
		f.branch(def)
		f.setUnreachable()
		return
	}
	f.emit("switch %s {", f.ex(idx))
	for _, l := range order {
		var cs []string
		for _, i := range targets[l] {
			cs = append(cs, strconv.Itoa(i))
		}
		f.emit("case %s:", strings.Join(cs, ", "))
		f.branch(l)
	}
	f.emit("default:")
	f.branch(def)
	f.emit("}")
	f.setUnreachable()
}

// finish assembles the function's declarations and body.
func (f *fn) finish(header string, body string) string {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString(" {\n")
	if f.idx >= 0 && f.c.recursive[f.idx] {
		b.WriteString("if d > rt.MaxCallDepth {\nrt.Exhausted()\n}\n")
	}
	if f.needDelegateDefer {
		b.WriteString("defer rt.UndelegateFrame()\n")
	}
	body = pruneDead(body)
	body = f.peephole(body)
	f.dropUnmentioned(body)
	// group declarations by type for readability
	byType := map[string][]string{}
	var types []string
	for _, n := range f.decls {
		t := f.declType[n]
		if _, ok := byType[t]; !ok {
			types = append(types, t)
		}
		byType[t] = append(byType[t], n)
	}
	for _, t := range types {
		var names []string
		for _, n := range byType[t] {
			if !f.dropped[n] {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, "var %s %s\n", strings.Join(names, ", "), t)
		}
	}
	scanReads(body, f.read)
	var unused []string
	for _, n := range f.decls {
		if !f.read[n] && !f.dropped[n] {
			unused = append(unused, n)
		}
	}
	if len(unused) > 0 {
		for _, n := range unused {
			fmt.Fprintf(&b, "_ = %s\n", n)
		}
	}
	if f.startUsed {
		fmt.Fprintf(&b, "%s:\n", f.startLabel)
	}
	b.WriteString(body)
	b.WriteString("}\n\n")
	return b.String()
}

var assignLHS = regexp.MustCompile(`^\s*([A-Za-z_]\w*(?:, [A-Za-z_]\w*)*) = `)

// scanReads records every identifier read (i.e. not merely assigned) in
// the generated code.
func scanReads(body string, read map[string]bool) {
	for _, line := range strings.Split(body, "\n") {
		if m := assignLHS.FindStringSubmatchIndex(line); m != nil {
			line = line[m[1]:]
		}
		for i := 0; i < len(line); {
			ch := line[i]
			if ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
				j := i + 1
				for j < len(line) && (line[j] == '_' || line[j] >= 'a' && line[j] <= 'z' || line[j] >= 'A' && line[j] <= 'Z' || line[j] >= '0' && line[j] <= '9') {
					j++
				}
				if i == 0 || line[i-1] != '.' {
					read[line[i:j]] = true
				}
				i = j
				continue
			}
			if ch == '"' {
				// skip string literal
				j := i + 1
				for j < len(line) && line[j] != '"' {
					if line[j] == '\\' {
						j++
					}
					j++
				}
				i = j + 1
				continue
			}
			i++
		}
	}
}

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// mentions counts identifier occurrences (not after '.') in code.
func mentions(code string) map[string]int {
	n := map[string]int{}
	for _, loc := range identRe.FindAllStringIndex(code, -1) {
		if loc[0] > 0 && code[loc[0]-1] == '.' {
			continue
		}
		n[code[loc[0]:loc[1]]]++
	}
	return n
}

var slotAssign = regexp.MustCompile(`^(s\d+(?:_\d+)?) = (.*)$`)
var useLine = regexp.MustCompile(`^(return |[A-Za-z_][A-Za-z0-9_]* = )(s\d+(?:_\d+)?)$`)

// peephole folds "sN = expr" followed by "return sN" / "x = sN" into a
// single statement when sN is used nowhere else.
func (f *fn) peephole(body string) string {
	lines := strings.Split(body, "\n")
	cnt := mentions(body)
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if i+1 < len(lines) {
			if m := slotAssign.FindStringSubmatch(lines[i]); m != nil {
				if u := useLine.FindStringSubmatch(lines[i+1]); u != nil && u[2] == m[1] && cnt[m[1]] == 2 {
					out = append(out, u[1]+m[2])
					i++
					continue
				}
			}
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

// dropUnmentioned marks declared variables that no longer appear at all.
func (f *fn) dropUnmentioned(body string) {
	cnt := mentions(body)
	f.dropped = map[string]bool{}
	for _, n := range f.decls {
		if cnt[n] == 0 && !f.isLocal(n) {
			f.dropped[n] = true
		}
	}
}

func (f *fn) isLocal(n string) bool {
	for _, l := range f.lname {
		if l == n {
			return true
		}
	}
	return false
}
