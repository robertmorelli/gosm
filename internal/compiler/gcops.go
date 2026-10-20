package compiler

import (
	"fmt"
	"strings"

	"gosm/internal/wasm"
)

// simple ensures entry i (counted from the top, 0 = top) is not a compound
// expression so it can be referenced several times.
func (f *fn) simpleTop() entry {
	i := len(f.stack) - 1
	if f.stack[i].kind == kExpr {
		f.materialize(i)
	}
	return f.stack[i]
}

func refT(h wasm.HeapType, null bool) wasm.ValType { return wasm.RefType(h, null) }

var (
	anyRef = refT(wasm.HeapAny, true)
	eqRef  = refT(wasm.HeapEq, true)
)

// testExpr returns a Go boolean expression testing whether reference x (of
// static type x.t) is in type (ref null? ht).
func (f *fn) testExpr(x entry, ht wasm.HeapType, nullable bool) string {
	c := f.c
	xs := f.ex(x)
	if c.isSubHeap(x.t.Heap, ht) {
		if nullable || !x.t.Nullable {
			return "true"
		}
		return fmt.Sprintf("%s != nil", xs)
	}
	var t string
	top := c.topHeap(ht)
	switch top {
	case wasm.HeapFunc:
		switch {
		case ht == wasm.HeapFunc:
			t = xs + " != nil"
		case ht == wasm.HeapNoFunc:
			t = "false"
		default:
			t = fmt.Sprintf("rt.IsFuncType(%s, %s)", xs, c.info(uint32(ht)).rtt)
		}
	case wasm.HeapExn:
		if ht == wasm.HeapExn {
			t = xs + " != nil"
		} else {
			t = "false"
		}
	case wasm.HeapExtern:
		if ht == wasm.HeapExtern {
			t = xs + " != nil"
		} else {
			t = "false"
		}
	default:
		if !x.t.Heap.IsAbstract() && !ht.IsAbstract() {
			// concrete -> concrete within one hierarchy: read the header
			src, dst := c.info(uint32(x.t.Heap)), c.info(uint32(ht))
			if src.header {
				if !dst.hasSub && dst.final {
					t = fmt.Sprintf("(%s != nil && rt.ObjRTT(%s) == %s)", xs, xs, dst.rtt)
				} else {
					t = fmt.Sprintf("(%s != nil && rt.ObjRTT(%s).SubOf(%s))", xs, xs, dst.rtt)
				}
				break
			}
		}
		xa := c.conv(xs, x.t, anyRef)
		switch ht {
		case wasm.HeapAny:
			t = xa + " != nil"
		case wasm.HeapEq:
			t = "rt.IsEq(" + xa + ")"
		case wasm.HeapI31:
			t = "rt.IsI31(" + xa + ")"
		case wasm.HeapStruct:
			t = "rt.IsStruct(" + xa + ")"
		case wasm.HeapArray:
			t = "rt.IsArray(" + xa + ")"
		case wasm.HeapNone:
			t = "false"
		default:
			t = fmt.Sprintf("rt.IsType(%s, %s)", xa, c.info(uint32(ht)).rtt)
		}
	}
	if nullable && x.t.Nullable {
		if t == "false" {
			return xs + " == nil"
		}
		return fmt.Sprintf("(%s == nil || %s)", xs, t)
	}
	return t
}

