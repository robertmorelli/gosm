package compiler

import (
	"fmt"
	"strings"

	"gosm/internal/wasm"
	"gosm/rt"
)

// typeInfo describes a canonical defined type.
type typeInfo struct {
	idx    int // canonical type index
	kind   byte
	super  int // canonical index of declared supertype, -1 if none
	root   int
	header bool   // struct/array carries an rt.Object header
	goName string // Go type name for struct/array types
	rtt    string // name of the package-level *rt.RTT variable
	fields []fieldInfo
	hasSub bool
}

type fieldInfo struct {
	name    string // Go field name (promoted, unique across the chain)
	storage string // Go storage type (inherited from the root declaration)
	ft      wasm.FieldType
	owner   int // canonical type index that declares the Go field
}

// computeTypes canonicalizes types (iso-recursive equivalence) and assigns
// Go representations.
func (c *compiler) computeTypes() {
	m := c.m
	n := len(m.Types)
	c.keys = make([]string, n)
	c.canon = make([]int, n)
	first := map[string]int{}
	for _, g := range m.RecGroups {
		sers := make([]string, g.Len)
		for i := 0; i < g.Len; i++ {
			sers[i] = c.subSer(g, g.Start+i)
		}
		gk := rt.GroupKey(sers)
		for i := 0; i < g.Len; i++ {
			k := rt.TypeKey(gk, i)
			c.keys[g.Start+i] = k
			if f, ok := first[k]; ok {
				c.canon[g.Start+i] = f
			} else {
				first[k] = g.Start + i
				c.canon[g.Start+i] = g.Start + i
			}
		}
	}
	c.tinfo = map[int]*typeInfo{}
	for i := 0; i < n; i++ {
		if c.canon[i] != i {
			continue
		}
		st := &m.Types[i]
		ti := &typeInfo{idx: i, kind: st.Comp.Kind, super: -1}
		if len(st.Supers) > 0 {
			ti.super = c.canon[st.Supers[0]]
		}
		c.tinfo[i] = ti
	}
	// roots and hierarchy membership
	for i := 0; i < n; i++ {
		ti := c.tinfo[i]
		if ti == nil {
			continue
		}
		r := i
		for c.tinfo[r].super >= 0 {
			r = c.tinfo[r].super
		}
		ti.root = r
		if ti.super >= 0 {
			c.tinfo[ti.super].hasSub = true
		}
	}
	multi := map[int]bool{}
	for _, ti := range c.tinfo {
		if ti.super >= 0 {
			multi[ti.root] = true
		}
	}
	// Go names
	for i := 0; i < n; i++ {
		ti := c.tinfo[i]
		if ti == nil {
			continue
		}
		ti.header = multi[ti.root] && ti.kind != wasm.KindFunc
		name := ""
		for j := 0; j < n; j++ { // first named member of the class
			if c.canon[j] == i {
				if s, ok := m.Names.Types[uint32(j)]; ok && s != "" {
					name = s
					break
				}
			}
		}
		switch ti.kind {
		case wasm.KindStruct:
			if name == "" {
				name = fmt.Sprintf("Struct%d", i)
			}
			ti.goName = c.pkgIdent(exportIdent(name))
			ti.rtt = c.pkgIdent("rtt" + ti.goName)
		case wasm.KindArray:
			if name == "" {
				name = fmt.Sprintf("Array%d", i)
			}
			ti.goName = c.pkgIdent(exportIdent(name))
			ti.rtt = c.pkgIdent("rtt" + ti.goName)
		case wasm.KindFunc:
			if name != "" {
				ti.rtt = c.pkgIdent("rtt" + exportIdent(name))
			} else {
				ti.rtt = c.pkgIdent(fmt.Sprintf("rttFunc%d", i))
			}
		}
	}
	// fields, in index order (supertypes always precede subtypes)
	for i := 0; i < n; i++ {
		ti := c.tinfo[i]
		if ti == nil || ti.kind == wasm.KindFunc {
			continue
		}
		c.layoutFields(ti)
	}
}

