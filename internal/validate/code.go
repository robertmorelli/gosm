package validate

import (
	"gosm/internal/wasm"
)

type ctrlFrame struct {
	op          wasm.Op
	start, end  []wasm.ValType
	height      int
	initHeight  int
	unreachable bool
}

type fctx struct {
	c         *checker
	vals      []wasm.ValType
	ctrls     []ctrlFrame
	locals    []wasm.ValType
	inited    []bool
	initStack []int
	results   []wasm.ValType

	isConst  bool
	nglobals int // globals visible to a constant expression
}

var (
	tRefNullFunc   = wasm.RefType(wasm.HeapFunc, true)
	tRefNullExn    = wasm.RefType(wasm.HeapExn, true)
	tRefExn        = wasm.RefType(wasm.HeapExn, false)
	tRefNullEq     = wasm.RefType(wasm.HeapEq, true)
	tRefNullArray  = wasm.RefType(wasm.HeapArray, true)
	tRefNullI31    = wasm.RefType(wasm.HeapI31, true)
	tRefNullExtern = wasm.RefType(wasm.HeapExtern, true)
	tRefNullAny    = wasm.RefType(wasm.HeapAny, true)
)

func (f *fctx) push(t wasm.ValType) { f.vals = append(f.vals, t) }

func (f *fctx) pushAll(ts []wasm.ValType) {
	f.vals = append(f.vals, ts...)
}

func (f *fctx) top() *ctrlFrame { return &f.ctrls[len(f.ctrls)-1] }

func (f *fctx) pop() wasm.ValType {
	fr := f.top()
	if len(f.vals) == fr.height {
		if fr.unreachable {
			return bot
		}
		fail("type mismatch: instruction requires a value but stack is empty")
	}
	t := f.vals[len(f.vals)-1]
	f.vals = f.vals[:len(f.vals)-1]
	return t
}

func (f *fctx) popExpect(want wasm.ValType) wasm.ValType {
	got := f.pop()
	if !f.c.matchVal(got, want) {
		fail("type mismatch: instruction requires [%s] but stack has [%s]", want, typeName(got))
	}
	if isBot(got) {
		return want
	}
	return got
}

func typeName(t wasm.ValType) string {
	if isBot(t) {
		return "bot"
	}
	if t.Kind == wasm.Ref && t.Heap == heapBot {
		return "(ref bot)"
	}
	return t.String()
}

// popVals pops ts (last first) and returns the actual popped types.
func (f *fctx) popVals(ts []wasm.ValType) []wasm.ValType {
	out := make([]wasm.ValType, len(ts))
	for i := len(ts) - 1; i >= 0; i-- {
		got := f.pop()
		if !f.c.matchVal(got, ts[i]) {
			fail("type mismatch: instruction requires %s but stack has %s at position %d", typesString(ts), typeName(got), i)
		}
		out[i] = got
	}
	return out
}

// popRef pops a reference of any type; a polymorphic stack yields (ref bot).
func (f *fctx) popRef() wasm.ValType {
	t := f.pop()
	if isBot(t) {
		return wasm.RefType(heapBot, false)
	}
	if t.Kind != wasm.Ref {
		fail("type mismatch: instruction requires reference type but stack has %s", t)
	}
	return t
}

func (f *fctx) pushCtrl(op wasm.Op, start, end []wasm.ValType) {
	f.ctrls = append(f.ctrls, ctrlFrame{op: op, start: start, end: end, height: len(f.vals), initHeight: len(f.initStack)})
	f.pushAll(start)
}

func (f *fctx) resetInits(h int) {
	for len(f.initStack) > h {
		f.inited[f.initStack[len(f.initStack)-1]] = false
		f.initStack = f.initStack[:len(f.initStack)-1]
	}
}

func (f *fctx) popCtrl() ctrlFrame {
	if len(f.ctrls) == 0 {
		fail("unexpected end")
	}
	fr := *f.top()
	f.popVals(fr.end)
	if len(f.vals) != fr.height {
		fail("type mismatch: block requires %s but stack has %d extra values", typesString(fr.end), len(f.vals)-fr.height)
	}
	f.resetInits(fr.initHeight)
	f.ctrls = f.ctrls[:len(f.ctrls)-1]
	return fr
}

func (f *fctx) setUnreachable() {
	fr := f.top()
	f.vals = f.vals[:fr.height]
	fr.unreachable = true
}

func labelTypes(fr *ctrlFrame) []wasm.ValType {
	if fr.op == wasm.OpLoop {
		return fr.start
	}
	return fr.end
}