func (f *fn) refOp(in *wasm.Instr) bool {
	c := f.c
	switch in.Op {
	case wasm.OpRefNull:
		f.pushNull(refT(in.HT, true))
	case wasm.OpRefIsNull:
		x := f.pop()
		f.pushPure(ti32, fmt.Sprintf("rt.B2I(%s == nil)", f.ex(x)), x)
	case wasm.OpRefFunc:
		f.pushPure(refT(wasm.HeapType(c.m.FuncTypeIdx(in.A)), false), c.funcRefExpr(in.A))
	case wasm.OpRefEq:
		b := f.pop()
		a := f.pop()
		var ea, eb string
		if c.goType(a.t) == c.goType(b.t) {
			ea, eb = f.ex(a), f.ex(b)
		} else {
			ea, eb = c.conv(f.ex(a), a.t, eqRef), c.conv(f.ex(b), b.t, eqRef)
		}
		f.pushPure(ti32, fmt.Sprintf("rt.B2I(%s == %s)", ea, eb), a, b)
	case wasm.OpRefAsNonNull:
		x := f.simpleTop()
		f.pop()
		f.emit("if %s == nil {\npanic(rt.ErrNullRef)\n}", f.ex(x))
		x.t.Nullable = false
		f.push(x)
	case wasm.OpBrOnNull:
		x := f.simpleTop()
		f.pop()
		f.emit("if %s == nil {", f.ex(x))
		f.branch(in.A)
		f.emit("}")
		x.t.Nullable = false
		f.push(x)
	case wasm.OpBrOnNonNull:
		x := f.simpleTop()
		f.stack[len(f.stack)-1].t.Nullable = false
		f.emit("if %s != nil {", f.ex(x))
		f.branch(in.A)
		f.emit("}")
		f.pop()
	default:
		return false
	}
	return true
}

// fieldOf returns field i of type typeIdx with its wasm types expressed in
// this module's type index space (the universe's layout may come from an
// equivalent type of another module).
func (f *fn) fieldOf(typeIdx uint32, i uint32) fieldInfo { return f.c.fieldOf(typeIdx, i) }

func (c *compiler) fieldOf(typeIdx uint32, i uint32) fieldInfo {
	fi := c.info(typeIdx).fields[i]
	fi.ft = c.m.Types[typeIdx].Comp.Fields[i]
	t := typeIdx
	for {
		st := &c.m.Types[t]
		if len(st.Supers) == 0 || int(i) >= len(c.m.Types[st.Supers[0]].Comp.Fields) {
			break
		}
		t = st.Supers[0]
	}
	fi.storageVT = c.m.Types[t].Comp.Fields[i].Type
	return fi
}

// storageVT is the value type whose Go representation is the field storage.
func (c *compiler) storageVT(fi fieldInfo) wasm.ValType { return fi.storageVT }

// toStorage converts a popped value to field storage representation.
func (f *fn) toStorage(e entry, fi fieldInfo) string {
	c := f.c
	switch fi.ft.Type.Kind {
	case wasm.I8:
		if e.kind == kConst && e.isNum {
			return fmt.Sprintf("int8(%d)", int8(e.bits))
		}
		return "int8(" + f.ex(e) + ")"
	case wasm.I16:
		if e.kind == kConst && e.isNum {
			return fmt.Sprintf("int16(%d)", int16(e.bits))
		}
		return "int16(" + f.ex(e) + ")"
	}
	return c.conv(f.ex(e), e.t, c.storageVT(fi))
}

// fromStorage converts a field read to the field's value type.
func (f *fn) fromStorage(expr string, fi fieldInfo, signed bool) string {
	switch fi.ft.Type.Kind {
	case wasm.I8:
		if signed {
			return "int32(" + expr + ")"
		}
		return "int32(uint8(" + expr + "))"
	case wasm.I16:
		if signed {
			return "int32(" + expr + ")"
		}
		return "int32(uint16(" + expr + "))"
	}
	return f.c.conv(expr, f.c.storageVT(fi), fi.ft.Type)
}

func (f *fn) elemDec(ft wasm.FieldType) (string, int) {
	switch ft.Type.Kind {
	case wasm.I8:
		return "rt.DecI8", 1
	case wasm.I16:
		return "rt.DecI16", 2
	case wasm.I32:
		return "rt.DecI32", 4
	case wasm.I64:
		return "rt.DecI64", 8
	case wasm.F32:
		return "rt.DecF32", 4
	case wasm.F64:
		return "rt.DecF64", 8
	case wasm.V128:
		return "rt.DecV128", 16
	}
	panic("array.new_data on reference array")
}

