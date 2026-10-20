package compiler

import (
	"encoding/binary"
	"fmt"
	"strings"

	"gosm/internal/wasm"
)

var tv128 = wasm.TV128

func simdFuncName(name string) string {
	var b strings.Builder
	for _, p := range strings.FieldsFunc(name, func(r rune) bool { return r == '.' || r == '_' }) {
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return "rt." + b.String()
}

func sigType(c byte) wasm.ValType {
	switch c {
	case 'v':
		return tv128
	case 'i':
		return ti32
	case 'l':
		return ti64
	case 'f':
		return tf32
	case 'd':
		return tf64
	}
	panic("bad sig")
}

func (f *fn) simdOp(in *wasm.Instr) {
	c := f.c
	sub := in.Op.Sub()
	info := wasm.SIMDOps[sub]
	name := info.Name
	if strings.Contains(info.Sig, "m") {
		f.simdMem(in, name)
		return
	}
	switch {
	case name == "v128.const":
		lo := binary.LittleEndian.Uint64(in.V128[:8])
		hi := binary.LittleEndian.Uint64(in.V128[8:])
		f.push(entry{t: tv128, expr: fmt.Sprintf("rt.V128{Lo: 0x%x, Hi: 0x%x}", lo, hi), kind: kConst, maxH: -1})
		return
	case name == "i8x16.shuffle":
		ops := f.popN(2)
		var ls []string
		for _, b := range in.V128 {
			ls = append(ls, fmt.Sprint(b))
		}
		f.pushPure(tv128, fmt.Sprintf("rt.I8x16Shuffle(%s, %s, [16]byte{%s})", f.ex(ops[0]), f.ex(ops[1]), strings.Join(ls, ", ")), ops...)
		return
	}
	parts := strings.SplitN(info.Sig, ":", 2)
	ops := f.popN(len(parts[0]))
	var args []string
	for _, o := range ops {
		args = append(args, f.ex(o))
	}
	if strings.Contains(name, "extract_lane") {
		args = append(args, fmt.Sprint(in.Lane))
	} else if strings.Contains(name, "replace_lane") {
		args = []string{args[0], fmt.Sprint(in.Lane), args[1]}
	}
	_ = c
	f.pushPure(sigType(parts[1][0]), fmt.Sprintf("%s(%s)", simdFuncName(name), strings.Join(args, ", ")), ops...)
}

func (f *fn) simdMem(in *wasm.Instr, name string) {
	c := f.c
	m := c.memRef(in.B)
	op := strings.TrimPrefix(name, "v128.")
	if strings.HasPrefix(op, "store") {
		v := f.pop()
		addr := f.pop()
		ea := f.ea(in.B, addr, in.Offset)
		vs := f.ex(v)
		switch op {
		case "store":
			f.emit("%s.SV128(%s, %s)", m, ea, vs)
		case "store8_lane":
			f.emit("%s.S8(%s, uint8(rt.I8x16ExtractLaneU(%s, %d)))", m, ea, vs, in.Lane)
		case "store16_lane":
			f.emit("%s.S16(%s, uint16(rt.I16x8ExtractLaneU(%s, %d)))", m, ea, vs, in.Lane)
		case "store32_lane":
			f.emit("%s.S32(%s, uint32(rt.I32x4ExtractLane(%s, %d)))", m, ea, vs, in.Lane)
		case "store64_lane":
			f.emit("%s.S64(%s, uint64(rt.I64x2ExtractLane(%s, %d)))", m, ea, vs, in.Lane)
		}
		return
	}
	var vec entry
	if strings.HasSuffix(op, "_lane") {
		vec = f.pop()
	}
	addr := f.pop()
	ea := f.ea(in.B, addr, in.Offset)
	var r string
	switch op {
	case "load":
		r = fmt.Sprintf("%s.LV128(%s)", m, ea)
	case "load8x8_s", "load8x8_u", "load16x4_s", "load16x4_u", "load32x2_s", "load32x2_u":
		r = fmt.Sprintf("%s(%s.L64(%s))", simdFuncName("v128."+op), m, ea)
	case "load8_splat":
		r = fmt.Sprintf("rt.I8x16Splat(int32(%s.L8(%s)))", m, ea)
	case "load16_splat":
		r = fmt.Sprintf("rt.I16x8Splat(int32(%s.L16(%s)))", m, ea)
	case "load32_splat":
		r = fmt.Sprintf("rt.I32x4Splat(int32(%s.L32(%s)))", m, ea)
	case "load64_splat":
		r = fmt.Sprintf("rt.I64x2Splat(int64(%s.L64(%s)))", m, ea)
	case "load32_zero":
		r = fmt.Sprintf("rt.V128{Lo: uint64(%s.L32(%s))}", m, ea)
	case "load64_zero":
		r = fmt.Sprintf("rt.V128{Lo: %s.L64(%s)}", m, ea)
	case "load8_lane":
		r = fmt.Sprintf("rt.I8x16ReplaceLane(%s, %d, int32(%s.L8(%s)))", f.ex(vec), in.Lane, m, ea)
	case "load16_lane":
		r = fmt.Sprintf("rt.I16x8ReplaceLane(%s, %d, int32(%s.L16(%s)))", f.ex(vec), in.Lane, m, ea)
	case "load32_lane":
		r = fmt.Sprintf("rt.I32x4ReplaceLane(%s, %d, int32(%s.L32(%s)))", f.ex(vec), in.Lane, m, ea)
	case "load64_lane":
		r = fmt.Sprintf("rt.I64x2ReplaceLane(%s, %d, int64(%s.L64(%s)))", f.ex(vec), in.Lane, m, ea)
	default:
		panic("unknown simd memory op " + name)
	}
	f.pushEager(tv128, r)
}
