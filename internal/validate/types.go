package validate

import (
	"strconv"
	"strings"

	"gosm/internal/wasm"
)

// heapBot is the internal bottom heap type: the heap type of a reference
// popped from a polymorphic (unreachable) stack. It matches every heap type.
const heapBot wasm.HeapType = -0x40

// bot is the unknown value type popped from a polymorphic stack.
var bot = wasm.ValType{}

func isBot(t wasm.ValType) bool { return t.Kind == 0 }

// validateTypes checks every rec group in order and computes the canonical
// (iso-recursive) index of every defined type.
func (c *checker) validateTypes() {
	m := c.m
	c.canon = make([]int, len(m.Types))
	first := map[string]int{}
	for _, g := range m.RecGroups {
		end := g.Start + g.Len
		// Within the group, types may refer to any type of the group.
		c.ntypes = end
		for i := g.Start; i < end; i++ {
			c.checkSubTypeRefs(&m.Types[i])
		}
		var b strings.Builder
		for i := g.Start; i < end; i++ {
			c.serSub(&b, &m.Types[i], g)
			b.WriteByte(';')
		}
		key := b.String()
		f, ok := first[key]
		if !ok {
			f = g.Start
			first[key] = f
		}
		for i := g.Start; i < end; i++ {
			c.canon[i] = f + (i - g.Start)
		}
		for i := g.Start; i < end; i++ {
			c.checkSubType(i)
		}
	}
	c.ntypes = len(m.Types)
}

func (c *checker) checkSubTypeRefs(st *wasm.SubType) {
	for _, s := range st.Supers {
		if int(s) >= c.ntypes {
			fail("unknown type %d", s)
		}
	}
	ct := &st.Comp
	switch ct.Kind {
	case wasm.KindFunc:
		for _, t := range ct.Func.Params {
			c.checkVal(t)
		}
		for _, t := range ct.Func.Results {
			c.checkVal(t)
		}
	default:
		for _, f := range ct.Fields {
			if !f.Packed() {
				c.checkVal(f.Type)
			}
		}
	}
}

func (c *checker) checkSubType(i int) {
	st := &c.m.Types[i]
	if len(st.Supers) > 1 {
		fail("sub type %d has too many supertypes", i)
	}
	for _, s := range st.Supers {
		if int(s) >= i {
			if int(s) >= len(c.m.Types) {
				fail("unknown type %d", s)
			}
			fail("sub type %d: supertype %d must be defined earlier", i, s)
		}
		sup := &c.m.Types[s]
		if sup.Final {
			fail("sub type %d does not match final super type %d", i, s)
		}
		if !c.matchComp(&st.Comp, &sup.Comp) {
			fail("sub type %d does not match super type %d", i, s)
		}
	}
}

func (c *checker) serHeap(b *strings.Builder, h wasm.HeapType, g wasm.RecGroup) {
	if h.IsAbstract() {
		b.WriteString(h.String())
		return
	}
	i := int(h)
	if i >= g.Start && i < g.Start+g.Len {
		b.WriteString("r")
		b.WriteString(strconv.Itoa(i - g.Start))
		return
	}
	b.WriteString("c")
	b.WriteString(strconv.Itoa(c.canon[i]))
}

func (c *checker) serVal(b *strings.Builder, t wasm.ValType, g wasm.RecGroup) {
	b.WriteString(strconv.Itoa(int(t.Kind)))
	if t.Kind == wasm.Ref {
		if t.Nullable {
			b.WriteString("n")
		}
		b.WriteString("(")
		c.serHeap(b, t.Heap, g)
		b.WriteString(")")
	}
	b.WriteString(",")
}

func (c *checker) serSub(b *strings.Builder, st *wasm.SubType, g wasm.RecGroup) {
	if st.Final {
		b.WriteString("F")
	}
	b.WriteString("<")
	for _, s := range st.Supers {
		c.serHeap(b, wasm.HeapType(s), g)
		b.WriteString(",")
	}
	b.WriteString(">")
	b.WriteString(strconv.Itoa(int(st.Comp.Kind)))
	switch st.Comp.Kind {
	case wasm.KindFunc:
		b.WriteString("(")
		for _, t := range st.Comp.Func.Params {
			c.serVal(b, t, g)
		}
		b.WriteString(")(")
		for _, t := range st.Comp.Func.Results {
			c.serVal(b, t, g)
		}
		b.WriteString(")")
	default:
		b.WriteString("{")
		for _, f := range st.Comp.Fields {
			if f.Mutable {
				b.WriteString("m")
			}
			c.serVal(b, f.Type, g)
		}
		b.WriteString("}")
	}
}

// checkVal checks that type indices in t are in bounds.
func (c *checker) checkVal(t wasm.ValType) {
	if t.Kind == wasm.Ref {
		c.checkHeap(t.Heap)
	}
}

func (c *checker) checkHeap(h wasm.HeapType) {
	if !h.IsAbstract() && int(h) >= c.ntypes {
		fail("unknown type %d", int(h))
	}
}