// segSlice converts elem segment values to the array element storage type.
func (f *fn) convSlice(src string, from wasm.ValType, fi fieldInfo) string {
	c := f.c
	to := c.storageVT(fi)
	if c.goType(from) == c.goType(to) {
		return src
	}
	return fmt.Sprintf("rt.ConvSlice(%s, func(x %s) %s { return %s })", src, c.goType(from), c.goType(to), c.conv("x", from, to))
}

func (f *fn) gcOp(in *wasm.Instr) bool {
	c := f.c
	if in.Op.Prefix() != wasm.PrefixGC {
		return false
	}
	switch in.Op {
	case wasm.OpStructNew, wasm.OpStructNewDefault:
		ti := c.info(in.A)
		var args []string
		if in.Op == wasm.OpStructNew {
			vals := f.popN(len(ti.fields))
			for i := range ti.fields {
				args = append(args, f.toStorage(vals[i], f.fieldOf(in.A, uint32(i))))
			}
			f.pushEager(refT(wasm.HeapType(in.A), false), fmt.Sprintf("%s(%s)", c.ctorName(ti), strings.Join(args, ", ")))
		} else {
			f.pushEager(refT(wasm.HeapType(in.A), false), fmt.Sprintf("%s()", c.ctorDefaultName(ti)))
		}
	case wasm.OpStructGet, wasm.OpStructGetS, wasm.OpStructGetU:
		fi := f.fieldOf(in.A, in.B)
		p := f.popAs(refT(wasm.HeapType(in.A), true))
		r := f.fromStorage(p+"."+fi.name, fi, in.Op != wasm.OpStructGetU)
		f.pushEager(fi.ft.Unpacked(), r)
	case wasm.OpStructSet:
		fi := f.fieldOf(in.A, in.B)
		v := f.pop()
		p := f.popAs(refT(wasm.HeapType(in.A), true))
		f.emit("%s.%s = %s", p, fi.name, f.toStorage(v, fi))
	case wasm.OpArrayNew, wasm.OpArrayNewDefault:
		ti := c.info(in.A)
		fi := f.fieldOf(in.A, 0)
		n := f.pop()
		var v string
		if in.Op == wasm.OpArrayNew {
			v = f.toStorage(f.pop(), fi)
		} else {
			v = c.zeroOf(c.storageVT(fi))
			if fi.ft.Packed() {
				v = fi.storage + "(0)"
			}
		}
		mk := "rt.MakeSlice"
		if fi.ft.Type.Kind == wasm.V128 {
			mk = "rt.MakeSliceAny"
		}
		f.pushEager(refT(wasm.HeapType(in.A), false), fmt.Sprintf("%s(%s(%s, %s))", c.ctorName(ti), mk, f.ex(n), v))
	case wasm.OpArrayNewFixed:
		ti := c.info(in.A)
		fi := f.fieldOf(in.A, 0)
		vals := f.popN(int(in.B))
		var args []string
		for _, v := range vals {
			args = append(args, f.toStorage(v, fi))
		}
		f.pushEager(refT(wasm.HeapType(in.A), false), fmt.Sprintf("%s([]%s{%s})", c.ctorName(ti), fi.storage, strings.Join(args, ", ")))
	case wasm.OpArrayNewData:
		ti := c.info(in.A)
		fi := f.fieldOf(in.A, 0)
		dec, size := f.elemDec(fi.ft)
		vals := f.popN(2)
		f.pushEager(refT(wasm.HeapType(in.A), false), fmt.Sprintf("%s(%s(rt.DataBytes(%s, %s, %s, %d)))", c.ctorName(ti), dec, c.dataRef(in.B), f.ex(vals[0]), f.ex(vals[1]), size))
	case wasm.OpArrayNewElem:
		ti := c.info(in.A)
		fi := f.fieldOf(in.A, 0)
		vals := f.popN(2)
		seg := fmt.Sprintf("rt.SegSlice(%s, %s, %s)", c.elemRef(in.B), f.ex(vals[0]), f.ex(vals[1]))
		conv := f.convSlice(seg, c.m.Elems[in.B].Type, fi)
		if conv == seg {
			conv = fmt.Sprintf("append([]%s(nil), %s...)", fi.storage, seg)
		}
		f.pushEager(refT(wasm.HeapType(in.A), false), fmt.Sprintf("%s(%s)", c.ctorName(ti), conv))
	case wasm.OpArrayGet, wasm.OpArrayGetS, wasm.OpArrayGetU:
		fi := f.fieldOf(in.A, 0)
		i := f.pop()
		p := f.popAs(refT(wasm.HeapType(in.A), true))
		r := f.fromStorage(fmt.Sprintf("%s.Elems[%s]", p, f.u32(i)), fi, in.Op != wasm.OpArrayGetU)
		f.pushEager(fi.ft.Unpacked(), r)
	case wasm.OpArraySet:
		fi := f.fieldOf(in.A, 0)
		v := f.pop()
		i := f.pop()
		p := f.popAs(refT(wasm.HeapType(in.A), true))
		f.emit("%s.Elems[%s] = %s", p, f.u32(i), f.toStorage(v, fi))
	case wasm.OpArrayLen:
		x := f.pop()
		if x.t.IsRef() && !x.t.Heap.IsAbstract() {
			f.pushEager(ti32, fmt.Sprintf("int32(len(%s.Elems))", f.ex(x)))
		} else {
			f.pushEager(ti32, fmt.Sprintf("rt.ArrayLen(%s)", f.ex(x)))
		}
	case wasm.OpArrayFill:
		fi := f.fieldOf(in.A, 0)
		n := f.pop()
		v := f.pop()
		off := f.pop()
		p := f.popAs(refT(wasm.HeapType(in.A), true))
		f.emit("rt.ArrayFill(%s.Elems, %s, %s, %s)", p, f.ex(off), f.toStorage(v, fi), f.ex(n))
	case wasm.OpArrayCopy:
		dfi := f.fieldOf(in.A, 0)
		sfi := f.fieldOf(in.B, 0)
		n := f.pop()
		s := f.pop()
		src := f.popAs(refT(wasm.HeapType(in.B), true))
		d := f.pop()
		dst := f.popAs(refT(wasm.HeapType(in.A), true))
		if dfi.storage == sfi.storage {
			f.emit("rt.ArrayCopy(%s.Elems, %s, %s.Elems, %s, %s)", dst, f.ex(d), src, f.ex(s), f.ex(n))
		} else {
			from, to := c.storageVT(sfi), c.storageVT(dfi)
			f.emit("rt.ArrayCopyConv(%s.Elems, %s, %s.Elems, %s, %s, func(x %s) %s { return %s })",
				dst, f.ex(d), src, f.ex(s), f.ex(n), c.goType(from), c.goType(to), c.conv("x", from, to))
		}
	case wasm.OpArrayInitData:
		fi := f.fieldOf(in.A, 0)
		dec, size := f.elemDec(fi.ft)
		vals := f.popN(3)
		p := f.popAs(refT(wasm.HeapType(in.A), true))
		f.emit("rt.ArrayInit(%s.Elems, %s, %s(rt.DataBytes(%s, %s, %s, %d)), 0, %s)", p, f.ex(vals[0]), dec, c.dataRef(in.B), f.ex(vals[1]), f.ex(vals[2]), size, f.ex(vals[2]))
	case wasm.OpArrayInitElem:
		fi := f.fieldOf(in.A, 0)
		vals := f.popN(3)
		p := f.popAs(refT(wasm.HeapType(in.A), true))
		seg := f.convSlice(c.elemRef(in.B), c.m.Elems[in.B].Type, fi)
		f.emit("rt.ArrayInit(%s.Elems, %s, %s, %s, %s)", p, f.ex(vals[0]), seg, f.ex(vals[1]), f.ex(vals[2]))
	case wasm.OpRefTest, wasm.OpRefTestNull:
		x := f.simpleTop()
		f.pop()
		f.pushPure(ti32, "rt.B2I("+f.testExpr(x, in.HT, in.Null2)+")", x)
	case wasm.OpRefCast, wasm.OpRefCastNull:
		x := f.simpleTop()
		f.pop()
		t := f.testExpr(x, in.HT, in.Null2)
		if t != "true" {
			f.emit("if !(%s) {\npanic(rt.ErrCast)\n}", t)
		}
		to := refT(in.HT, in.Null2)
		f.pushPure(to, c.conv(f.ex(x), x.t, to), x)
	case wasm.OpBrOnCast, wasm.OpBrOnCastFail:
		x := f.simpleTop()
		t2 := refT(in.HT2, in.Null2)
		test := f.testExpr(x, in.HT2, in.Null2)
		casted := x
		casted.t = t2
		casted.expr = c.conv(f.ex(x), x.t, t2)
		casted.kind = kExpr
		failT := x.t
		if in.Null2 {
			failT.Nullable = false
		}
		top := len(f.stack) - 1
		if in.Op == wasm.OpBrOnCast {
			f.emit("if %s {", test)
			f.stack[top] = casted
			f.branch(in.A)
			f.emit("}")
			x.t = failT
			f.stack[top] = x
		} else {
			f.emit("if !(%s) {", test)
			fx := x
			fx.t = failT
			f.stack[top] = fx
			f.branch(in.A)
			f.emit("}")
			f.stack[top] = casted
		}
	case wasm.OpAnyConvertExtern:
		x := f.pop()
		t := refT(wasm.HeapAny, x.t.Nullable)
		x.t = t
		f.push(x)
	case wasm.OpExternConvertAny:
		x := f.pop()
		t := refT(wasm.HeapExtern, x.t.Nullable)
		e := c.conv(f.ex(x), x.t, anyRef)
		f.pushPure(t, e, x)
	case wasm.OpRefI31:
		x := f.pop()
		f.pushPure(refT(wasm.HeapI31, false), "rt.MakeI31("+f.ex(x)+")", x)
	case wasm.OpI31GetS:
		x := f.pop()
		f.pushEager(ti32, "rt.I31GetS("+f.ex(x)+")")
	case wasm.OpI31GetU:
		x := f.pop()
		f.pushEager(ti32, "rt.I31GetU("+f.ex(x)+")")
	default:
		return false
	}
	return true
}

