package compiler

// Exception handling.
//
// In modules that contain handlers (try_table or legacy try), exceptions do
// not use Go panics inside wasm code. A throw stores the exception in
// m.exn and transfers control to the innermost enclosing handler (a label
// in the same Go function) or returns zero values; every call site checks
// m.exn afterwards and does the same. try blocks are therefore ordinary
// inline code: entering one costs nothing.
//
// At the boundaries the Go convention (panic with *rt.Exception) is kept:
// host import wrappers recover such panics into m.exn, and entry points
// (exports, function references, start) re-panic a pending exception.
//
// Modules without handlers throw with panic directly.

import (
	"fmt"
	"strings"

	"gosm/internal/wasm"
)

type legacyTry struct {
	exnVar   string
	inBody   bool
	clauses  []string // dispatch code for the catch clauses, in order
	hasAll   bool
	delegate int // ctrl index to unwind from for delegate, -1 if none
}

// handlerLabel returns the label of t's exception dispatch code.
func (t *ctrl) handlerLabel() string { return "H" + t.label[1:] }

// unwind emits a transfer of the pending exception m.exn to the innermost
// handler enclosing ctrl index from (inclusive), or a return.
func (f *fn) unwindFrom(from int) {
	for i := from; i >= 0; i-- {
		t := f.ctrls[i]
		switch t.kind {
		case ctrlTry:
			t.handlerUsed = true
			f.emit("goto %s", t.handlerLabel())
			return
		case ctrlLegacy:
			if t.legacy.inBody {
				t.handlerUsed = true
				f.emit("goto %s", t.handlerLabel())
				return
			}
		}
	}
	f.returnZerosPlain()
}

func (f *fn) unwind() { f.unwindFrom(len(f.ctrls) - 1) }

// returnZerosPlain returns zero values without touching the operand stack.
func (f *fn) returnZerosPlain() {
	if len(f.results) == 0 {
		f.emit("return")
		return
	}
	var zs []string
	for _, r := range f.results {
		zs = append(zs, f.c.zeroOf(r))
	}
	f.emit("return %s", strings.Join(zs, ", "))
}

// checkExn propagates a pending exception after a call.
func (f *fn) checkExn() {
	if !f.c.usesExn {
		return
	}
	f.emit("if m.exn != nil {")
	f.unwind()
	f.emit("}")
}

// throwExpr raises the exception e (a Go expression of type *rt.Exception).
func (f *fn) raise(e string) {
	if f.c.usesExn {
		f.emit("m.exn = %s", e)
		f.unwind()
	} else {
		f.emit("panic(%s)", e)
	}
	f.setUnreachable()
}

func (f *fn) throw(tag uint32) {
	c := f.c
	tft := c.m.Types[c.m.TagTypeIdx(tag)].Comp.Func
	vals := f.popN(len(tft.Params))
	var args []string
	for i, p := range tft.Params {
		args = append(args, c.conv(f.ex(vals[i]), vals[i].t, p))
	}
	if len(args) == 0 {
		f.raise(fmt.Sprintf("&rt.Exception{Tag: %s}", c.tagRef(tag)))
	} else {
		f.raise(fmt.Sprintf("&rt.Exception{Tag: %s, Values: []any{%s}}", c.tagRef(tag), strings.Join(args, ", ")))
	}
}

func (f *fn) throwRef() {
	e := f.pop()
	f.raise(fmt.Sprintf("rt.NonNullExn(%s)", f.ex(e)))
}

// catchDest is the variable receiving the i-th value delivered to target.
func (f *fn) catchDest(target *ctrl, i int) string {
	if target.kind == ctrlFunc {
		f.ensureRetVars()
		return f.retVars[i]
	}
	return f.slot(target.base+i, f.c.goType(target.labelTypes()[i]))
}