func (f *fctx) label(l uint32) *ctrlFrame {
	if int(l) >= len(f.ctrls) {
		fail("unknown label %d", l)
	}
	return &f.ctrls[len(f.ctrls)-1-int(l)]
}

func (f *fctx) blockType(bt wasm.BlockType) (params, results []wasm.ValType) {
	switch bt.Kind {
	case 0:
		return nil, nil
	case 1:
		f.c.checkVal(bt.Val)
		return nil, []wasm.ValType{bt.Val}
	}
	ft := f.c.funcType(bt.Idx)
	return ft.Params, ft.Results
}

func (f *fctx) local(x uint32) wasm.ValType {
	if int(x) >= len(f.locals) {
		fail("unknown local %d", x)
	}
	return f.locals[x]
}

func (f *fctx) global(x uint32) wasm.GlobalType {
	n := len(f.c.globals)
	if f.isConst {
		n = f.nglobals
	}
	if int(x) >= n {
		fail("unknown global %d", x)
	}
	return f.c.globals[x]
}

func (f *fctx) elemType(x uint32) wasm.ValType {
	if int(x) >= len(f.c.m.Elems) {
		fail("unknown elem segment %d", x)
	}
	return f.c.m.Elems[x].Type
}

func (f *fctx) data(x uint32) {
	if f.c.m.DataCount == nil {
		fail("data count section required")
	}
	if int(x) >= int(*f.c.m.DataCount) {
		fail("unknown data segment %d", x)
	}
}

func (f *fctx) memArg(in *wasm.Instr, maxAlign uint32) wasm.ValType {
	mt := f.c.mem(in.B)
	if in.Align > maxAlign {
		fail("alignment must not be larger than natural")
	}
	if !mt.Limits.Is64 && in.Offset > 0xFFFFFFFF {
		fail("offset out of range")
	}
	return addrType(mt.Limits)
}

func sigType(c byte) wasm.ValType {
	switch c {
	case 'i':
		return wasm.TI32
	case 'l':
		return wasm.TI64
	case 'f':
		return wasm.TF32
	case 'd':
		return wasm.TF64
	case 'v':
		return wasm.TV128
	}
	panic("bad sig char")
}

// applySig pops operands and pushes results described by sig ("ab:c").
// 'm' stands for addr (memory address type).
func (f *fctx) applySig(sig string, addr wasm.ValType) {
	colon := 0
	for colon < len(sig) && sig[colon] != ':' {
		colon++
	}
	for i := colon - 1; i >= 0; i-- {
		if sig[i] == 'm' {
			f.popExpect(addr)
		} else {
			f.popExpect(sigType(sig[i]))
		}
	}
	for i := colon + 1; i < len(sig); i++ {
		f.push(sigType(sig[i]))
	}
}

func (f *fctx) checkLabelMatch(ts []wasm.ValType, l uint32, what string) {
	lt := labelTypes(f.label(l))
	if !f.c.matchVals(ts, lt) {
		fail("type mismatch: %s %s does not match label type %s", what, typesString(ts), typesString(lt))
	}
}

// funcBody validates function fidx's body.
func (c *checker) funcBody(fidx uint32, code *wasm.Code) {
	ft := c.funcTypeOf(fidx)
	f := &fctx{c: c, results: ft.Results}
	f.locals = append(append([]wasm.ValType{}, ft.Params...), code.Locals...)
	f.inited = make([]bool, len(f.locals))
	for i, t := range f.locals {
		if i < len(ft.Params) {
			f.inited[i] = true
		} else {
			c.checkVal(t)
			f.inited[i] = defaultable(t)
		}
	}
	f.run(code.Body, ft.Results)
}

// constExpr validates a constant expression of type want.
func (c *checker) constExpr(e []wasm.Instr, want wasm.ValType, nglobals int) {
	f := &fctx{c: c, isConst: true, nglobals: nglobals}
	f.run(e, []wasm.ValType{want})
}

func (f *fctx) run(body []wasm.Instr, results []wasm.ValType) {
	f.pushCtrl(wasm.OpBlock, nil, results)
	for i := range body {
		if len(f.ctrls) == 0 {
			fail("unexpected content after end of function")
		}
		f.instr(&body[i])
	}
	if len(f.ctrls) != 0 {
		fail("unexpected end of function")
	}
}