func (f *fn) tableOp(in *wasm.Instr) bool {
	c := f.c
	switch in.Op {
	case wasm.OpTableGet:
		i := f.pop()
		f.pushEager(c.m.TableType(in.A).Elem, fmt.Sprintf("%s.Get(%s)", c.tableRef(in.A), f.u64(i)))
	case wasm.OpTableSet:
		et := c.m.TableType(in.A).Elem
		v := f.popAs(et)
		i := f.pop()
		f.emit("%s.Set(%s, %s)", c.tableRef(in.A), f.u64(i), v)
	case wasm.OpTableSize:
		if c.m.TableType(in.A).Limits.Is64 {
			f.pushEager(ti64, fmt.Sprintf("int64(%s.Size())", c.tableRef(in.A)))
		} else {
			f.pushEager(ti32, fmt.Sprintf("int32(%s.Size())", c.tableRef(in.A)))
		}
	case wasm.OpTableGrow:
		tt := c.m.TableType(in.A)
		n := f.pop()
		v := f.popAs(tt.Elem)
		if tt.Limits.Is64 {
			f.pushEager(ti64, fmt.Sprintf("%s.Grow(%s, %s)", c.tableRef(in.A), f.u64(n), v))
		} else {
			f.pushEager(ti32, fmt.Sprintf("int32(%s.Grow(%s, %s))", c.tableRef(in.A), f.u64(n), v))
		}
	case wasm.OpTableFill:
		tt := c.m.TableType(in.A)
		n := f.pop()
		v := f.popAs(tt.Elem)
		i := f.pop()
		f.emit("%s.Fill(%s, %s, %s)", c.tableRef(in.A), f.u64(i), v, f.u64(n))
	case wasm.OpTableCopy:
		dt, st := c.m.TableType(in.A), c.m.TableType(in.B)
		vals := f.popN(3)
		if c.goType(dt.Elem) == c.goType(st.Elem) {
			f.emit("rt.TableCopy(%s, %s, %s, %s, %s)", c.tableRef(in.A), f.u64(vals[0]), c.tableRef(in.B), f.u64(vals[1]), f.u64(vals[2]))
		} else {
			f.emit("rt.TableCopyConv(%s, %s, %s, %s, %s, func(x %s) %s { return %s })", c.tableRef(in.A), f.u64(vals[0]), c.tableRef(in.B), f.u64(vals[1]), f.u64(vals[2]),
				c.goType(st.Elem), c.goType(dt.Elem), c.conv("x", st.Elem, dt.Elem))
		}
	case wasm.OpTableInit:
		tt := c.m.TableType(in.B)
		et := c.m.Elems[in.A].Type
		vals := f.popN(3)
		seg := c.elemRef(in.A)
		if c.goType(et) != c.goType(tt.Elem) {
			seg = fmt.Sprintf("rt.ConvSlice(%s, func(x %s) %s { return %s })", seg, c.goType(et), c.goType(tt.Elem), c.conv("x", et, tt.Elem))
		}
		f.emit("%s.Init(%s, %s, %s, %s)", c.tableRef(in.B), f.u64(vals[0]), seg, f.u64(vals[1]), f.u64(vals[2]))
	case wasm.OpElemDrop:
		f.emit("%s = nil", c.elemRef(in.A))
	default:
		return false
	}
	return true
}