func (c *checker) kindOf(h wasm.HeapType) byte { return c.m.Types[h].Comp.Kind }

// isSubIdx reports whether defined type a is a subtype of defined type b.
func (c *checker) isSubIdx(a, b int) bool {
	cb := c.canon[b]
	for {
		if c.canon[a] == cb {
			return true
		}
		st := &c.m.Types[a]
		if len(st.Supers) == 0 {
			return false
		}
		s := int(st.Supers[0])
		if s >= a { // invalid forward supertype; reported elsewhere
			return false
		}
		a = s
	}
}

func (c *checker) matchHeap(a, b wasm.HeapType) bool {
	if a == heapBot {
		return true
	}
	if b == heapBot {
		return false
	}
	if a.IsAbstract() && b.IsAbstract() {
		if a == b {
			return true
		}
		switch a {
		case wasm.HeapNone:
			switch b {
			case wasm.HeapAny, wasm.HeapEq, wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray:
				return true
			}
		case wasm.HeapNoFunc:
			return b == wasm.HeapFunc
		case wasm.HeapNoExtern:
			return b == wasm.HeapExtern
		case wasm.HeapNoExn:
			return b == wasm.HeapExn
		case wasm.HeapEq:
			return b == wasm.HeapAny
		case wasm.HeapI31, wasm.HeapStruct, wasm.HeapArray:
			return b == wasm.HeapEq || b == wasm.HeapAny
		}
		return false
	}
	if !a.IsAbstract() && b.IsAbstract() {
		switch c.kindOf(a) {
		case wasm.KindFunc:
			return b == wasm.HeapFunc
		case wasm.KindStruct:
			return b == wasm.HeapStruct || b == wasm.HeapEq || b == wasm.HeapAny
		case wasm.KindArray:
			return b == wasm.HeapArray || b == wasm.HeapEq || b == wasm.HeapAny
		}
		return false
	}
	if a.IsAbstract() {
		if c.kindOf(b) == wasm.KindFunc {
			return a == wasm.HeapNoFunc
		}
		return a == wasm.HeapNone
	}
	return c.isSubIdx(int(a), int(b))
}

func (c *checker) matchVal(a, b wasm.ValType) bool {
	if isBot(a) {
		return true
	}
	if a.Kind != b.Kind {
		return false
	}
	if a.Kind != wasm.Ref {
		return true
	}
	if a.Nullable && !b.Nullable {
		return false
	}
	return c.matchHeap(a.Heap, b.Heap)
}

func (c *checker) matchVals(a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !c.matchVal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func (c *checker) matchStorage(a, b wasm.ValType) bool {
	if a.Kind == wasm.I8 || a.Kind == wasm.I16 || b.Kind == wasm.I8 || b.Kind == wasm.I16 {
		return a.Kind == b.Kind
	}
	return c.matchVal(a, b)
}

func (c *checker) matchField(a, b wasm.FieldType) bool {
	if a.Mutable != b.Mutable {
		return false
	}
	if !c.matchStorage(a.Type, b.Type) {
		return false
	}
	if a.Mutable {
		return c.matchStorage(b.Type, a.Type)
	}
	return true
}

func (c *checker) matchComp(a, b *wasm.CompType) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case wasm.KindFunc:
		return c.matchVals(b.Func.Params, a.Func.Params) && c.matchVals(a.Func.Results, b.Func.Results)
	case wasm.KindStruct:
		if len(a.Fields) < len(b.Fields) {
			return false
		}
		for i := range b.Fields {
			if !c.matchField(a.Fields[i], b.Fields[i]) {
				return false
			}
		}
		return true
	case wasm.KindArray:
		return c.matchField(a.Fields[0], b.Fields[0])
	}
	return false
}

// topHeap returns the top heap type of h's hierarchy.
func (c *checker) topHeap(h wasm.HeapType) wasm.HeapType {
	if h == heapBot {
		return heapBot
	}
	if !h.IsAbstract() {
		if c.kindOf(h) == wasm.KindFunc {
			return wasm.HeapFunc
		}
		return wasm.HeapAny
	}
	switch h {
	case wasm.HeapFunc, wasm.HeapNoFunc:
		return wasm.HeapFunc
	case wasm.HeapExtern, wasm.HeapNoExtern:
		return wasm.HeapExtern
	case wasm.HeapExn, wasm.HeapNoExn:
		return wasm.HeapExn
	}
	return wasm.HeapAny
}

func defaultable(t wasm.ValType) bool {
	return t.Kind != wasm.Ref || t.Nullable
}

func typesString(ts []wasm.ValType) string {
	s := make([]string, len(ts))
	for i, t := range ts {
		if isBot(t) {
			s[i] = "bot"
		} else if t.Kind == wasm.Ref && t.Heap == heapBot {
			s[i] = "(ref bot)"
		} else {
			s[i] = t.String()
		}
	}
	return "[" + strings.Join(s, " ") + "]"
}