func isConstOp(op wasm.Op) bool {
	switch op {
	case wasm.OpI32Const, wasm.OpI64Const, wasm.OpF32Const, wasm.OpF64Const,
		wasm.OpRefNull, wasm.OpRefFunc, wasm.OpGlobalGet, wasm.OpEnd,
		wasm.OpI32Add, wasm.OpI32Sub, wasm.OpI32Mul,
		wasm.OpI64Add, wasm.OpI64Sub, wasm.OpI64Mul,
		wasm.SIMDOp(12):
		return true
	}
	switch op {
	case wasm.OpStructNew, wasm.OpStructNewDefault, wasm.OpArrayNew, wasm.OpArrayNewDefault,
		wasm.OpArrayNewFixed, wasm.OpRefI31, wasm.OpAnyConvertExtern, wasm.OpExternConvertAny:
		return true
	}
	return false
}

func (f *fctx) instr(in *wasm.Instr) {
	c := f.c
	if f.isConst && !isConstOp(in.Op) {
		fail("constant expression required")
	}
	switch in.Op {
	case wasm.OpUnreachable:
		f.setUnreachable()
	case wasm.OpNop:
	case wasm.OpBlock, wasm.OpLoop:
		p, r := f.blockType(in.Block)
		f.popVals(p)
		f.pushCtrl(in.Op, p, r)
	case wasm.OpIf:
		p, r := f.blockType(in.Block)
		f.popExpect(wasm.TI32)
		f.popVals(p)
		f.pushCtrl(in.Op, p, r)
	case wasm.OpElse:
		if f.top().op != wasm.OpIf {
			fail("else without matching if")
		}
		fr := f.popCtrl()
		f.pushCtrl(wasm.OpElse, fr.start, fr.end)
	case wasm.OpEnd:
		fr := *f.top()
		if fr.op == wasm.OpIf {
			// implicit empty else: params must flow to results
			f.popCtrl()
			f.pushCtrl(wasm.OpElse, fr.start, fr.end)
		}
		done := f.popCtrl()
		f.pushAll(done.end)
		if len(f.ctrls) == 0 {
			// end of function / expression
			f.vals = f.vals[:0]
		}
	case wasm.OpTry:
		p, r := f.blockType(in.Block)
		f.popVals(p)
		f.pushCtrl(in.Op, p, r)
	case wasm.OpCatch:
		op := f.top().op
		if op != wasm.OpTry && op != wasm.OpCatch {
			fail("catch without matching try")
		}
		fr := f.popCtrl()
		tt := c.tagType(in.A)
		f.pushCtrl(wasm.OpCatch, tt.Params, fr.end)
		f.top().start = nil
	case wasm.OpCatchAll:
		op := f.top().op
		if op != wasm.OpTry && op != wasm.OpCatch {
			fail("catch_all without matching try")
		}
		fr := f.popCtrl()
		f.pushCtrl(wasm.OpCatchAll, nil, fr.end)
	case wasm.OpDelegate:
		if f.top().op != wasm.OpTry {
			fail("delegate without matching try")
		}
		fr := f.popCtrl()
		f.label(in.A)
		f.pushAll(fr.end)
	case wasm.OpRethrow:
		fr := f.label(in.A)
		if fr.op != wasm.OpCatch && fr.op != wasm.OpCatchAll {
			fail("invalid rethrow label")
		}
		f.setUnreachable()
	case wasm.OpThrow:
		tt := c.tagType(in.A)
		f.popVals(tt.Params)
		f.setUnreachable()
	case wasm.OpThrowRef:
		f.popExpect(tRefNullExn)
		f.setUnreachable()
	case wasm.OpTryTable:
		p, r := f.blockType(in.Block)
		for _, cc := range in.Catch {
			var ts []wasm.ValType
			if cc.Kind == wasm.CatchPlain || cc.Kind == wasm.CatchRef {
				ts = append(ts, c.tagType(cc.Tag).Params...)
			}
			if cc.Kind == wasm.CatchRef || cc.Kind == wasm.CatchAllRef {
				ts = append(ts, tRefExn)
			}
			f.checkLabelMatch(ts, cc.Label, "catch")
		}
		f.popVals(p)
		f.pushCtrl(in.Op, p, r)
	case wasm.OpBr:
		f.popVals(labelTypes(f.label(in.A)))
		f.setUnreachable()
	case wasm.OpBrIf:
		lt := labelTypes(f.label(in.A))
		f.popExpect(wasm.TI32)
		f.popVals(lt)
		f.pushAll(lt)
	case wasm.OpBrTable:
		f.popExpect(wasm.TI32)
		def := in.Labels[len(in.Labels)-1]
		arity := len(labelTypes(f.label(def)))
		for _, l := range in.Labels[:len(in.Labels)-1] {
			lt := labelTypes(f.label(l))
			if len(lt) != arity {
				fail("type mismatch: br_table targets have inconsistent arity")
			}
			f.pushAll(f.popVals(lt))
		}
		f.popVals(labelTypes(f.label(def)))
		f.setUnreachable()
	case wasm.OpReturn:
		f.popVals(f.results)
		f.setUnreachable()
	case wasm.OpCall:
		ft := c.funcTypeOf(in.A)
		f.popVals(ft.Params)
		f.pushAll(ft.Results)
	case wasm.OpReturnCall:
		ft := c.funcTypeOf(in.A)
		f.checkReturnCall(ft)
	case wasm.OpCallIndirect, wasm.OpReturnCallIndirect:
		tt := c.table(in.B)
		if !c.matchVal(tt.Elem, tRefNullFunc) {
			fail("type mismatch: call_indirect table must have function element type")
		}
		ft := c.funcType(in.A)
		f.popExpect(addrType(tt.Limits))
		if in.Op == wasm.OpCallIndirect {
			f.popVals(ft.Params)
			f.pushAll(ft.Results)
		} else {
			f.checkReturnCall(ft)
		}
	case wasm.OpCallRef, wasm.OpReturnCallRef:
		ft := c.funcType(in.A)
		f.popExpect(wasm.RefType(wasm.HeapType(in.A), true))
		if in.Op == wasm.OpCallRef {
			f.popVals(ft.Params)
			f.pushAll(ft.Results)
		} else {
			f.checkReturnCall(ft)
		}
	case wasm.OpDrop:
		f.pop()
	case wasm.OpSelect:
		f.popExpect(wasm.TI32)
		t1 := f.pop()
		t2 := f.pop()
		if t1.Kind == wasm.Ref || t2.Kind == wasm.Ref {
			fail("type mismatch: select without type requires numeric or vector operands")
		}
		if t1 != t2 && !isBot(t1) && !isBot(t2) {
			fail("type mismatch: select operands %s and %s differ", t1, t2)
		}
		if isBot(t1) {
			f.push(t2)
		} else {
			f.push(t1)
		}
	case wasm.OpSelectT:
		if len(in.Types) != 1 {
			fail("invalid result arity")
		}
		t := in.Types[0]
		c.checkVal(t)
		f.popExpect(wasm.TI32)
		f.popExpect(t)
		f.popExpect(t)
		f.push(t)
	case wasm.OpLocalGet:
		t := f.local(in.A)
		if !f.inited[in.A] {
			fail("uninitialized local %d", in.A)
		}
		f.push(t)
	case wasm.OpLocalSet:
		t := f.local(in.A)
		f.popExpect(t)
		f.initLocal(in.A)
	case wasm.OpLocalTee:
		t := f.local(in.A)
		f.popExpect(t)
		f.initLocal(in.A)
		f.push(t)
	case wasm.OpGlobalGet:
		g := f.global(in.A)
		if f.isConst && g.Mutable {
			fail("constant expression required")
		}
		f.push(g.Type)
	case wasm.OpGlobalSet:
		g := f.global(in.A)
		if !g.Mutable {
			fail("immutable global %d", in.A)
		}
		f.popExpect(g.Type)
	case wasm.OpTableGet:
		tt := c.table(in.A)
		f.popExpect(addrType(tt.Limits))
		f.push(tt.Elem)
	case wasm.OpTableSet:
		tt := c.table(in.A)
		f.popExpect(tt.Elem)
		f.popExpect(addrType(tt.Limits))
	case wasm.OpMemorySize:
		mt := c.mem(in.A)
		f.push(addrType(mt.Limits))
	case wasm.OpMemoryGrow:
		at := addrType(c.mem(in.A).Limits)
		f.popExpect(at)
		f.push(at)
	case wasm.OpI32Const:
		f.push(wasm.TI32)
	case wasm.OpI64Const:
		f.push(wasm.TI64)
	case wasm.OpF32Const:
		f.push(wasm.TF32)
	case wasm.OpF64Const:
		f.push(wasm.TF64)
	case wasm.OpRefNull:
		c.checkHeap(in.HT)
		f.push(wasm.RefType(in.HT, true))
	case wasm.OpRefIsNull:
		f.popRef()
		f.push(wasm.TI32)
	case wasm.OpRefFunc:
		c.funcTypeOf(in.A)
		if !f.isConst && !c.refs[in.A] {
			fail("undeclared function reference %d", in.A)
		}
		f.push(wasm.RefType(wasm.HeapType(c.funcs[in.A]), false))
	case wasm.OpRefEq:
		f.popExpect(tRefNullEq)
		f.popExpect(tRefNullEq)
		f.push(wasm.TI32)
	case wasm.OpRefAsNonNull:
		rt := f.popRef()
		f.push(wasm.RefType(rt.Heap, false))
	case wasm.OpBrOnNull:
		lt := labelTypes(f.label(in.A))
		rt := f.popRef()
		f.popVals(lt)
		f.pushAll(lt)
		f.push(wasm.RefType(rt.Heap, false))
	case wasm.OpBrOnNonNull:
		lt := labelTypes(f.label(in.A))
		if len(lt) == 0 || lt[len(lt)-1].Kind != wasm.Ref {
			fail("type mismatch: br_on_non_null label must end with a reference type")
		}
		rt := f.popRef()
		if !c.matchVal(wasm.RefType(rt.Heap, false), lt[len(lt)-1]) {
			fail("type mismatch: br_on_non_null operand %s does not match label", rt)
		}
		rest := lt[:len(lt)-1]
		f.popVals(rest)
		f.pushAll(rest)
	default:
		f.instr2(in)
	}
}

