// Package validate implements WebAssembly 3.0 module validation.
package validate

import (
	"fmt"

	"gosm/internal/wasm"
)

// Error is a validation error.
type Error struct{ Msg string }

func (e *Error) Error() string { return "invalid module: " + e.Msg }

type verr struct{ msg string }

func fail(format string, args ...any) {
	panic(verr{fmt.Sprintf(format, args...)})
}

type checker struct {
	m      *wasm.Module
	canon  []int
	ntypes int // number of types in scope

	funcs   []uint32 // type index per function
	tables  []wasm.TableType
	mems    []wasm.MemType
	globals []wasm.GlobalType
	tags    []uint32
	refs    map[uint32]bool // declared function references
}

// Validate checks that m is a valid WebAssembly 3.0 module (including GC,
// typed function references, exception handling with exnref plus the legacy
// try/catch/delegate/rethrow instructions, tail calls, multi-memory,
// memory64/table64, extended-const, bulk memory, reference types, SIMD and
// relaxed SIMD).
func Validate(m *wasm.Module) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if ve, ok := r.(verr); ok {
				err = &Error{ve.msg}
				return
			}
			panic(r)
		}
	}()
	c := &checker{m: m, refs: map[uint32]bool{}}
	c.validate()
	return nil
}

func (c *checker) funcType(typeIdx uint32) *wasm.FuncType {
	if int(typeIdx) >= c.ntypes {
		fail("unknown type %d", typeIdx)
	}
	st := &c.m.Types[typeIdx]
	if st.Comp.Kind != wasm.KindFunc {
		fail("type mismatch: type %d is not a function type", typeIdx)
	}
	return &st.Comp.Func
}

func (c *checker) structType(typeIdx uint32) []wasm.FieldType {
	if int(typeIdx) >= c.ntypes {
		fail("unknown type %d", typeIdx)
	}
	st := &c.m.Types[typeIdx]
	if st.Comp.Kind != wasm.KindStruct {
		fail("type mismatch: type %d is not a struct type", typeIdx)
	}
	return st.Comp.Fields
}

func (c *checker) arrayType(typeIdx uint32) wasm.FieldType {
	if int(typeIdx) >= c.ntypes {
		fail("unknown type %d", typeIdx)
	}
	st := &c.m.Types[typeIdx]
	if st.Comp.Kind != wasm.KindArray {
		fail("type mismatch: type %d is not an array type", typeIdx)
	}
	return st.Comp.Fields[0]
}

func (c *checker) tagType(t uint32) *wasm.FuncType {
	if int(t) >= len(c.tags) {
		fail("unknown tag %d", t)
	}
	return c.funcType(c.tags[t])
}

func (c *checker) table(t uint32) wasm.TableType {
	if int(t) >= len(c.tables) {
		fail("unknown table %d", t)
	}
	return c.tables[t]
}

func (c *checker) mem(i uint32) wasm.MemType {
	if int(i) >= len(c.mems) {
		fail("unknown memory %d", i)
	}
	return c.mems[i]
}

func (c *checker) funcTypeOf(f uint32) *wasm.FuncType {
	if int(f) >= len(c.funcs) {
		fail("unknown function %d", f)
	}
	return c.funcType(c.funcs[f])
}

func addrType(l wasm.Limits) wasm.ValType {
	if l.Is64 {
		return wasm.TI64
	}
	return wasm.TI32
}

func (c *checker) checkTableType(tt wasm.TableType) {
	c.checkVal(tt.Elem)
	l := tt.Limits
	if l.HasMax && l.Min > l.Max {
		fail("size minimum must not be greater than maximum")
	}
	if !l.Is64 && (l.Min > 0xFFFFFFFF || (l.HasMax && l.Max > 0xFFFFFFFF)) {
		fail("table size must be at most 2^32-1")
	}
}

func (c *checker) checkMemType(mt wasm.MemType) {
	l := mt.Limits
	limit := uint64(1) << 16
	msg := "memory size must be at most 65536 pages (4GiB)"
	if l.Is64 {
		limit = 1 << 48
		msg = "memory size must be at most 2^48 pages"
	}
	if l.Min > limit || (l.HasMax && l.Max > limit) {
		fail("%s", msg)
	}
	if l.HasMax && l.Min > l.Max {
		fail("size minimum must not be greater than maximum")
	}
	if l.Shared && !l.HasMax {
		fail("shared memory must have maximum")
	}
}

func (c *checker) checkTagType(typeIdx uint32) {
	ft := c.funcType(typeIdx)
	if len(ft.Results) != 0 {
		fail("non-empty tag result type")
	}
}