func (c *compiler) layoutFields(ti *typeInfo) {
	if ti.fields != nil || ti.kind == wasm.KindFunc {
		return
	}
	st := &c.m.Types[ti.idx]
	var inherited []fieldInfo
	taken := map[string]bool{"Object": true, "RTT": true, "WasmType": true, "WasmHeader": true}
	if ti.super >= 0 {
		sup := c.tinfo[ti.super]
		c.layoutFields(sup)
		inherited = sup.fields
		for s := ti.super; s >= 0; s = c.tinfo[s].super {
			taken[c.tinfo[s].goName] = true
		}
	}
	for _, f := range inherited {
		taken[f.name] = true
	}
	out := make([]fieldInfo, 0, len(st.Comp.Fields))
	for i, ft := range st.Comp.Fields {
		if i < len(inherited) {
			fi := inherited[i]
			fi.ft = ft
			out = append(out, fi)
			continue
		}
		var name string
		if ti.kind == wasm.KindArray {
			name = "Elems"
		} else {
			name = fmt.Sprintf("F%d", i)
			if fn, ok := c.m.Names.Fields[uint32(ti.idx)][uint32(i)]; ok && fn != "" {
				name = exportIdent(fn)
			}
		}
		base := name
		for k := 2; taken[name]; k++ {
			name = fmt.Sprintf("%s_%d", base, k)
		}
		taken[name] = true
		out = append(out, fieldInfo{name: name, storage: c.storageGo(ft), ft: ft, owner: ti.idx})
	}
	ti.fields = out
}

func (c *compiler) storageGo(ft wasm.FieldType) string {
	switch ft.Type.Kind {
	case wasm.I8:
		return "int8"
	case wasm.I16:
		return "int16"
	}
	return c.goType(ft.Type)
}

func (c *compiler) heapSer(h wasm.HeapType, g wasm.RecGroup) string {
	if h.IsAbstract() {
		return h.String()
	}
	i := int(h)
	if i >= g.Start && i < g.Start+g.Len {
		return fmt.Sprintf("rec.%d", i-g.Start)
	}
	return "<" + c.keys[i] + ">"
}

func (c *compiler) valSer(v wasm.ValType, g wasm.RecGroup) string {
	switch v.Kind {
	case wasm.I32:
		return "i32"
	case wasm.I64:
		return "i64"
	case wasm.F32:
		return "f32"
	case wasm.F64:
		return "f64"
	case wasm.V128:
		return "v128"
	case wasm.I8:
		return "i8"
	case wasm.I16:
		return "i16"
	}
	if v.Nullable {
		return "ref null " + c.heapSer(v.Heap, g)
	}
	return "ref " + c.heapSer(v.Heap, g)
}

func (c *compiler) subSer(g wasm.RecGroup, i int) string {
	st := &c.m.Types[i]
	sup := ""
	if len(st.Supers) > 0 {
		sup = c.heapSer(wasm.HeapType(st.Supers[0]), g)
	}
	var comp string
	switch st.Comp.Kind {
	case wasm.KindFunc:
		var ps, rs []string
		for _, p := range st.Comp.Func.Params {
			ps = append(ps, c.valSer(p, g))
		}
		for _, r := range st.Comp.Func.Results {
			rs = append(rs, c.valSer(r, g))
		}
		comp = rt.FuncSer(ps, rs)
	case wasm.KindStruct, wasm.KindArray:
		var fs []string
		for _, f := range st.Comp.Fields {
			s := c.valSer(f.Type, g)
			if f.Mutable {
				s = "mut " + s
			}
			fs = append(fs, s)
		}
		if st.Comp.Kind == wasm.KindStruct {
			comp = "struct(" + strings.Join(fs, ",") + ")"
		} else {
			comp = "array(" + strings.Join(fs, ",") + ")"
		}
	}
	return rt.SubtypeSer(st.Final, sup, comp)
}

// valKey is a module-independent description of a value type, used for
// import/export type checks.
func (c *compiler) valKey(v wasm.ValType) string {
	if v.IsRef() && !v.Heap.IsAbstract() {
		s := "<" + c.keys[int(v.Heap)] + ">"
		if v.Nullable {
			return "ref null " + s
		}
		return "ref " + s
	}
	return c.valSer(v, wasm.RecGroup{Start: -1})
}

func (c *compiler) info(typeIdx uint32) *typeInfo { return c.tinfo[c.canon[typeIdx]] }

// goType maps a value type to its Go type.
func (c *compiler) goType(v wasm.ValType) string {
	switch v.Kind {
	case wasm.I32:
		return "int32"
	case wasm.I64:
		return "int64"
	case wasm.F32:
		return "float32"
	case wasm.F64:
		return "float64"
	case wasm.V128:
		return "rt.V128"
	case wasm.I8, wasm.I16:
		return "int32"
	}
	switch v.Heap {
	case wasm.HeapFunc, wasm.HeapNoFunc:
		return "*rt.Func"
	case wasm.HeapExn, wasm.HeapNoExn:
		return "*rt.Exception"
	}
	if v.Heap.IsAbstract() {
		return "any"
	}
	ti := c.info(uint32(v.Heap))
	if ti.kind == wasm.KindFunc {
		return "*rt.Func"
	}
	return "*" + ti.goName
}