func (f *fctx) initLocal(x uint32) {
	if !f.inited[x] {
		f.inited[x] = true
		f.initStack = append(f.initStack, int(x))
	}
}

func (f *fctx) checkReturnCall(ft *wasm.FuncType) {
	if !f.c.matchVals(ft.Results, f.results) {
		fail("type mismatch: tail call result type %s does not match function result %s", typesString(ft.Results), typesString(f.results))
	}
	f.popVals(ft.Params)
	f.setUnreachable()
}

var numSigs = map[wasm.Op]string{}

func init() {
	set := func(lo, hi wasm.Op, sig string) {
		for o := lo; o <= hi; o++ {
			numSigs[o] = sig
		}
	}
	set(0x45, 0x45, "i:i")
	set(0x46, 0x4F, "ii:i")
	set(0x50, 0x50, "l:i")
	set(0x51, 0x5A, "ll:i")
	set(0x5B, 0x60, "ff:i")
	set(0x61, 0x66, "dd:i")
	set(0x67, 0x69, "i:i")
	set(0x6A, 0x78, "ii:i")
	set(0x79, 0x7B, "l:l")
	set(0x7C, 0x8A, "ll:l")
	set(0x8B, 0x91, "f:f")
	set(0x92, 0x98, "ff:f")
	set(0x99, 0x9F, "d:d")
	set(0xA0, 0xA6, "dd:d")
	set(0xA7, 0xA7, "l:i")
	set(0xA8, 0xA9, "f:i")
	set(0xAA, 0xAB, "d:i")
	set(0xAC, 0xAD, "i:l")
	set(0xAE, 0xAF, "f:l")
	set(0xB0, 0xB1, "d:l")
	set(0xB2, 0xB3, "i:f")
	set(0xB4, 0xB5, "l:f")
	set(0xB6, 0xB6, "d:f")
	set(0xB7, 0xB8, "i:d")
	set(0xB9, 0xBA, "l:d")
	set(0xBB, 0xBB, "f:d")
	set(0xBC, 0xBC, "f:i")
	set(0xBD, 0xBD, "d:l")
	set(0xBE, 0xBE, "i:f")
	set(0xBF, 0xBF, "l:d")
	set(0xC0, 0xC1, "i:i")
	set(0xC2, 0xC4, "l:l")
	set(wasm.OpI32TruncSatF32S, wasm.OpI32TruncSatF32U, "f:i")
	set(wasm.OpI32TruncSatF64S, wasm.OpI32TruncSatF64U, "d:i")
	set(wasm.OpI64TruncSatF32S, wasm.OpI64TruncSatF32U, "f:l")
	set(wasm.OpI64TruncSatF64S, wasm.OpI64TruncSatF64U, "d:l")
}