func (c *checker) validate() {
	m := c.m
	c.validateTypes()

	for _, im := range m.Imports {
		switch im.Kind {
		case wasm.ExtFunc:
			c.funcType(im.TypeIdx)
			c.funcs = append(c.funcs, im.TypeIdx)
		case wasm.ExtTable:
			c.checkTableType(im.Table)
			c.tables = append(c.tables, im.Table)
		case wasm.ExtMem:
			c.checkMemType(im.Mem)
			c.mems = append(c.mems, im.Mem)
		case wasm.ExtGlobal:
			c.checkVal(im.Global.Type)
			c.globals = append(c.globals, im.Global)
		case wasm.ExtTag:
			c.checkTagType(im.TypeIdx)
			c.tags = append(c.tags, im.TypeIdx)
		}
	}
	for _, f := range m.Funcs {
		c.funcType(f)
		c.funcs = append(c.funcs, f)
	}
	for _, t := range m.Tables {
		c.tables = append(c.tables, t.Type)
	}
	c.mems = append(c.mems, m.Mems...)
	nImpGlobals := len(c.globals)
	for _, g := range m.Globals {
		c.globals = append(c.globals, g.Type)
	}
	for _, t := range m.Tags {
		c.checkTagType(t)
		c.tags = append(c.tags, t)
	}

	// Declared function references: every ref.func outside function bodies.
	addRefs := func(e []wasm.Instr) {
		for _, in := range e {
			if in.Op == wasm.OpRefFunc {
				c.refs[in.A] = true
			}
		}
	}
	for _, t := range m.Tables {
		addRefs(t.Init)
	}
	for _, g := range m.Globals {
		addRefs(g.Init)
	}
	for _, e := range m.Elems {
		addRefs(e.Offset)
		for _, x := range e.Inits {
			addRefs(x)
		}
	}
	for _, d := range m.Datas {
		addRefs(d.Offset)
	}
	for _, e := range m.Exports {
		if e.Kind == wasm.ExtFunc {
			c.refs[e.Index] = true
		}
	}

	for _, t := range m.Tables {
		c.checkTableType(t.Type)
		if t.Init != nil {
			c.constExpr(t.Init, t.Type.Elem, nImpGlobals)
		} else if !t.Type.Elem.Nullable {
			fail("type mismatch: non-defaultable table type requires initializer")
		}
	}
	for _, mt := range m.Mems {
		c.checkMemType(mt)
	}
	for i, g := range m.Globals {
		c.checkVal(g.Type.Type)
		c.constExpr(g.Init, g.Type.Type, nImpGlobals+i)
	}

	for _, e := range m.Elems {
		c.checkVal(e.Type)
		for _, x := range e.Inits {
			c.constExpr(x, e.Type, len(c.globals))
		}
		if e.Mode == wasm.SegActive {
			tt := c.table(e.Table)
			c.constExpr(e.Offset, addrType(tt.Limits), len(c.globals))
			if !c.matchVal(e.Type, tt.Elem) {
				fail("type mismatch: element segment type %s does not match table type %s", e.Type, tt.Elem)
			}
		}
	}
	if m.DataCount != nil && int(*m.DataCount) != len(m.Datas) {
		fail("data count and data section have inconsistent lengths")
	}
	for _, d := range m.Datas {
		if d.Mode == wasm.SegActive {
			mt := c.mem(d.Mem)
			c.constExpr(d.Offset, addrType(mt.Limits), len(c.globals))
		}
	}
	if m.Start != nil {
		ft := c.funcTypeOf(*m.Start)
		if len(ft.Params) != 0 || len(ft.Results) != 0 {
			fail("start function must have type [] -> []")
		}
	}
	names := map[string]bool{}
	for _, e := range m.Exports {
		if names[e.Name] {
			fail("duplicate export name %q", e.Name)
		}
		names[e.Name] = true
		switch e.Kind {
		case wasm.ExtFunc:
			c.funcTypeOf(e.Index)
		case wasm.ExtTable:
			c.table(e.Index)
		case wasm.ExtMem:
			c.mem(e.Index)
		case wasm.ExtGlobal:
			if int(e.Index) >= len(c.globals) {
				fail("unknown global %d", e.Index)
			}
		case wasm.ExtTag:
			if int(e.Index) >= len(c.tags) {
				fail("unknown tag %d", e.Index)
			}
		default:
			fail("malformed export kind")
		}
	}
	if len(m.Codes) != len(m.Funcs) {
		fail("function and code section have inconsistent lengths")
	}
	for i := range m.Codes {
		fidx := m.NumImportedFuncs + i
		c.funcBody(uint32(fidx), &m.Codes[i])
	}
}