func (f *fn) beginTryCommon(in *wasm.Instr, kind int) *ctrl {
	params, results := f.blockSig(in.Block)
	f.spillAll(false)
	t := &ctrl{
		kind:    kind,
		params:  params,
		results: results,
		base:    f.height() - len(params),
		label:   f.newLabel("B"),
		catches: in.Catch,
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
	f.ctrls = append(f.ctrls, t)
	return t
}

func (f *fn) beginTryTable(in *wasm.Instr) {
	f.beginTryCommon(in, ctrlTry)
}

func (f *fn) endTryTable(t *ctrl) {
	c := f.c
	if !t.unreachable {
		vals := f.stack[len(f.stack)-len(t.results):]
		f.assignTo(t.base, t.results, vals)
		t.endReached = true
		if t.handlerUsed {
			t.used = true
			f.emit("goto %s", t.label)
		}
	}
	f.ctrls = f.ctrls[:len(f.ctrls)-1]
	if t.handlerUsed {
		f.emit("%s:", t.handlerLabel())
		hasAll := false
		for _, cc := range t.catches {
			target := f.ctrls[len(f.ctrls)-1-int(cc.Label)]
			ts := target.labelTypes()
			switch cc.Kind {
			case wasm.CatchPlain, wasm.CatchRef:
				f.emit("if m.exn.Tag == %s {", c.tagRef(cc.Tag))
				tft := c.m.Types[c.m.TagTypeIdx(cc.Tag)].Comp.Func
				for i, p := range tft.Params {
					f.emit("%s = %s", f.catchDest(target, i), c.conv(fmt.Sprintf("m.exn.Values[%d].(%s)", i, c.goType(p)), p, ts[i]))
				}
				if cc.Kind == wasm.CatchRef {
					f.emit("%s = m.exn", f.catchDest(target, len(tft.Params)))
				}
				f.emit("m.exn = nil")
				f.jump(target)
				f.emit("}")
			case wasm.CatchAll, wasm.CatchAllRef:
				if cc.Kind == wasm.CatchAllRef {
					f.emit("%s = m.exn", f.catchDest(target, 0))
				}
				f.emit("m.exn = nil")
				f.jump(target)
				hasAll = true
			}
			if target.kind != ctrlLoop && target.kind != ctrlFunc {
				target.endReached = true
			}
			if hasAll {
				break
			}
		}
		if !hasAll {
			f.unwind()
		}
	}
	if t.used {
		f.emit("%s:", t.label)
	}
	f.finishBlock(t)
}

// --- legacy exception handling (try / catch / catch_all / delegate / rethrow) ---

func (f *fn) beginLegacyTry(in *wasm.Instr) {
	t := f.beginTryCommon(in, ctrlLegacy)
	t.legacy = &legacyTry{inBody: true, delegate: -1, exnVar: "x" + t.label[1:]}
}

// endLegacySection ends the body or the current catch handler.
func (f *fn) endLegacySection(t *ctrl) {
	if !t.unreachable {
		vals := f.stack[len(f.stack)-len(t.results):]
		f.assignTo(t.base, t.results, vals)
		t.endReached = true
		t.used = true
		f.emit("goto %s", t.label)
	}
	t.legacy.inBody = false
}

func (f *fn) legacyCatch(in *wasm.Instr) {
	c := f.c
	t := f.top()
	lt := t.legacy
	f.endLegacySection(t)
	if !t.handlerUsed {
		// nothing in the body can throw: the handler is dead code
		f.stack = f.stack[:t.base]
		t.unreachable = true
		return
	}
	n := len(lt.clauses) + 1
	clause := fmt.Sprintf("C%s_%d", t.label[1:], n)
	f.declare(lt.exnVar, "*rt.Exception")
	f.stack = f.stack[:t.base]
	t.unreachable = false
	f.emit("%s:", clause)
	if in.Op == wasm.OpCatch {
		lt.clauses = append(lt.clauses, fmt.Sprintf("if m.exn.Tag == %s {\n%s = m.exn\nm.exn = nil\ngoto %s\n}", c.tagRef(in.A), lt.exnVar, clause))
		tft := c.m.Types[c.m.TagTypeIdx(in.A)].Comp.Func
		for i, p := range tft.Params {
			f.pushEager(p, fmt.Sprintf("%s.Values[%d].(%s)", lt.exnVar, i, c.goType(p)))
		}
	} else {
		lt.clauses = append(lt.clauses, fmt.Sprintf("%s = m.exn\nm.exn = nil\ngoto %s", lt.exnVar, clause))
		lt.hasAll = true
	}
}

func (f *fn) legacyDelegate(in *wasm.Instr) {
	t := f.top()
	f.endLegacySection(t)
	// exceptions from the body go to the handler of label in.A (relative
	// to the try's enclosing context)
	t.legacy.delegate = len(f.ctrls) - 2 - int(in.A)
	f.endLegacyTry(t)
}

func (f *fn) legacyRethrow(in *wasm.Instr) {
	t := f.ctrls[len(f.ctrls)-1-int(in.A)]
	f.raise(t.legacy.exnVar)
}

func (f *fn) endLegacyTry(t *ctrl) {
	lt := t.legacy
	if lt.delegate < 0 && (lt.inBody || !t.unreachable) {
		f.endLegacySection(t)
	}
	f.ctrls = f.ctrls[:len(f.ctrls)-1]
	if t.handlerUsed {
		f.emit("%s:", t.handlerLabel())
		if lt.delegate >= 0 {
			f.unwindFrom(lt.delegate)
		} else {
			for _, cl := range lt.clauses {
				f.emit("%s", cl)
			}
			if !lt.hasAll {
				f.unwind()
			}
		}
	}
	if t.used {
		f.emit("%s:", t.label)
	}
	f.finishBlock(t)
}

// --- module-level support ---

// computeExn decides whether the module uses the pending-exception scheme.
func (c *compiler) computeExn() {
	for _, code := range c.m.Codes {
		for _, in := range code.Body {
			if in.Op == wasm.OpTryTable || in.Op == wasm.OpTry {
				c.usesExn = true
				return
			}
		}
	}
}

// guardName returns a helper method that calls a function value of type ft
// that may panic with a wasm exception (e.g. one from another instance),
// converting the panic into m.exn.
func (c *compiler) guardName(ft *wasm.FuncType) string {
	sig := c.goFuncSig(ft)
	if n, ok := c.guards[sig]; ok {
		return n
	}
	n := c.memberIdent(fmt.Sprintf("guard%d", len(c.guards)))
	c.guards[sig] = n
	var ps, args []string
	ps = append(ps, "fn "+sig, "d int")
	args = append(args, "d")
	for i, p := range ft.Params {
		ps = append(ps, fmt.Sprintf("a%d %s", i, c.goType(p)))
		args = append(args, fmt.Sprintf("a%d", i))
	}
	call := fmt.Sprintf("fn(%s)", strings.Join(args, ", "))
	if len(ft.Results) > 0 {
		call = "return " + call
	}
	fmt.Fprintf(&c.extra, "func (m *Instance) %s(%s)%s {\ndefer m.catchExn()\n%s\n}\n\n", n, strings.Join(ps, ", "), goResults(c, ft.Results), call)
	return n
}

func (c *compiler) writeExnSupport(w *strings.Builder) {
	if !c.usesExn {
		return
	}
	w.WriteString(`// catchExn is deferred around calls that may panic with a wasm exception
// (host imports, foreign function references); it makes it pending.
func (m *Instance) catchExn() {
	if r := recover(); r != nil {
		if e, ok := r.(*rt.Exception); ok {
			m.exn = e
			return
		}
		panic(r)
	}
}

`)
}