// --- legacy exception handling (try / catch / catch_all / delegate / rethrow) ---

type legacyTry struct {
	id       int
	exnVar   string
	inBody   bool
	handlers int
	bodyW    *strings.Builder // closure body
	handlerW *strings.Builder // switch cases for handlers
	clauses  []string         // recover matching code
	delegate bool
}

func (f *fn) newTryID() int {
	f.nlabel++
	return f.nlabel
}

func (f *fn) beginLegacyTry(in *wasm.Instr) {
	params, results := f.blockSig(in.Block)
	f.spillAll(false)
	t := &ctrl{
		kind:    ctrlLegacy,
		params:  params,
		results: results,
		base:    f.height() - len(params),
		closure: f.closure,
		label:   f.newLabel("B"),
	}
	for i, p := range params {
		h := t.base + i
		e := f.stack[h]
		s := f.slot(h, f.c.goType(p))
		if !(e.kind == kSlot && e.expr == s) {
			f.emit("%s = %s", s, f.c.conv(f.ex(e), e.t, p))
			f.stack[h] = entry{t: p, expr: s, kind: kSlot, slots: []string{s}, maxH: h}
		}
	}
	lt := &legacyTry{id: f.newTryID(), inBody: true}
	lt.exnVar = fmt.Sprintf("x%d", lt.id)
	t.legacy = lt
	f.ctrls = append(f.ctrls, t)
	t.parentW = f.w
	f.w = &strings.Builder{}
	f.closure++
	f.tryStack = append(f.tryStack, t)
}

