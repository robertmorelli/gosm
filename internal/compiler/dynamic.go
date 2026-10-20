package compiler

import (
	"fmt"
	"strings"

	"gosm/internal/wasm"
)

var jsStringBuiltins = map[string]bool{
	"cast": true, "test": true, "fromCharCodeArray": true, "intoCharCodeArray": true,
	"fromCharCode": true, "fromCodePoint": true, "charCodeAt": true, "codePointAt": true,
	"length": true, "concat": true, "substring": true, "equals": true, "compare": true,
}

func (c *compiler) builtinBody(name string, ft *wasm.FuncType) string {
	nullArr := func(p string) string {
		return fmt.Sprintf("if %s == nil {\npanic(rt.ErrNullArray)\n}\n", p)
	}
	switch name {
	case "cast":
		return "return rt.JSCast(p0)"
	case "test":
		return "return rt.JSTest(p0)"
	case "fromCharCodeArray":
		return nullArr("p0") + "return rt.JSFromCharCodeArray(p0.Elems, p1, p2)"
	case "intoCharCodeArray":
		return nullArr("p1") + "return rt.JSIntoCharCodeArray(p0, p1.Elems, p2)"
	case "fromCharCode":
		return "return rt.JSFromCharCode(p0)"
	case "fromCodePoint":
		return "return rt.JSFromCodePoint(p0)"
	case "charCodeAt":
		return "return m.jsCache.CharCodeAt(p0, p1)"
	case "codePointAt":
		return "return m.jsCache.CodePointAt(p0, p1)"
	case "length":
		return "return m.jsCache.Length(p0)"
	case "concat":
		return "return rt.JSConcat(p0, p1)"
	case "substring":
		return "return rt.JSSubstring(p0, p1, p2)"
	case "equals":
		return "return rt.JSEquals(p0, p1)"
	case "compare":
		return "return rt.JSCompare(p0, p1)"
	case "decodeStringFromUTF8Array":
		return nullArr("p0") + "return rt.JSDecodeUTF8(p0.Elems, p1, p2)"
	case "measureStringAsUTF8":
		return "return rt.JSMeasureUTF8(p0)"
	case "encodeStringIntoUTF8Array":
		return nullArr("p1") + "return rt.JSEncodeUTF8Into(p0, p1.Elems, p2)"
	case "encodeStringToUTF8Array":
		ti := c.info(uint32(ft.Results[0].Heap))
		return fmt.Sprintf("return %s(rt.JSEncodeUTF8(p0))", c.ctorName(ti))
	}
	panic("unknown builtin " + name)
}