// memory load/store: signature and log2 of natural alignment
var memOps = map[wasm.Op]struct {
	sig   string
	align uint32
}{
	wasm.OpI32Load: {"m:i", 2}, wasm.OpI64Load: {"m:l", 3}, wasm.OpF32Load: {"m:f", 2}, wasm.OpF64Load: {"m:d", 3},
	wasm.OpI32Load8S: {"m:i", 0}, wasm.OpI32Load8U: {"m:i", 0}, wasm.OpI32Load16S: {"m:i", 1}, wasm.OpI32Load16U: {"m:i", 1},
	wasm.OpI64Load8S: {"m:l", 0}, wasm.OpI64Load8U: {"m:l", 0}, wasm.OpI64Load16S: {"m:l", 1}, wasm.OpI64Load16U: {"m:l", 1},
	wasm.OpI64Load32S: {"m:l", 2}, wasm.OpI64Load32U: {"m:l", 2},
	wasm.OpI32Store: {"mi:", 2}, wasm.OpI64Store: {"ml:", 3}, wasm.OpF32Store: {"mf:", 2}, wasm.OpF64Store: {"md:", 3},
	wasm.OpI32Store8: {"mi:", 0}, wasm.OpI32Store16: {"mi:", 1},
	wasm.OpI64Store8: {"ml:", 0}, wasm.OpI64Store16: {"ml:", 1}, wasm.OpI64Store32: {"ml:", 2},
}