// leaveBody closes the try body closure scope (at the first catch or end).
func (f *fn) leaveLegacyBody(t *ctrl) {
	lt := t.legacy
	if !t.unreachable {
		vals := f.stack[len(f.stack)-len(t.results):]
		f.assignTo(t.base, t.results, vals)
		t.endReached = true
	}
	lt.bodyW = f.w
	lt.inBody = false
	f.closure--
	f.tryStack = f.tryStack[:len(f.tryStack)-1]
	lt.handlerW = &strings.Builder{}
	f.w = lt.handlerW
}

func (f *fn) legacyCatch(in *wasm.Instr) {
	c := f.c
	t := f.top()
	lt := t.legacy
	if lt.inBody {
		f.leaveLegacyBody(t)
	} else {
		// end of previous handler
		if !t.unreachable {
			vals := f.stack[len(f.stack)-len(t.results):]
			f.assignTo(t.base, t.results, vals)
			t.endReached = true
		}
	}
	lt.handlers++
	code := -lt.handlers
	f.declare(lt.exnVar, "*rt.Exception")
	f.stack = f.stack[:t.base]
	t.unreachable = false
	f.emit("case %d:", code)
	if in.Op == wasm.OpCatch {
		lt.clauses = append(lt.clauses, fmt.Sprintf("if e.Tag == %s {\n%s = e\nc = %d\nreturn\n}", c.tagRef(in.A), lt.exnVar, code))
		tft := c.m.Types[c.m.TagTypeIdx(in.A)].Comp.Func
		for i, p := range tft.Params {
			f.pushEager(p, fmt.Sprintf("%s.Values[%d].(%s)", lt.exnVar, i, c.goType(p)))
		}
	} else {
		lt.clauses = append(lt.clauses, fmt.Sprintf("%s = e\nc = %d\nreturn", lt.exnVar, code))
	}
}

