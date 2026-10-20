package rt

import (
	"fmt"
	"strings"
)

// LinkError reports an import that is missing or has an incompatible type.
type LinkError struct {
	Module, Name string
	Msg          string
}

func (e *LinkError) Error() string {
	return fmt.Sprintf("link error: import %q %q: %s", e.Module, e.Name, e.Msg)
}

func LinkErr(module, name, msg string) *LinkError {
	return &LinkError{Module: module, Name: name, Msg: msg}
}

func linkFail(msg string) { panic(&LinkError{Msg: msg}) }

func CheckMemoryImport(m *Memory, min, max uint64, hasMax, is64, shared bool) {
	switch {
	case m == nil:
		linkFail("missing memory")
	case m.Is64 != is64:
		linkFail("incompatible import type: memory index type")
	case m.Shared != shared:
		linkFail("incompatible import type: memory sharing")
	case m.Pages() < min:
		linkFail("incompatible import type: memory too small")
	case hasMax && (!m.HasMax || m.Max > max):
		linkFail("incompatible import type: memory maximum")
	}
}

func CheckTableImport[T comparable](t *Table[T], min, max uint64, hasMax, is64 bool, elem string) {
	switch {
	case t == nil:
		linkFail("missing table")
	case t.Is64 != is64:
		linkFail("incompatible import type: table index type")
	case t.Size() < min:
		linkFail("incompatible import type: table too small")
	case hasMax && (!t.HasMax || t.Max > max):
		linkFail("incompatible import type: table maximum")
	case t.ElemType != "" && t.ElemType != elem:
		linkFail("incompatible import type: table element type")
	}
}

func CheckGlobalImport[T any](g *Global[T], mutable bool, typ string) {
	switch {
	case g == nil:
		linkFail("missing global")
	case g.Mutable != mutable:
		linkFail("incompatible import type: global mutability")
	case mutable && g.Type != "" && g.Type != typ:
		linkFail("incompatible import type: global type")
	case !mutable && g.Type != "" && !ValSubtype(g.Type, typ):
		linkFail("incompatible import type: global type")
	}
}

func CheckTagImport(t *Tag, typ *RTT) {
	if t == nil {
		linkFail("missing tag")
	}
	if t.Type != typ {
		linkFail("incompatible import type: tag type")
	}
}

// ValSubtype reports whether value type a is a subtype of b, both given in
// the compiler's serialization ("i32", "ref null func", "ref <key>", ...).
func ValSubtype(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := strings.HasPrefix(a, "ref "), strings.HasPrefix(b, "ref ")
	if !ra || !rb {
		return false
	}
	a, b = a[4:], b[4:]
	na, nb := strings.HasPrefix(a, "null "), strings.HasPrefix(b, "null ")
	if na {
		a = a[5:]
	}
	if nb {
		b = b[5:]
	}
	if na && !nb {
		return false
	}
	return heapSub(a, b)
}

func heapTop(h string) string {
	switch h {
	case "func", "nofunc":
		return "func"
	case "extern", "noextern":
		return "extern"
	case "exn", "noexn":
		return "exn"
	case "any", "eq", "i31", "struct", "array", "none":
		return "any"
	}
	if t := rttByKey(h); t != nil && t.Kind == KFunc {
		return "func"
	}
	return "any"
}

func rttByKey(h string) *RTT {
	if strings.HasPrefix(h, "<") && strings.HasSuffix(h, ">") {
		if v, ok := byKey.Load(h[1 : len(h)-1]); ok {
			return v.(*RTT)
		}
	}
	return nil
}

func heapSub(a, b string) bool {
	if a == b {
		return true
	}
	if heapTop(a) != heapTop(b) {
		return false
	}
	switch a {
	case "none", "nofunc", "noextern", "noexn":
		return true
	}
	ta, tb := rttByKey(a), rttByKey(b)
	switch b {
	case "any", "func", "extern", "exn":
		return true
	case "eq":
		return a == "i31" || a == "struct" || a == "array" || (ta != nil && ta.Kind != KFunc)
	case "struct":
		return ta != nil && ta.Kind == KStruct
	case "array":
		return ta != nil && ta.Kind == KArray
	}
	if ta != nil && tb != nil {
		return ta.SubOf(tb) || ta.Key == tb.Key
	}
	return false
}