func simdAlign(sub uint32) uint32 {
	switch sub {
	case 0, 11:
		return 4
	case 1, 2, 3, 4, 5, 6, 10, 93, 87, 91:
		return 3
	case 9, 92, 86, 90:
		return 2
	case 8, 85, 89:
		return 1
	}
	return 0 // 7, 84, 88
}

func simdLanes(sub uint32) int {
	switch sub {
	case 21, 22, 23, 84, 88:
		return 16
	case 24, 25, 26, 85, 89:
		return 8
	case 27, 28, 31, 32, 86, 90:
		return 4
	}
	return 2 // 29, 30, 33, 34, 87, 91
}

func (f *fctx) instr2(in *wasm.Instr) {
	c := f.c
	if sig, ok := numSigs[in.Op]; ok {
		f.applySig(sig, bot)
		return
	}
	if mo, ok := memOps[in.Op]; ok {
		at := f.memArg(in, mo.align)
		f.applySig(mo.sig, at)
		return
	}
	switch in.Op.Prefix() {
	case wasm.PrefixSIMD:
		sub := in.Op.Sub()
		info := wasm.SIMDOps[sub]
		var at wasm.ValType
		if sub <= 11 || (sub >= 84 && sub <= 93) {
			at = f.memArg(in, simdAlign(sub))
		}
		if (sub >= 21 && sub <= 34) || (sub >= 84 && sub <= 91) {
			if int(in.Lane) >= simdLanes(sub) {
				fail("invalid lane index")
			}
		}
		if sub == 13 {
			for _, b := range in.V128 {
				if b >= 32 {
					fail("invalid lane index")
				}
			}
		}
		f.applySig(info.Sig, at)
		return
	case wasm.PrefixMisc:
		f.miscInstr(in)
		return
	case wasm.PrefixAtomic:
		f.atomicInstr(in)
		return
	case wasm.PrefixGC:
		f.gcInstr(in)
		return
	}
	_ = c
	fail("illegal opcode 0x%x", uint32(in.Op))
}

func minAddr(a, b wasm.Limits) wasm.ValType {
	if a.Is64 && b.Is64 {
		return wasm.TI64
	}
	return wasm.TI32
}

func (f *fctx) miscInstr(in *wasm.Instr) {
	c := f.c
	switch in.Op {
	case wasm.OpMemoryInit:
		at := addrType(c.mem(in.B).Limits)
		f.data(in.A)
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.popExpect(at)
	case wasm.OpDataDrop:
		f.data(in.A)
	case wasm.OpMemoryCopy:
		d := c.mem(in.A).Limits
		s := c.mem(in.B).Limits
		f.popExpect(minAddr(d, s))
		f.popExpect(addrType(s))
		f.popExpect(addrType(d))
	case wasm.OpMemoryFill:
		at := addrType(c.mem(in.A).Limits)
		f.popExpect(at)
		f.popExpect(wasm.TI32)
		f.popExpect(at)
	case wasm.OpTableInit:
		tt := c.table(in.B)
		et := f.elemType(in.A)
		if !c.matchVal(et, tt.Elem) {
			fail("type mismatch: element segment type %s does not match table type %s", et, tt.Elem)
		}
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.popExpect(addrType(tt.Limits))
	case wasm.OpElemDrop:
		f.elemType(in.A)
	case wasm.OpTableCopy:
		d := c.table(in.A)
		s := c.table(in.B)
		if !c.matchVal(s.Elem, d.Elem) {
			fail("type mismatch: table.copy source type %s does not match destination %s", s.Elem, d.Elem)
		}
		f.popExpect(minAddr(d.Limits, s.Limits))
		f.popExpect(addrType(s.Limits))
		f.popExpect(addrType(d.Limits))
	case wasm.OpTableGrow:
		tt := c.table(in.A)
		at := addrType(tt.Limits)
		f.popExpect(at)
		f.popExpect(tt.Elem)
		f.push(at)
	case wasm.OpTableSize:
		tt := c.table(in.A)
		f.push(addrType(tt.Limits))
	case wasm.OpTableFill:
		tt := c.table(in.A)
		at := addrType(tt.Limits)
		f.popExpect(at)
		f.popExpect(tt.Elem)
		f.popExpect(at)
	default:
		fail("illegal opcode 0xfc %d", in.Op.Sub())
	}
}