// topHeap returns the top of the hierarchy a heap type belongs to.
func (c *compiler) topHeap(h wasm.HeapType) wasm.HeapType {
	switch h {
	case wasm.HeapFunc, wasm.HeapNoFunc:
		return wasm.HeapFunc
	case wasm.HeapExtern, wasm.HeapNoExtern:
		return wasm.HeapExtern
	case wasm.HeapExn, wasm.HeapNoExn:
		return wasm.HeapExn
	}
	if h.IsAbstract() {
		return wasm.HeapAny
	}
	if c.info(uint32(h)).kind == wasm.KindFunc {
		return wasm.HeapFunc
	}
	return wasm.HeapAny
}

// isSubHeap reports whether a <: b (static, within this module).
func (c *compiler) isSubHeap(a, b wasm.HeapType) bool {
	if a == b {
		return true
	}
	if !a.IsAbstract() && !b.IsAbstract() {
		ca, cb := c.canon[a], c.canon[b]
		for t := ca; t >= 0; t = c.tinfo[t].super {
			if t == cb {
				return true
			}
		}
		return false
	}
	switch b {
	case wasm.HeapAny:
		return c.topHeap(a) == wasm.HeapAny
	case wasm.HeapEq:
		if a == wasm.HeapI31 || a == wasm.HeapStruct || a == wasm.HeapArray || a == wasm.HeapNone || a == wasm.HeapEq {
			return true
		}
		return !a.IsAbstract() && c.info(uint32(a)).kind != wasm.KindFunc
	case wasm.HeapStruct:
		return a == wasm.HeapNone || (!a.IsAbstract() && c.info(uint32(a)).kind == wasm.KindStruct)
	case wasm.HeapArray:
		return a == wasm.HeapNone || (!a.IsAbstract() && c.info(uint32(a)).kind == wasm.KindArray)
	case wasm.HeapFunc:
		return a == wasm.HeapNoFunc || (!a.IsAbstract() && c.info(uint32(a)).kind == wasm.KindFunc)
	case wasm.HeapExtern:
		return a == wasm.HeapNoExtern
	case wasm.HeapExn:
		return a == wasm.HeapNoExn
	}
	if !b.IsAbstract() {
		top := c.topHeap(b)
		return (top == wasm.HeapAny && a == wasm.HeapNone) || (top == wasm.HeapFunc && a == wasm.HeapNoFunc)
	}
	return false
}

// conv converts expression e of static type from to the Go representation
// of type to. Up- and (statically known) down-casts are supported.
func (c *compiler) conv(e string, from, to wasm.ValType) string {
	gf, gt := c.goType(from), c.goType(to)
	if gf == gt {
		return e
	}
	if e == "nil" || strings.HasSuffix(e, ")(nil)") {
		return c.nullOf(to)
	}
	fromConcrete := strings.HasPrefix(gf, "*") && gf != "*rt.Func" && gf != "*rt.Exception"
	toConcrete := strings.HasPrefix(gt, "*") && gt != "*rt.Func" && gt != "*rt.Exception"
	switch {
	case fromConcrete && toConcrete:
		return fmt.Sprintf("rt.ViewAs[%s](%s)", gt[1:], e)
	case fromConcrete && gt == "any":
		if c.info(uint32(from.Heap)).header {
			return fmt.Sprintf("rt.Box(%s)", e)
		}
		return fmt.Sprintf("rt.AnyOf(%s)", e)
	case gf == "any" && toConcrete:
		if c.info(uint32(to.Heap)).header {
			return fmt.Sprintf("rt.AsObj[%s](%s)", gt[1:], e)
		}
		return fmt.Sprintf("rt.As[%s](%s)", gt[1:], e)
	}
	// e.g. nullref (any) flowing into a funcref/exnref position: only null
	// can flow between hierarchies.
	return c.nullOf(to)
}

func (c *compiler) nullOf(t wasm.ValType) string {
	g := c.goType(t)
	if g == "any" {
		return "any(nil)"
	}
	return "(" + g + ")(nil)"
}

func (c *compiler) zeroOf(t wasm.ValType) string {
	switch t.Kind {
	case wasm.I32:
		return "int32(0)"
	case wasm.I64:
		return "int64(0)"
	case wasm.F32:
		return "float32(0)"
	case wasm.F64:
		return "float64(0)"
	case wasm.V128:
		return "rt.V128{}"
	case wasm.I8, wasm.I16:
		return "0"
	}
	return c.nullOf(t)
}
