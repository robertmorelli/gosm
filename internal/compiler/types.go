package compiler

import (
	"fmt"
	"strings"

	"gosm/internal/wasm"
	"gosm/rt"
)

// universe holds the Go representation of every defined type of all modules
// compiled into one package. Types are identified by their iso-recursive
// structural key, so equivalent types of different modules share one Go
// type and one RTT.
type universe struct {
	types    map[string]*typeInfo
	order    []*typeInfo
	pkgNames map[string]bool
}

func newUniverse() *universe {
	return &universe{types: map[string]*typeInfo{}, pkgNames: map[string]bool{}}
}

// typeInfo describes a canonical defined type.
type typeInfo struct {
	c       *compiler // compiler of the defining module
	midx    int       // type index in the defining module
	key     string
	kind    byte
	final   bool
	super   *typeInfo // declared supertype, nil if none
	root    *typeInfo
	header  bool   // struct/array carries an rt.Object header
	goName  string // Go type name for struct/array types
	rtt     string // name of the package-level *rt.RTT variable
	fields  []fieldInfo
	hasSub  bool
	names   []string // name-section names of all equivalent types
	laidOut bool
}

type fieldInfo struct {
	name      string // Go field name (promoted, unique across the chain)
	storage   string // Go storage type (inherited from the root declaration)
	storageVT wasm.ValType
	ft        wasm.FieldType // the field as declared in this type
}

// computeKeys computes the structural key of every type of the module.
func (c *compiler) computeKeys() {
	m := c.m
	c.keys = make([]string, len(m.Types))
	for _, g := range m.RecGroups {
		sers := make([]string, g.Len)
		for i := 0; i < g.Len; i++ {
			sers[i] = c.subSer(g, g.Start+i)
		}
		gk := rt.GroupKey(sers)
		for i := 0; i < g.Len; i++ {
			c.keys[g.Start+i] = rt.TypeKey(gk, i)
		}
	}
}

// register adds the module's types to the universe.
func (u *universe) register(c *compiler) {
	m := c.m
	for i := range m.Types {
		k := c.keys[i]
		ti, ok := u.types[k]
		if !ok {
			st := &m.Types[i]
			ti = &typeInfo{c: c, midx: i, key: k, kind: st.Comp.Kind, final: st.Final}
			if len(st.Supers) > 0 {
				ti.super = u.types[c.keys[st.Supers[0]]]
			}
			u.types[k] = ti
			u.order = append(u.order, ti)
		}
		if n, ok := m.Names.Types[uint32(i)]; ok && n != "" {
			ti.names = append(ti.names, n)
		}
	}
}

// finish assigns Go names, headers and field layouts once all modules are
// registered.
func (u *universe) finish() {
	for _, ti := range u.order {
		r := ti
		for r.super != nil {
			r = r.super
		}
		ti.root = r
		if ti.super != nil {
			ti.super.hasSub = true
		}
	}
	multi := map[*typeInfo]bool{}
	for _, ti := range u.order {
		if ti.super != nil {
			multi[ti.root] = true
		}
	}
	for i, ti := range u.order {
		ti.header = multi[ti.root] && ti.kind != wasm.KindFunc
		name := ""
		if len(ti.names) > 0 {
			name = ti.names[0]
		}
		switch ti.kind {
		case wasm.KindStruct, wasm.KindArray:
			if name == "" {
				if ti.kind == wasm.KindStruct {
					name = fmt.Sprintf("Struct%d", i)
				} else {
					name = fmt.Sprintf("Array%d", i)
				}
			}
			ti.goName = u.pkgIdent(exportIdent(name))
			ti.rtt = u.pkgIdent("rtt" + ti.goName)
		case wasm.KindFunc:
			if name != "" {
				ti.rtt = u.pkgIdent("rtt" + exportIdent(name))
			} else {
				ti.rtt = u.pkgIdent(fmt.Sprintf("rttFunc%d", i))
			}
		}
	}
	for _, ti := range u.order {
		ti.layoutFields()
	}
}

func (u *universe) pkgIdent(base string) string {
	name := base
	for k := 2; u.pkgNames[name]; k++ {
		name = fmt.Sprintf("%s%d", base, k)
	}
	u.pkgNames[name] = true
	return name
}

func (ti *typeInfo) comp() *wasm.CompType { return &ti.c.m.Types[ti.midx].Comp }

func (ti *typeInfo) layoutFields() {
	if ti.laidOut || ti.kind == wasm.KindFunc {
		return
	}
	ti.laidOut = true
	c := ti.c
	var inherited []fieldInfo
	taken := map[string]bool{"Object": true, "RTT": true, "WasmType": true, "WasmHeader": true, "WasmLen": true}
	if ti.super != nil {
		ti.super.layoutFields()
		inherited = ti.super.fields
		for s := ti.super; s != nil; s = s.super {
			taken[s.goName] = true
		}
	}
	for _, f := range inherited {
		taken[f.name] = true
	}
	fields := ti.comp().Fields
	out := make([]fieldInfo, 0, len(fields))
	for i, ft := range fields {
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
			if fn, ok := c.m.Names.Fields[uint32(ti.midx)][uint32(i)]; ok && fn != "" {
				name = exportIdent(fn)
			}
		}
		base := name
		for k := 2; taken[name]; k++ {
			name = fmt.Sprintf("%s_%d", base, k)
		}
		taken[name] = true
		out = append(out, fieldInfo{name: name, storage: c.storageGo(ft), storageVT: ft.Type, ft: ft})
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

func (c *compiler) info(typeIdx uint32) *typeInfo { return c.u.types[c.keys[typeIdx]] }

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
		tb := c.info(uint32(b))
		for t := c.info(uint32(a)); t != nil; t = t.super {
			if t == tb {
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
	if e == "nil" || e == "any(nil)" || strings.HasSuffix(e, ")(nil)") {
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