// delegateTarget returns the try id that receives exceptions delegated to
// label depth l (relative to the try's enclosing context), or 0 for the
// function's caller.
func (f *fn) delegateTarget(from int) int {
	for i := from; i >= 0; i-- {
		t := f.ctrls[i]
		if t.kind == ctrlLegacy && t.legacy.inBody {
			return t.legacy.id
		}
		if t.kind == ctrlTry {
			return f.tryID(t)
		}
	}
	return 0
}

func (f *fn) tryID(t *ctrl) int {
	if t.legacy == nil {
		t.legacy = &legacyTry{id: f.newTryID()}
	}
	return t.legacy.id
}

func (f *fn) legacyDelegate(in *wasm.Instr) {
	t := f.top()
	lt := t.legacy
	f.leaveLegacyBody(t)
	target := f.delegateTarget(len(f.ctrls) - 2 - int(in.A))
	if target == 0 {
		f.needDelegateDefer = true
	}
	lt.delegate = true
	lt.clauses = append(lt.clauses, fmt.Sprintf("panic(&rt.Delegated{Exn: e, Target: %d})", target))
	f.endLegacyTry(t)
}

func (f *fn) legacyRethrow(in *wasm.Instr) {
	t := f.ctrls[len(f.ctrls)-1-int(in.A)]
	f.emit("panic(%s)", t.legacy.exnVar)
	f.setUnreachable()
}

func (f *fn) endLegacyTry(t *ctrl) {
	lt := t.legacy
	if lt.inBody {
		f.leaveLegacyBody(t)
	} else if !lt.delegate && !t.unreachable {
		vals := f.stack[len(f.stack)-len(t.results):]
		f.assignTo(t.base, t.results, vals)
		t.endReached = true
	}
	f.w = t.parentW
	f.ctrls = f.ctrls[:len(f.ctrls)-1]

	cvar := fmt.Sprintf("c%d", lt.id)
	f.declare(cvar, "int")
	f.emit("%s = func() (c int) {", cvar)
	if len(lt.clauses) > 0 {
		f.emit("defer func() {")
		f.emit("if r := recover(); r != nil {")
		f.emit("e := rt.Unwrap(r, %d)", lt.id)
		for _, cl := range lt.clauses {
			f.emit("%s", cl)
		}
		if !strings.Contains(lt.clauses[len(lt.clauses)-1], "c = ") || strings.HasPrefix(lt.clauses[len(lt.clauses)-1], "if ") {
			f.emit("panic(r)")
		}
		f.emit("}")
		f.emit("}()")
	}
	f.w.WriteString(lt.bodyW.String())
	f.emit("return 0")
	f.emit("}()")
	if len(t.exits) > 0 || lt.handlers > 0 {
		f.emit("switch %s {", cvar)
		f.w.WriteString(lt.handlerW.String())
		for _, x := range t.exits {
			f.emit("case %d:", x.code)
			if x.target == t {
				t.used = true
				f.emit("goto %s", t.label)
			} else {
				f.jump(x.target)
			}
		}
		f.emit("}")
	}
	if t.used {
		f.emit("%s:", t.label)
	}
	f.finishBlock(t)
}