// writeDynamic emits NewDynamic, which links imports by (module, name)
// lookups. It is used to link generated modules with each other.
func (c *compiler) writeDynamic(w *strings.Builder) {
	m := c.m
	var fields, link, methods strings.Builder
	fi := 0
	for i, im := range m.Imports {
		hn, ok := c.hostNames[i]
		if !ok {
			if im.Kind == wasm.ExtFunc {
				fi++
			}
			continue
		}
		fmt.Fprintf(&link, "v = resolve(%q, %q)\n", im.Module, im.Name)
		switch im.Kind {
		case wasm.ExtFunc:
			ft := &m.Types[im.TypeIdx].Comp.Func
			sig := c.goFuncSig(ft)
			fmt.Fprintf(&fields, "fn%d %s\n", i, sig)
			fmt.Fprintf(&link, "if f, ok := v.(*rt.Func); !ok || f == nil || !f.Type.SubOf(%s) {\npanic(rt.LinkErr(%q, %q, \"incompatible import type\"))\n} else if fn, ok := f.Fn.(%s); !ok {\npanic(rt.LinkErr(%q, %q, \"incompatible import type\"))\n} else {\nh.fn%d = fn\nh.refs[%d] = f\n}\n",
				c.info(im.TypeIdx).rtt, im.Module, im.Name, sig, im.Module, im.Name, i, fi)
			var args []string
			args = append(args, "0")
			for k := range ft.Params {
				args = append(args, fmt.Sprintf("p%d", k))
			}
			call := fmt.Sprintf("h.fn%d(%s)", i, strings.Join(args, ", "))
			if len(ft.Results) > 0 {
				call = "return " + call
			}
			fmt.Fprintf(&methods, "func (h *dynHost) %s%s {\n\t%s\n}\n\n", hn, c.sigGo(ft, nil, false), call)
			fi++
		case wasm.ExtMem:
			fmt.Fprintf(&fields, "v%d *rt.Memory\n", i)
			fmt.Fprintf(&link, "h.v%d, _ = v.(*rt.Memory)\nif h.v%d == nil {\npanic(rt.LinkErr(%q, %q, \"incompatible import type\"))\n}\n", i, i, im.Module, im.Name)
			fmt.Fprintf(&methods, "func (h *dynHost) %s() *rt.Memory { return h.v%d }\n\n", hn, i)
		case wasm.ExtTable:
			tg := c.tableGo(im.Table)
			fmt.Fprintf(&fields, "v%d %s\n", i, tg)
			fmt.Fprintf(&link, "h.v%d, _ = v.(%s)\nif h.v%d == nil {\npanic(rt.LinkErr(%q, %q, \"incompatible import type\"))\n}\n", i, tg, i, im.Module, im.Name)
			fmt.Fprintf(&methods, "func (h *dynHost) %s() %s { return h.v%d }\n\n", hn, tg, i)
		case wasm.ExtGlobal:
			gg := fmt.Sprintf("*rt.Global[%s]", c.goType(im.Global.Type))
			fmt.Fprintf(&fields, "v%d %s\n", i, gg)
			fmt.Fprintf(&link, "h.v%d, _ = v.(%s)\nif h.v%d == nil {\npanic(rt.LinkErr(%q, %q, \"incompatible import type\"))\n}\n", i, gg, i, im.Module, im.Name)
			fmt.Fprintf(&methods, "func (h *dynHost) %s() %s { return h.v%d }\n\n", hn, gg, i)
		case wasm.ExtTag:
			fmt.Fprintf(&fields, "v%d *rt.Tag\n", i)
			fmt.Fprintf(&link, "h.v%d, _ = v.(*rt.Tag)\nif h.v%d == nil {\npanic(rt.LinkErr(%q, %q, \"incompatible import type\"))\n}\n", i, i, im.Module, im.Name)
			fmt.Fprintf(&methods, "func (h *dynHost) %s() *rt.Tag { return h.v%d }\n\n", hn, i)
		}
	}
	fmt.Fprintf(w, "// NewDynamic instantiates the module, resolving each import by calling\n// resolve(module, name). Function imports must be *rt.Func values (e.g.\n// another instance's Exports), memories *rt.Memory, and so on.\n")
	w.WriteString("func NewDynamic(resolve func(module, name string) any) (*Instance, error) {\n")
	fmt.Fprintf(w, "h := &dynHost{refs: make([]*rt.Func, %d)}\n", m.NumImportedFuncs)
	w.WriteString("if err := rt.Catch(func() {\nvar v any\n_ = v\n")
	w.WriteString(link.String())
	w.WriteString("}); err != nil {\nreturn nil, err\n}\nreturn New(h)\n}\n\n")
	w.WriteString("type dynHost struct {\nrefs []*rt.Func\n")
	w.WriteString(fields.String())
	w.WriteString("}\n\n")
	w.WriteString("func (h *dynHost) WasmFuncRef(i int) *rt.Func { return h.refs[i] }\n\n")
	w.WriteString(methods.String())
}

func (c *compiler) goFuncSig(ft *wasm.FuncType) string {
	var ps, rs []string
	ps = append(ps, "int")
	for _, p := range ft.Params {
		ps = append(ps, c.goType(p))
	}
	for _, r := range ft.Results {
		rs = append(rs, c.goType(r))
	}
	s := "func(" + strings.Join(ps, ", ") + ")"
	switch len(rs) {
	case 0:
	case 1:
		s += " " + rs[0]
	default:
		s += " (" + strings.Join(rs, ", ") + ")"
	}
	return s
}