func (f *fctx) field(x, i uint32) wasm.FieldType {
	fs := f.c.structType(x)
	if int(i) >= len(fs) {
		fail("unknown field %d", i)
	}
	return fs[i]
}

func isNumOrVec(t wasm.ValType) bool { return t.Kind != wasm.Ref }

func (f *fctx) gcInstr(in *wasm.Instr) {
	c := f.c
	refX := func(x uint32) wasm.ValType { return wasm.RefType(wasm.HeapType(x), true) }
	switch in.Op {
	case wasm.OpStructNew:
		fs := c.structType(in.A)
		for i := len(fs) - 1; i >= 0; i-- {
			f.popExpect(fs[i].Unpacked())
		}
		f.push(wasm.RefType(wasm.HeapType(in.A), false))
	case wasm.OpStructNewDefault:
		fs := c.structType(in.A)
		for _, fl := range fs {
			if !defaultable(fl.Type) {
				fail("type mismatch: struct.new_default requires defaultable field types")
			}
		}
		f.push(wasm.RefType(wasm.HeapType(in.A), false))
	case wasm.OpStructGet, wasm.OpStructGetS, wasm.OpStructGetU:
		fl := f.field(in.A, in.B)
		if (in.Op == wasm.OpStructGet) == fl.Packed() {
			if fl.Packed() {
				fail("type mismatch: struct.get on packed field requires _s or _u")
			}
			fail("type mismatch: struct.get_s/u requires packed field")
		}
		f.popExpect(refX(in.A))
		f.push(fl.Unpacked())
	case wasm.OpStructSet:
		fl := f.field(in.A, in.B)
		if !fl.Mutable {
			fail("immutable field %d", in.B)
		}
		f.popExpect(fl.Unpacked())
		f.popExpect(refX(in.A))
	case wasm.OpArrayNew:
		et := c.arrayType(in.A)
		f.popExpect(wasm.TI32)
		f.popExpect(et.Unpacked())
		f.push(wasm.RefType(wasm.HeapType(in.A), false))
	case wasm.OpArrayNewDefault:
		et := c.arrayType(in.A)
		if !defaultable(et.Type) {
			fail("type mismatch: array.new_default requires defaultable element type")
		}
		f.popExpect(wasm.TI32)
		f.push(wasm.RefType(wasm.HeapType(in.A), false))
	case wasm.OpArrayNewFixed:
		et := c.arrayType(in.A)
		for i := uint32(0); i < in.B; i++ {
			f.popExpect(et.Unpacked())
		}
		f.push(wasm.RefType(wasm.HeapType(in.A), false))
	case wasm.OpArrayNewData:
		et := c.arrayType(in.A)
		if !isNumOrVec(et.Type) {
			fail("array type is not numeric or vector")
		}
		f.data(in.B)
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.push(wasm.RefType(wasm.HeapType(in.A), false))
	case wasm.OpArrayNewElem:
		et := c.arrayType(in.A)
		st := f.elemType(in.B)
		if !c.matchStorage(st, et.Type) {
			fail("type mismatch: element segment type %s does not match array element type %s", st, et.Type)
		}
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.push(wasm.RefType(wasm.HeapType(in.A), false))
	case wasm.OpArrayGet, wasm.OpArrayGetS, wasm.OpArrayGetU:
		et := c.arrayType(in.A)
		if (in.Op == wasm.OpArrayGet) == et.Packed() {
			if et.Packed() {
				fail("type mismatch: array.get on packed element requires _s or _u")
			}
			fail("type mismatch: array.get_s/u requires packed element")
		}
		f.popExpect(wasm.TI32)
		f.popExpect(refX(in.A))
		f.push(et.Unpacked())
	case wasm.OpArraySet:
		et := c.arrayType(in.A)
		if !et.Mutable {
			fail("immutable array")
		}
		f.popExpect(et.Unpacked())
		f.popExpect(wasm.TI32)
		f.popExpect(refX(in.A))
	case wasm.OpArrayLen:
		f.popExpect(tRefNullArray)
		f.push(wasm.TI32)
	case wasm.OpArrayFill:
		et := c.arrayType(in.A)
		if !et.Mutable {
			fail("immutable array")
		}
		f.popExpect(wasm.TI32)
		f.popExpect(et.Unpacked())
		f.popExpect(wasm.TI32)
		f.popExpect(refX(in.A))
	case wasm.OpArrayCopy:
		dt := c.arrayType(in.A)
		st := c.arrayType(in.B)
		if !dt.Mutable {
			fail("immutable array")
		}
		if !c.matchStorage(st.Type, dt.Type) {
			fail("array types do not match")
		}
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.popExpect(refX(in.B))
		f.popExpect(wasm.TI32)
		f.popExpect(refX(in.A))
	case wasm.OpArrayInitData:
		et := c.arrayType(in.A)
		if !et.Mutable {
			fail("immutable array")
		}
		if !isNumOrVec(et.Type) {
			fail("array type is not numeric or vector")
		}
		f.data(in.B)
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.popExpect(refX(in.A))
	case wasm.OpArrayInitElem:
		et := c.arrayType(in.A)
		if !et.Mutable {
			fail("immutable array")
		}
		st := f.elemType(in.B)
		if !c.matchStorage(st, et.Type) {
			fail("type mismatch: element segment type %s does not match array element type %s", st, et.Type)
		}
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.popExpect(wasm.TI32)
		f.popExpect(refX(in.A))
	case wasm.OpRefTest, wasm.OpRefTestNull, wasm.OpRefCast, wasm.OpRefCastNull:
		c.checkHeap(in.HT)
		rt := wasm.RefType(in.HT, in.Null2)
		f.popExpect(wasm.RefType(c.topHeap(in.HT), true))
		if in.Op == wasm.OpRefTest || in.Op == wasm.OpRefTestNull {
			f.push(wasm.TI32)
		} else {
			f.push(rt)
		}
	case wasm.OpBrOnCast, wasm.OpBrOnCastFail:
		c.checkHeap(in.HT)
		c.checkHeap(in.HT2)
		rt1 := wasm.RefType(in.HT, in.Null1)
		rt2 := wasm.RefType(in.HT2, in.Null2)
		if !c.matchVal(rt2, rt1) {
			fail("type mismatch: br_on_cast target type %s must be a subtype of %s", rt2, rt1)
		}
		lt := labelTypes(f.label(in.A))
		if len(lt) == 0 || lt[len(lt)-1].Kind != wasm.Ref {
			fail("type mismatch: br_on_cast label must end with a reference type")
		}
		diff := wasm.RefType(rt1.Heap, rt1.Nullable && !rt2.Nullable)
		branch, fall := rt2, diff
		if in.Op == wasm.OpBrOnCastFail {
			branch, fall = diff, rt2
		}
		if !c.matchVal(branch, lt[len(lt)-1]) {
			fail("type mismatch: br_on_cast branch type %s does not match label %s", branch, lt[len(lt)-1])
		}
		f.popExpect(rt1)
		rest := lt[:len(lt)-1]
		f.popVals(rest)
		f.pushAll(rest)
		f.push(fall)
	case wasm.OpAnyConvertExtern:
		rt := f.popExpect(tRefNullExtern)
		f.push(wasm.RefType(wasm.HeapAny, rt.Nullable))
	case wasm.OpExternConvertAny:
		rt := f.popExpect(tRefNullAny)
		f.push(wasm.RefType(wasm.HeapExtern, rt.Nullable))
	case wasm.OpRefI31:
		f.popExpect(wasm.TI32)
		f.push(wasm.RefType(wasm.HeapI31, false))
	case wasm.OpI31GetS, wasm.OpI31GetU:
		f.popExpect(tRefNullI31)
		f.push(wasm.TI32)
	default:
		fail("illegal opcode 0xfb %d", in.Op.Sub())
	}
}

func (f *fctx) atomicInstr(in *wasm.Instr) {
	info, ok := wasm.AtomicOps[in.Op.Sub()]
	if !ok {
		fail("illegal opcode 0x%x", uint32(in.Op))
	}
	if info.Kind == wasm.AtomicFence {
		return
	}
	log2 := uint32(0)
	for 1<<log2 < info.Size {
		log2++
	}
	if in.Align != log2 {
		fail("alignment must be equal to natural")
	}
	at := f.memArg(in, log2)
	t := wasm.ValType{Kind: info.Type}
	switch info.Kind {
	case wasm.AtomicNotify:
		f.popExpect(wasm.TI32)
		f.popExpect(at)
		f.push(wasm.TI32)
	case wasm.AtomicWait:
		f.popExpect(wasm.TI64)
		f.popExpect(t)
		f.popExpect(at)
		f.push(wasm.TI32)
	case wasm.AtomicLoad:
		f.popExpect(at)
		f.push(t)
	case wasm.AtomicStore:
		f.popExpect(t)
		f.popExpect(at)
	case wasm.AtomicRMW:
		f.popExpect(t)
		f.popExpect(at)
		f.push(t)
	case wasm.AtomicCmpxchg:
		f.popExpect(t)
		f.popExpect(t)
		f.popExpect(at)
		f.push(t)
	}
}
