package rt

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Test helpers

func v8(x ...uint8) V128 {
	var a [16]uint8
	copy(a[:], x)
	var r V128
	*(*[16]uint8)(unsafe.Pointer(&r)) = a
	return r
}
func vs8(x ...int8) V128 {
	var r V128
	copy((*[16]int8)(unsafe.Pointer(&r))[:], x)
	return r
}
func v16(x ...uint16) V128 {
	var r V128
	copy((*[8]uint16)(unsafe.Pointer(&r))[:], x)
	return r
}
func vs16(x ...int16) V128 {
	var r V128
	copy((*[8]int16)(unsafe.Pointer(&r))[:], x)
	return r
}
func v32(x ...uint32) V128 {
	var r V128
	copy((*[4]uint32)(unsafe.Pointer(&r))[:], x)
	return r
}
func vs32(x ...int32) V128 {
	var r V128
	copy((*[4]int32)(unsafe.Pointer(&r))[:], x)
	return r
}
func vf32(x ...float32) V128 {
	var r V128
	for i, f := range x {
		(*[4]uint32)(unsafe.Pointer(&r))[i] = math.Float32bits(f)
	}
	return r
}
func vf64(x ...float64) V128 {
	return V128{math.Float64bits(x[0]), math.Float64bits(x[1])}
}

func g8(v V128) [16]uint8  { return *(*[16]uint8)(unsafe.Pointer(&v)) }
func gs8(v V128) [16]int8  { return *(*[16]int8)(unsafe.Pointer(&v)) }
func g16(v V128) [8]uint16 { return *(*[8]uint16)(unsafe.Pointer(&v)) }
func gs16(v V128) [8]int16 { return *(*[8]int16)(unsafe.Pointer(&v)) }
func g32(v V128) [4]uint32 { return *(*[4]uint32)(unsafe.Pointer(&v)) }
func gs32(v V128) [4]int32 { return *(*[4]int32)(unsafe.Pointer(&v)) }
func gs64(v V128) [2]int64 { return [2]int64{int64(v.Lo), int64(v.Hi)} }
func gf32(v V128) [4]float32 {
	u := g32(v)
	return [4]float32{math.Float32frombits(u[0]), math.Float32frombits(u[1]), math.Float32frombits(u[2]), math.Float32frombits(u[3])}
}
func gf64(v V128) [2]float64 {
	return [2]float64{math.Float64frombits(v.Lo), math.Float64frombits(v.Hi)}
}

func eqV(t *testing.T, name string, got, want V128) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got {%#016x %#016x}, want {%#016x %#016x}", name, got.Lo, got.Hi, want.Lo, want.Hi)
	}
}

// sameF32 compares by bits, treating any two NaNs as equal.
func sameF32(a, b float32) bool {
	if a != a && b != b {
		return true
	}
	return math.Float32bits(a) == math.Float32bits(b)
}
func sameF64(a, b float64) bool {
	if a != a && b != b {
		return true
	}
	return math.Float64bits(a) == math.Float64bits(b)
}

func eqF32(t *testing.T, name string, got V128, want ...float32) {
	t.Helper()
	g := gf32(got)
	for i := range want {
		if !sameF32(g[i], want[i]) {
			t.Errorf("%s lane %d: got %v (%#x), want %v (%#x)", name, i, g[i], math.Float32bits(g[i]), want[i], math.Float32bits(want[i]))
		}
	}
}
func eqF64(t *testing.T, name string, got V128, want ...float64) {
	t.Helper()
	g := gf64(got)
	for i := range want {
		if !sameF64(g[i], want[i]) {
			t.Errorf("%s lane %d: got %v (%#x), want %v (%#x)", name, i, g[i], math.Float64bits(g[i]), want[i], math.Float64bits(want[i]))
		}
	}
}

var (
	tnan32 = float32(math.NaN())
	nan64v = math.NaN()
	negz32 = float32(math.Copysign(0, -1))
	negz64 = math.Copysign(0, -1)
	inf32  = float32(math.Inf(1))
	inf64  = math.Inf(1)
	ones   = V128{^uint64(0), ^uint64(0)}
)

// ---------------------------------------------------------------------------
// Completeness: every non-memory SIMD op (except v128.const) has a function.

func simdGoName(wasm string) string {
	var b strings.Builder
	for _, p := range strings.FieldsFunc(wasm, func(r rune) bool { return r == '.' || r == '_' }) {
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

func TestSIMDCompleteness(t *testing.T) {
	src, err := os.ReadFile("../internal/wasm/simd_ops.go")
	if err != nil {
		t.Skip("simd_ops.go not found:", err)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "simd.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]*ast.FuncDecl{}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
			have[fd.Name.Name] = fd
		}
	}
	re := regexp.MustCompile(`\{"([a-z0-9_.]+)", "([a-z]*):([a-z]*)"\}`)
	n := 0
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		name, ops := m[1], m[2]
		if name == "v128.const" || strings.Contains(ops, "m") {
			continue
		}
		n++
		if _, ok := have[simdGoName(name)]; !ok {
			t.Errorf("missing %s for %s", simdGoName(name), name)
		}
	}
	if n < 200 {
		t.Errorf("only matched %d ops; regexp broken?", n)
	}
	for _, l := range []string{"V128Load8x8S", "V128Load8x8U", "V128Load16x4S", "V128Load16x4U", "V128Load32x2S", "V128Load32x2U"} {
		if _, ok := have[l]; !ok {
			t.Errorf("missing %s", l)
		}
	}
	if simdGoName("i32x4.trunc_sat_f64x2_s_zero") != "I32x4TruncSatF64x2SZero" {
		t.Error("naming")
	}
}

// ---------------------------------------------------------------------------
// Splat / extract / replace / loads

func TestSIMDSplatLanes(t *testing.T) {
	eqV(t, "i8 splat", I8x16Splat(0x1ff), V128{0xffffffffffffffff, 0xffffffffffffffff})
	eqV(t, "i8 splat", I8x16Splat(0x12), V128{0x1212121212121212, 0x1212121212121212})
	eqV(t, "i16 splat", I16x8Splat(0x12345), V128{0x2345234523452345, 0x2345234523452345})
	eqV(t, "i32 splat", I32x4Splat(-2), V128{0xfffffffefffffffe, 0xfffffffefffffffe})
	eqV(t, "i64 splat", I64x2Splat(7), V128{7, 7})
	eqF32(t, "f32 splat", F32x4Splat(1.5), 1.5, 1.5, 1.5, 1.5)
	eqF64(t, "f64 splat", F64x2Splat(-2.5), -2.5, -2.5)

	v := v8(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 0x80)
	if got := I8x16ExtractLaneS(v, 15); got != -128 {
		t.Errorf("extract_lane_s %d", got)
	}
	if got := I8x16ExtractLaneU(v, 15); got != 128 {
		t.Errorf("extract_lane_u %d", got)
	}
	if got := I8x16ExtractLaneU(v, 9); got != 9 {
		t.Errorf("extract_lane_u 9 %d", got)
	}
	w := v16(0, 0, 0, 0, 0, 0, 0, 0xfffe)
	if I16x8ExtractLaneS(w, 7) != -2 || I16x8ExtractLaneU(w, 7) != 0xfffe {
		t.Error("i16 extract")
	}
	if I32x4ExtractLane(vs32(1, 2, -3, 4), 2) != -3 {
		t.Error("i32 extract")
	}
	if I64x2ExtractLane(V128{1, 0xffffffffffffffff}, 1) != -1 {
		t.Error("i64 extract")
	}
	if F32x4ExtractLane(vf32(1, 2, 3, 4), 3) != 4 || F64x2ExtractLane(vf64(1, 2), 1) != 2 {
		t.Error("float extract")
	}
	// NaN payload preserved by extract/replace.
	pn := math.Float32frombits(0x7fa00001)
	if math.Float32bits(F32x4ExtractLane(F32x4ReplaceLane(V128{}, 1, pn), 1)) != 0x7fa00001 {
		t.Error("f32 replace/extract NaN payload")
	}

	eqV(t, "i8 replace", I8x16ReplaceLane(V128{}, 8, 0x1ab), V128{0, 0xab})
	eqV(t, "i16 replace", I16x8ReplaceLane(V128{}, 1, -1), V128{0xffff0000, 0})
	eqV(t, "i32 replace", I32x4ReplaceLane(ones, 3, 0), V128{^uint64(0), 0xffffffff})
	eqV(t, "i64 replace", I64x2ReplaceLane(V128{}, 0, -1), V128{^uint64(0), 0})
	eqF64(t, "f64 replace", F64x2ReplaceLane(vf64(1, 2), 0, 3), 3, 2)
	eqF32(t, "f32 replace", F32x4ReplaceLane(vf32(1, 2, 3, 4), 2, 9), 1, 2, 9, 4)
}

func TestSIMDLoadExtend(t *testing.T) {
	x := uint64(0x80ff7f0102030405)
	if got := gs16(V128Load8x8S(x)); got != [8]int16{5, 4, 3, 2, 1, 127, -1, -128} {
		t.Errorf("load8x8_s %v", got)
	}
	if got := g16(V128Load8x8U(x)); got != [8]uint16{5, 4, 3, 2, 1, 127, 255, 128} {
		t.Errorf("load8x8_u %v", got)
	}
	if got := gs32(V128Load16x4S(x)); got != [4]int32{0x0405, 0x0203, 0x7f01, -32513} {
		t.Errorf("load16x4_s %v", got)
	}
	if got := g32(V128Load16x4U(x)); got != [4]uint32{0x0405, 0x0203, 0x7f01, 0x80ff} {
		t.Errorf("load16x4_u %v", got)
	}
	if got := gs64(V128Load32x2S(x)); got != [2]int64{0x02030405, int64(int32(-0x7f0080ff))} {
		t.Errorf("load32x2_s %v", got)
	}
	eqV(t, "load32x2_u", V128Load32x2U(x), V128{0x02030405, 0x80ff7f01})

	v := vs8(-1, 2, -3, 4, -5, 6, -7, 8, -128, 127, 0, 1, 2, 3, 4, 5)
	if got := gs16(I16x8ExtendLowI8x16S(v)); got != [8]int16{-1, 2, -3, 4, -5, 6, -7, 8} {
		t.Errorf("extend_low_s %v", got)
	}
	if got := gs16(I16x8ExtendHighI8x16S(v)); got != [8]int16{-128, 127, 0, 1, 2, 3, 4, 5} {
		t.Errorf("extend_high_s %v", got)
	}
	if got := g16(I16x8ExtendHighI8x16U(v)); got != [8]uint16{128, 127, 0, 1, 2, 3, 4, 5} {
		t.Errorf("extend_high_u %v", got)
	}
	if got := g16(I16x8ExtendLowI8x16U(v)); got[0] != 255 || got[1] != 2 {
		t.Errorf("extend_low_u %v", got)
	}
	w := vs16(-1, 2, -3, 4, -32768, 32767, 0, 1)
	if got := gs32(I32x4ExtendLowI16x8S(w)); got != [4]int32{-1, 2, -3, 4} {
		t.Errorf("i32 extend_low_s %v", got)
	}
	if got := gs32(I32x4ExtendHighI16x8S(w)); got != [4]int32{-32768, 32767, 0, 1} {
		t.Errorf("i32 extend_high_s %v", got)
	}
	if got := g32(I32x4ExtendLowI16x8U(w)); got != [4]uint32{65535, 2, 65533, 4} {
		t.Errorf("i32 extend_low_u %v", got)
	}
	if got := g32(I32x4ExtendHighI16x8U(w)); got != [4]uint32{32768, 32767, 0, 1} {
		t.Errorf("i32 extend_high_u %v", got)
	}
	d := vs32(-1, 2, math.MinInt32, 3)
	if got := gs64(I64x2ExtendLowI32x4S(d)); got != [2]int64{-1, 2} {
		t.Errorf("i64 extend_low_s %v", got)
	}
	if got := gs64(I64x2ExtendHighI32x4S(d)); got != [2]int64{math.MinInt32, 3} {
		t.Errorf("i64 extend_high_s %v", got)
	}
	eqV(t, "i64 extend_low_u", I64x2ExtendLowI32x4U(d), V128{0xffffffff, 2})
	eqV(t, "i64 extend_high_u", I64x2ExtendHighI32x4U(d), V128{0x80000000, 3})
}

// ---------------------------------------------------------------------------
// Shuffle / swizzle

func TestSIMDShuffleSwizzle(t *testing.T) {
	a := v8(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15)
	b := v8(16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31)
	l := [16]byte{31, 0, 16, 15, 1, 17, 2, 18, 30, 29, 3, 3, 3, 20, 0, 31}
	if got := g8(I8x16Shuffle(a, b, l)); got != [16]uint8(l) {
		t.Errorf("shuffle %v", got)
	}
	s := v8(15, 0, 16, 255, 1, 0x80, 2, 17, 3, 3, 14, 100, 4, 5, 6, 7)
	want := [16]uint8{15, 0, 0, 0, 1, 0, 2, 0, 3, 3, 14, 0, 4, 5, 6, 7}
	if got := g8(I8x16Swizzle(a, s)); got != want {
		t.Errorf("swizzle %v", got)
	}
	if got := g8(I8x16RelaxedSwizzle(a, s)); got != want {
		t.Errorf("relaxed_swizzle %v", got)
	}
}

// ---------------------------------------------------------------------------
// Comparisons

func TestSIMDCompare(t *testing.T) {
	a := vs8(-1, 0, 1, -128, 127, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	b := vs8(1, 0, -1, 127, -128, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	m := func(x ...uint8) [16]uint8 {
		var r [16]uint8
		copy(r[:], x)
		for i := len(x); i < 16; i++ {
			r[i] = r[15]
		}
		return r
	}
	_ = m
	chk := func(name string, got V128, first6 [6]uint8, rest uint8) {
		t.Helper()
		g := g8(got)
		for i := 0; i < 16; i++ {
			w := rest
			if i < 6 {
				w = first6[i]
			}
			if g[i] != w {
				t.Errorf("%s: lane %d got %#x want %#x", name, i, g[i], w)
			}
		}
	}
	F, T := uint8(0), uint8(0xff)
	chk("eq", I8x16Eq(a, b), [6]uint8{F, T, F, F, F, T}, T)
	chk("ne", I8x16Ne(a, b), [6]uint8{T, F, T, T, T, F}, F)
	chk("lt_s", I8x16LtS(a, b), [6]uint8{T, F, F, T, F, F}, F)
	chk("lt_u", I8x16LtU(a, b), [6]uint8{F, F, T, F, T, F}, F)
	chk("gt_s", I8x16GtS(a, b), [6]uint8{F, F, T, F, T, F}, F)
	chk("gt_u", I8x16GtU(a, b), [6]uint8{T, F, F, T, F, F}, F)
	chk("le_s", I8x16LeS(a, b), [6]uint8{T, T, F, T, F, T}, T)
	chk("le_u", I8x16LeU(a, b), [6]uint8{F, T, T, F, T, T}, T)
	chk("ge_s", I8x16GeS(a, b), [6]uint8{F, T, T, F, T, T}, T)
	chk("ge_u", I8x16GeU(a, b), [6]uint8{T, T, F, T, F, T}, T)

	x := vs16(-1, 0, 1, -32768, 0, 0, 0, 0)
	y := vs16(1, 0, -1, 32767, 0, 0, 0, 0)
	eqV(t, "i16 lt_s", I16x8LtS(x, y), v16(0xffff, 0, 0, 0xffff))
	eqV(t, "i16 lt_u", I16x8LtU(x, y), v16(0, 0, 0xffff, 0))
	eqV(t, "i16 eq", I16x8Eq(x, y), v16(0, 0xffff, 0, 0, 0xffff, 0xffff, 0xffff, 0xffff))
	eqV(t, "i16 ne", I16x8Ne(x, y), v16(0xffff, 0, 0xffff, 0xffff))
	eqV(t, "i16 gt_s", I16x8GtS(x, y), v16(0, 0, 0xffff, 0))
	eqV(t, "i16 gt_u", I16x8GtU(x, y), v16(0xffff, 0, 0, 0xffff))
	eqV(t, "i16 le_s", I16x8LeS(x, y), v16(0xffff, 0xffff, 0, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff))
	eqV(t, "i16 le_u", I16x8LeU(x, y), v16(0, 0xffff, 0xffff, 0, 0xffff, 0xffff, 0xffff, 0xffff))
	eqV(t, "i16 ge_s", I16x8GeS(x, y), v16(0, 0xffff, 0xffff, 0, 0xffff, 0xffff, 0xffff, 0xffff))
	eqV(t, "i16 ge_u", I16x8GeU(x, y), v16(0xffff, 0xffff, 0, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff))

	p := vs32(-1, 0, 1, math.MinInt32)
	q := vs32(1, 0, -1, math.MaxInt32)
	eqV(t, "i32 eq", I32x4Eq(p, q), v32(0, ^uint32(0), 0, 0))
	eqV(t, "i32 ne", I32x4Ne(p, q), v32(^uint32(0), 0, ^uint32(0), ^uint32(0)))
	eqV(t, "i32 lt_s", I32x4LtS(p, q), v32(^uint32(0), 0, 0, ^uint32(0)))
	eqV(t, "i32 lt_u", I32x4LtU(p, q), v32(0, 0, ^uint32(0), 0))
	eqV(t, "i32 gt_s", I32x4GtS(p, q), v32(0, 0, ^uint32(0), 0))
	eqV(t, "i32 gt_u", I32x4GtU(p, q), v32(^uint32(0), 0, 0, ^uint32(0)))
	eqV(t, "i32 le_s", I32x4LeS(p, q), v32(^uint32(0), ^uint32(0), 0, ^uint32(0)))
	eqV(t, "i32 le_u", I32x4LeU(p, q), v32(0, ^uint32(0), ^uint32(0), 0))
	eqV(t, "i32 ge_s", I32x4GeS(p, q), v32(0, ^uint32(0), ^uint32(0), 0))
	eqV(t, "i32 ge_u", I32x4GeU(p, q), v32(^uint32(0), ^uint32(0), 0, ^uint32(0)))

	l1 := V128{uint64(1 << 63), 5}
	l2 := V128{1<<63 - 1, 5}
	eqV(t, "i64 eq", I64x2Eq(l1, l2), V128{0, ^uint64(0)})
	eqV(t, "i64 ne", I64x2Ne(l1, l2), V128{^uint64(0), 0})
	eqV(t, "i64 lt_s", I64x2LtS(l1, l2), V128{^uint64(0), 0})
	eqV(t, "i64 gt_s", I64x2GtS(l1, l2), V128{0, 0})
	eqV(t, "i64 le_s", I64x2LeS(l1, l2), V128{^uint64(0), ^uint64(0)})
	eqV(t, "i64 ge_s", I64x2GeS(l1, l2), V128{0, ^uint64(0)})

	fa := vf32(tnan32, 0, 1, -1)
	fb := vf32(tnan32, negz32, 2, -1)
	M := ^uint32(0)
	eqV(t, "f32 eq", F32x4Eq(fa, fb), v32(0, M, 0, M))
	eqV(t, "f32 ne", F32x4Ne(fa, fb), v32(M, 0, M, 0))
	eqV(t, "f32 lt", F32x4Lt(fa, fb), v32(0, 0, M, 0))
	eqV(t, "f32 gt", F32x4Gt(fa, fb), v32(0, 0, 0, 0))
	eqV(t, "f32 le", F32x4Le(fa, fb), v32(0, M, M, M))
	eqV(t, "f32 ge", F32x4Ge(fa, fb), v32(0, M, 0, M))

	da, db := vf64(nan64v, 1), vf64(1, 1)
	eqV(t, "f64 eq", F64x2Eq(da, db), V128{0, ^uint64(0)})
	eqV(t, "f64 ne", F64x2Ne(da, db), V128{^uint64(0), 0})
	eqV(t, "f64 lt", F64x2Lt(vf64(0, 2), vf64(negz64, 3)), V128{0, ^uint64(0)})
	eqV(t, "f64 gt", F64x2Gt(vf64(4, 2), vf64(3, 3)), V128{^uint64(0), 0})
	eqV(t, "f64 le", F64x2Le(da, db), V128{0, ^uint64(0)})
	eqV(t, "f64 ge", F64x2Ge(vf64(0, 2), vf64(negz64, 3)), V128{^uint64(0), 0})
}

// ---------------------------------------------------------------------------
// Bitwise, any/all/bitmask

func TestSIMDBitwise(t *testing.T) {
	a := V128{0xff00ff00ff00ff00, 0x0123456789abcdef}
	b := V128{0x0ff00ff00ff00ff0, 0xfedcba9876543210}
	c := V128{0xffffffff00000000, 0x00000000ffffffff}
	eqV(t, "not", V128Not(a), V128{^a.Lo, ^a.Hi})
	eqV(t, "and", V128And(a, b), V128{a.Lo & b.Lo, a.Hi & b.Hi})
	eqV(t, "andnot", V128Andnot(a, b), V128{a.Lo &^ b.Lo, a.Hi &^ b.Hi})
	eqV(t, "or", V128Or(a, b), V128{a.Lo | b.Lo, a.Hi | b.Hi})
	eqV(t, "xor", V128Xor(a, b), V128{a.Lo ^ b.Lo, a.Hi ^ b.Hi})
	want := V128{0xff00ff000ff00ff0, 0xfedcba9889abcdef}
	eqV(t, "bitselect", V128Bitselect(a, b, c), want)
	eqV(t, "i8 laneselect", I8x16RelaxedLaneselect(a, b, c), want)
	eqV(t, "i16 laneselect", I16x8RelaxedLaneselect(a, b, c), want)
	eqV(t, "i32 laneselect", I32x4RelaxedLaneselect(a, b, c), want)
	eqV(t, "i64 laneselect", I64x2RelaxedLaneselect(a, b, c), want)

	if V128AnyTrue(V128{}) != 0 || V128AnyTrue(V128{0, 1 << 63}) != 1 {
		t.Error("any_true")
	}
	all := v8(1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)
	if I8x16AllTrue(all) != 1 || I8x16AllTrue(I8x16ReplaceLane(all, 15, 0)) != 0 {
		t.Error("i8 all_true")
	}
	if I16x8AllTrue(all) != 1 || I16x8AllTrue(v16(1, 1, 1, 1, 1, 1, 1, 0)) != 0 {
		t.Error("i16 all_true")
	}
	if I32x4AllTrue(v32(1, 0x100, 0x10000, 0x1000000)) != 1 || I32x4AllTrue(v32(1, 1, 0, 1)) != 0 {
		t.Error("i32 all_true")
	}
	if I64x2AllTrue(V128{1 << 63, 1}) != 1 || I64x2AllTrue(V128{1, 0}) != 0 {
		t.Error("i64 all_true")
	}

	if got := I8x16Bitmask(v8(0x80, 0x7f, 0xff, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x80)); got != 0x8005 {
		t.Errorf("i8 bitmask %#x", got)
	}
	if got := I8x16Bitmask(ones); got != 0xffff {
		t.Errorf("i8 bitmask all %#x", got)
	}
	if got := I16x8Bitmask(v16(0x8000, 0x7fff, 0, 0, 0, 0, 0, 0xffff)); got != 0x81 {
		t.Errorf("i16 bitmask %#x", got)
	}
	if got := I32x4Bitmask(v32(0, 0x80000000, 0x7fffffff, 0xffffffff)); got != 0xa {
		t.Errorf("i32 bitmask %#x", got)
	}
	if got := I64x2Bitmask(V128{1 << 63, 1}); got != 1 {
		t.Errorf("i64 bitmask %#x", got)
	}
	if got := I64x2Bitmask(V128{1, 1 << 63}); got != 2 {
		t.Errorf("i64 bitmask %#x", got)
	}
}

// ---------------------------------------------------------------------------
// Integer arithmetic

func TestSIMDIntArith(t *testing.T) {
	// abs / neg wrap on the minimum value.
	if got := gs8(I8x16Abs(vs8(-128, -1, 1, 0, 127, -127))); got[0] != -128 || got[1] != 1 || got[2] != 1 || got[4] != 127 || got[5] != 127 {
		t.Errorf("i8 abs %v", got)
	}
	if got := gs8(I8x16Neg(vs8(-128, -1, 1, 0, 127))); got[0] != -128 || got[1] != 1 || got[2] != -1 || got[4] != -127 {
		t.Errorf("i8 neg %v", got)
	}
	if got := gs16(I16x8Abs(vs16(-32768, -5, 5))); got[0] != -32768 || got[1] != 5 || got[2] != 5 {
		t.Errorf("i16 abs %v", got)
	}
	if got := gs16(I16x8Neg(vs16(-32768, -5, 5))); got[0] != -32768 || got[1] != 5 || got[2] != -5 {
		t.Errorf("i16 neg %v", got)
	}
	if got := gs32(I32x4Abs(vs32(math.MinInt32, -7, 7, 0))); got != [4]int32{math.MinInt32, 7, 7, 0} {
		t.Errorf("i32 abs %v", got)
	}
	if got := gs32(I32x4Neg(vs32(math.MinInt32, -7, 7, 0))); got != [4]int32{math.MinInt32, 7, -7, 0} {
		t.Errorf("i32 neg %v", got)
	}
	eqV(t, "i64 abs", I64x2Abs(V128{1 << 63, uint64(0xfffffffffffffffd)}), V128{1 << 63, 3})
	eqV(t, "i64 neg", I64x2Neg(V128{1 << 63, 3}), V128{1 << 63, uint64(0xfffffffffffffffd)})

	if got := g8(I8x16Popcnt(v8(0, 1, 3, 0xff, 0x80, 0x55))); got[0] != 0 || got[1] != 1 || got[2] != 2 || got[3] != 8 || got[4] != 1 || got[5] != 4 {
		t.Errorf("popcnt %v", got)
	}

	// Saturating i8.
	a := vs8(127, -128, 100, -100, 1, -1, 0, 0)
	b := vs8(1, -1, 100, -100, -1, 1, 0, 0)
	if got := gs8(I8x16AddSatS(a, b)); got[0] != 127 || got[1] != -128 || got[2] != 127 || got[3] != -128 || got[4] != 0 {
		t.Errorf("add_sat_s %v", got)
	}
	if got := gs8(I8x16SubSatS(vs8(-128, 127, 0), vs8(1, -1, -128))); got[0] != -128 || got[1] != 127 || got[2] != 127 {
		t.Errorf("sub_sat_s %v", got)
	}
	if got := g8(I8x16AddSatU(v8(255, 200, 1), v8(1, 100, 1))); got[0] != 255 || got[1] != 255 || got[2] != 2 {
		t.Errorf("add_sat_u %v", got)
	}
	if got := g8(I8x16SubSatU(v8(0, 5, 200), v8(1, 10, 100))); got[0] != 0 || got[1] != 0 || got[2] != 100 {
		t.Errorf("sub_sat_u %v", got)
	}
	if got := g8(I8x16Add(v8(255, 1), v8(1, 2))); got[0] != 0 || got[1] != 3 {
		t.Errorf("add %v", got)
	}
	if got := g8(I8x16Sub(v8(0, 5), v8(1, 2))); got[0] != 255 || got[1] != 3 {
		t.Errorf("sub %v", got)
	}
	if got := g8(I8x16AvgrU(v8(255, 0, 1, 2), v8(255, 1, 2, 2))); got[0] != 255 || got[1] != 1 || got[2] != 2 || got[3] != 2 {
		t.Errorf("avgr_u %v", got)
	}
	mn := vs8(-1, 5)
	mx := vs8(1, 3)
	if got := gs8(I8x16MinS(mn, mx)); got[0] != -1 || got[1] != 3 {
		t.Errorf("min_s %v", got)
	}
	if got := gs8(I8x16MinU(mn, mx)); got[0] != 1 || got[1] != 3 {
		t.Errorf("min_u %v", got)
	}
	if got := gs8(I8x16MaxS(mn, mx)); got[0] != 1 || got[1] != 5 {
		t.Errorf("max_s %v", got)
	}
	if got := gs8(I8x16MaxU(mn, mx)); got[0] != -1 || got[1] != 5 {
		t.Errorf("max_u %v", got)
	}

	// Saturating i16.
	if got := gs16(I16x8AddSatS(vs16(32767, -32768, 1), vs16(1, -1, 1))); got[0] != 32767 || got[1] != -32768 || got[2] != 2 {
		t.Errorf("i16 add_sat_s %v", got)
	}
	if got := gs16(I16x8SubSatS(vs16(-32768, 32767), vs16(1, -1))); got[0] != -32768 || got[1] != 32767 {
		t.Errorf("i16 sub_sat_s %v", got)
	}
	if got := g16(I16x8AddSatU(v16(65535, 60000), v16(1, 10000))); got[0] != 65535 || got[1] != 65535 {
		t.Errorf("i16 add_sat_u %v", got)
	}
	if got := g16(I16x8SubSatU(v16(0, 10), v16(1, 3))); got[0] != 0 || got[1] != 7 {
		t.Errorf("i16 sub_sat_u %v", got)
	}
	if got := g16(I16x8Add(v16(65535), v16(2))); got[0] != 1 {
		t.Errorf("i16 add %v", got)
	}
	if got := g16(I16x8Sub(v16(0), v16(1))); got[0] != 65535 {
		t.Errorf("i16 sub %v", got)
	}
	if got := g16(I16x8Mul(v16(0x100, 3), v16(0x100, 0xffff))); got[0] != 0 || got[1] != 0xfffd {
		t.Errorf("i16 mul %v", got)
	}
	if got := g16(I16x8AvgrU(v16(65535, 0, 3), v16(65535, 1, 4))); got[0] != 65535 || got[1] != 1 || got[2] != 4 {
		t.Errorf("i16 avgr_u %v", got)
	}
	h1, h2 := vs16(-1, 5), vs16(1, 3)
	if got := gs16(I16x8MinS(h1, h2)); got[0] != -1 || got[1] != 3 {
		t.Errorf("i16 min_s %v", got)
	}
	if got := gs16(I16x8MinU(h1, h2)); got[0] != 1 {
		t.Errorf("i16 min_u %v", got)
	}
	if got := gs16(I16x8MaxS(h1, h2)); got[0] != 1 {
		t.Errorf("i16 max_s %v", got)
	}
	if got := gs16(I16x8MaxU(h1, h2)); got[0] != -1 {
		t.Errorf("i16 max_u %v", got)
	}

	// i32
	if got := g32(I32x4Add(v32(0xffffffff, 1), v32(1, 2))); got[0] != 0 || got[1] != 3 {
		t.Errorf("i32 add %v", got)
	}
	if got := g32(I32x4Sub(v32(0, 5), v32(1, 2))); got[0] != 0xffffffff || got[1] != 3 {
		t.Errorf("i32 sub %v", got)
	}
	if got := g32(I32x4Mul(v32(0x10000, 3), v32(0x10000, 0xffffffff))); got[0] != 0 || got[1] != 0xfffffffd {
		t.Errorf("i32 mul %v", got)
	}
	w1, w2 := vs32(-1, 5), vs32(1, 3)
	if got := gs32(I32x4MinS(w1, w2)); got[0] != -1 || got[1] != 3 {
		t.Errorf("i32 min_s %v", got)
	}
	if got := gs32(I32x4MinU(w1, w2)); got[0] != 1 {
		t.Errorf("i32 min_u %v", got)
	}
	if got := gs32(I32x4MaxS(w1, w2)); got[0] != 1 {
		t.Errorf("i32 max_s %v", got)
	}
	if got := gs32(I32x4MaxU(w1, w2)); got[0] != -1 {
		t.Errorf("i32 max_u %v", got)
	}

	// i64
	eqV(t, "i64 add", I64x2Add(V128{^uint64(0), 1}, V128{1, 2}), V128{0, 3})
	eqV(t, "i64 sub", I64x2Sub(V128{0, 5}, V128{1, 2}), V128{^uint64(0), 3})
	eqV(t, "i64 mul", I64x2Mul(V128{1 << 32, 3}, V128{1 << 32, ^uint64(0)}), V128{0, uint64(0xfffffffffffffffd)})
}

func TestSIMDShifts(t *testing.T) {
	if got := g8(I8x16Shl(v8(0x81, 1), 9)); got[0] != 0x02 || got[1] != 2 {
		t.Errorf("i8 shl mod %v", got)
	}
	if got := gs8(I8x16ShrS(vs8(-128, 64), 7)); got[0] != -1 || got[1] != 0 {
		t.Errorf("i8 shr_s %v", got)
	}
	if got := g8(I8x16ShrU(v8(0x80, 0xff), 15)); got[0] != 1 || got[1] != 1 {
		t.Errorf("i8 shr_u mod %v", got)
	}
	if got := g8(I8x16Shl(v8(3), -1)); got[0] != 0x80 { // -1 mod 8 = 7
		t.Errorf("i8 shl -1 %v", got)
	}
	if got := g16(I16x8Shl(v16(0x8001), 17)); got[0] != 2 {
		t.Errorf("i16 shl %v", got)
	}
	if got := gs16(I16x8ShrS(vs16(-32768), 15)); got[0] != -1 {
		t.Errorf("i16 shr_s %v", got)
	}
	if got := g16(I16x8ShrU(v16(0x8000), 31)); got[0] != 1 {
		t.Errorf("i16 shr_u %v", got)
	}
	if got := g32(I32x4Shl(v32(1), 33)); got[0] != 2 {
		t.Errorf("i32 shl %v", got)
	}
	if got := gs32(I32x4ShrS(vs32(math.MinInt32), 31)); got[0] != -1 {
		t.Errorf("i32 shr_s %v", got)
	}
	if got := g32(I32x4ShrU(v32(0x80000000), 63)); got[0] != 1 {
		t.Errorf("i32 shr_u %v", got)
	}
	eqV(t, "i64 shl", I64x2Shl(V128{1, 1 << 63}, 65), V128{2, 0})
	eqV(t, "i64 shr_s", I64x2ShrS(V128{1 << 63, 4}, 64+63), V128{^uint64(0), 0})
	eqV(t, "i64 shr_u", I64x2ShrU(V128{1 << 63, 4}, 63), V128{1, 0})
	eqV(t, "i64 shr_u 0", I64x2ShrU(V128{1 << 63, 4}, 64), V128{1 << 63, 4})
}

func TestSIMDNarrow(t *testing.T) {
	a := vs16(-129, -128, 127, 128, 0, -1, 32767, -32768)
	b := vs16(255, 256, 1, 2, 3, 4, 5, 6)
	if got := gs8(I8x16NarrowI16x8S(a, b)); got != [16]int8{-128, -128, 127, 127, 0, -1, 127, -128, 127, 127, 1, 2, 3, 4, 5, 6} {
		t.Errorf("narrow_s %v", got)
	}
	if got := g8(I8x16NarrowI16x8U(a, b)); got != [16]uint8{0, 0, 127, 128, 0, 0, 255, 0, 255, 255, 1, 2, 3, 4, 5, 6} {
		t.Errorf("narrow_u %v", got)
	}
	c := vs32(-32769, 32768, -1, 65536)
	d := vs32(65535, 32767, -32768, 7)
	if got := gs16(I16x8NarrowI32x4S(c, d)); got != [8]int16{-32768, 32767, -1, 32767, 32767, 32767, -32768, 7} {
		t.Errorf("i16 narrow_s %v", got)
	}
	if got := g16(I16x8NarrowI32x4U(c, d)); got != [8]uint16{0, 32768, 0, 65535, 65535, 32767, 0, 7} {
		t.Errorf("i16 narrow_u %v", got)
	}
}

func TestSIMDExtmulPairwiseDot(t *testing.T) {
	a := vs8(-128, 127, -1, 2, 3, 4, 5, 6, -128, -128, 127, 1, 0, 0, 0, 0)
	b := vs8(-128, 127, 1, 2, 3, 4, 5, 6, 127, -128, 127, -1, 0, 0, 0, 0)
	if got := gs16(I16x8ExtmulLowI8x16S(a, b)); got != [8]int16{16384, 16129, -1, 4, 9, 16, 25, 36} {
		t.Errorf("extmul_low_s %v", got)
	}
	if got := gs16(I16x8ExtmulHighI8x16S(a, b)); got != [8]int16{-16256, 16384, 16129, -1, 0, 0, 0, 0} {
		t.Errorf("extmul_high_s %v", got)
	}
	if got := g16(I16x8ExtmulLowI8x16U(a, b)); got[0] != 16384 || got[2] != 255 {
		t.Errorf("extmul_low_u %v", got)
	}
	if got := g16(I16x8ExtmulHighI8x16U(v8(0, 0, 0, 0, 0, 0, 0, 0, 255), v8(0, 0, 0, 0, 0, 0, 0, 0, 255))); got[0] != 65025 {
		t.Errorf("extmul_high_u %v", got)
	}
	x := vs16(-32768, 2, 3, 4, -32768, 1, 1, 1)
	y := vs16(-32768, 2, 3, 4, 32767, 1, 1, 1)
	if got := gs32(I32x4ExtmulLowI16x8S(x, y)); got != [4]int32{1 << 30, 4, 9, 16} {
		t.Errorf("i32 extmul_low_s %v", got)
	}
	if got := gs32(I32x4ExtmulHighI16x8S(x, y)); got != [4]int32{-32768 * 32767, 1, 1, 1} {
		t.Errorf("i32 extmul_high_s %v", got)
	}
	if got := g32(I32x4ExtmulLowI16x8U(v16(65535), v16(65535))); got[0] != 0xfffe0001 {
		t.Errorf("i32 extmul_low_u %v", got)
	}
	if got := g32(I32x4ExtmulHighI16x8U(v16(0, 0, 0, 0, 65535), v16(0, 0, 0, 0, 2))); got[0] != 131070 {
		t.Errorf("i32 extmul_high_u %v", got)
	}
	p := vs32(math.MinInt32, -1, 3, math.MaxInt32)
	if got := gs64(I64x2ExtmulLowI32x4S(p, p)); got != [2]int64{1 << 62, 1} {
		t.Errorf("i64 extmul_low_s %v", got)
	}
	if got := gs64(I64x2ExtmulHighI32x4S(p, vs32(0, 0, -3, math.MinInt32))); got != [2]int64{-9, math.MaxInt32 * math.MinInt32} {
		t.Errorf("i64 extmul_high_s %v", got)
	}
	eqV(t, "i64 extmul_low_u", I64x2ExtmulLowI32x4U(v32(0xffffffff, 2), v32(0xffffffff, 3)), V128{0xfffffffe00000001, 6})
	eqV(t, "i64 extmul_high_u", I64x2ExtmulHighI32x4U(v32(0, 0, 0xffffffff, 1), v32(0, 0, 2, 1)), V128{0x1fffffffe, 1})

	if got := gs16(I16x8ExtaddPairwiseI8x16S(vs8(-128, -128, 127, 127, -1, 1))); got[0] != -256 || got[1] != 254 || got[2] != 0 {
		t.Errorf("extadd_s %v", got)
	}
	if got := g16(I16x8ExtaddPairwiseI8x16U(v8(255, 255, 1, 2))); got[0] != 510 || got[1] != 3 {
		t.Errorf("extadd_u %v", got)
	}
	if got := gs32(I32x4ExtaddPairwiseI16x8S(vs16(-32768, -32768, 32767, 32767))); got[0] != -65536 || got[1] != 65534 {
		t.Errorf("i32 extadd_s %v", got)
	}
	if got := g32(I32x4ExtaddPairwiseI16x8U(v16(65535, 65535, 1, 2))); got[0] != 131070 || got[1] != 3 {
		t.Errorf("i32 extadd_u %v", got)
	}

	d1 := vs16(-32768, -32768, 1, 2, 3, 4, -5, 6)
	d2 := vs16(-32768, -32768, 3, 4, 5, 6, 7, 8)
	if got := gs32(I32x4DotI16x8S(d1, d2)); got != [4]int32{math.MinInt32, 11, 39, 13} {
		t.Errorf("dot %v", got)
	}
}

func TestSIMDQ15(t *testing.T) {
	a := vs16(-32768, 16384, -16384, 32767, 1, -1, 0x4000, -32768)
	b := vs16(-32768, 16384, 16384, 32767, 1, 1, 1, 32767)
	want := [8]int16{32767, 8192, -8192, 32766, 0, 0, 1, -32767}
	if got := gs16(I16x8Q15mulrSatS(a, b)); got != want {
		t.Errorf("q15mulr_sat_s %v", got)
	}
	if got := gs16(I16x8RelaxedQ15mulrS(a, b)); got != want {
		t.Errorf("relaxed_q15mulr_s %v", got)
	}
	// Rounding: 1*0x4000 => (0x4000+0x4000)>>15 = 1; -1*0x4000 => 0.
	if got := gs16(I16x8Q15mulrSatS(vs16(0x4000, -0x4000, 3), vs16(1, 1, 0x4000))); got[0] != 1 || got[1] != 0 || got[2] != 2 {
		t.Errorf("q15 rounding %v", got)
	}
}

// ---------------------------------------------------------------------------
// Float operations

func TestSIMDFloatMinMax(t *testing.T) {
	a := vf32(tnan32, 0, negz32, 1)
	b := vf32(1, negz32, 0, tnan32)
	eqF32(t, "f32 min", F32x4Min(a, b), tnan32, negz32, negz32, tnan32)
	eqF32(t, "f32 max", F32x4Max(a, b), tnan32, 0, 0, tnan32)
	eqF32(t, "f32 relaxed_min", F32x4RelaxedMin(a, b), tnan32, negz32, negz32, tnan32)
	eqF32(t, "f32 relaxed_max", F32x4RelaxedMax(a, b), tnan32, 0, 0, tnan32)
	// pmin(a,b) = b<a ? b : a ; pmax(a,b) = a<b ? b : a
	eqF32(t, "f32 pmin", F32x4Pmin(a, b), tnan32, 0, negz32, 1)
	eqF32(t, "f32 pmax", F32x4Pmax(a, b), tnan32, 0, negz32, 1)
	eqF32(t, "f32 pmin nan b", F32x4Pmin(vf32(1, 2, -inf32, 5), vf32(tnan32, 1, inf32, 3)), 1, 1, -inf32, 3)
	eqF32(t, "f32 pmax nan b", F32x4Pmax(vf32(1, 2, -inf32, 5), vf32(tnan32, 1, inf32, 3)), 1, 2, inf32, 5)
	eqF32(t, "f32 min plain", F32x4Min(vf32(1, -inf32, 3, 4), vf32(2, 5, -3, 4)), 1, -inf32, -3, 4)
	eqF32(t, "f32 max plain", F32x4Max(vf32(1, -inf32, 3, 4), vf32(2, 5, -3, 4)), 2, 5, 3, 4)
	if g := gf32(F32x4Min(a, b)); g[0] == g[0] || g[3] == g[3] {
		t.Error("f32 min NaN")
	}

	eqF64(t, "f64 min", F64x2Min(vf64(nan64v, 0), vf64(1, negz64)), nan64v, negz64)
	eqF64(t, "f64 min", F64x2Min(vf64(negz64, 1), vf64(0, nan64v)), negz64, nan64v)
	eqF64(t, "f64 max", F64x2Max(vf64(nan64v, 0), vf64(1, negz64)), nan64v, 0)
	eqF64(t, "f64 max", F64x2Max(vf64(negz64, 3), vf64(0, 2)), 0, 3)
	eqF64(t, "f64 relaxed_min", F64x2RelaxedMin(vf64(negz64, 1), vf64(0, 2)), negz64, 1)
	eqF64(t, "f64 relaxed_max", F64x2RelaxedMax(vf64(negz64, 1), vf64(0, 2)), 0, 2)
	eqF64(t, "f64 pmin", F64x2Pmin(vf64(nan64v, 0), vf64(1, negz64)), nan64v, 0)
	eqF64(t, "f64 pmin", F64x2Pmin(vf64(1, 3), vf64(nan64v, 2)), 1, 2)
	eqF64(t, "f64 pmax", F64x2Pmax(vf64(negz64, 1), vf64(0, nan64v)), negz64, 1)
	eqF64(t, "f64 pmax", F64x2Pmax(vf64(1, 3), vf64(2, 2)), 2, 3)
}

func TestSIMDFloatRounding(t *testing.T) {
	eqF32(t, "f32 nearest", F32x4Nearest(vf32(0.5, 1.5, 2.5, -0.5)), 0, 2, 2, negz32)
	eqF32(t, "f32 nearest2", F32x4Nearest(vf32(-1.5, -2.5, 8388609, tnan32)), -2, -2, 8388609, tnan32)
	eqF32(t, "f32 ceil", F32x4Ceil(vf32(-0.5, 1.1, -1.1, inf32)), negz32, 2, -1, inf32)
	eqF32(t, "f32 floor", F32x4Floor(vf32(-0.5, 1.9, -1.1, negz32)), -1, 1, -2, negz32)
	eqF32(t, "f32 trunc", F32x4Trunc(vf32(-0.5, 1.9, -1.9, tnan32)), negz32, 1, -1, tnan32)
	eqF64(t, "f64 nearest", F64x2Nearest(vf64(2.5, -3.5)), 2, -4)
	eqF64(t, "f64 nearest", F64x2Nearest(vf64(-0.5, 4503599627370497)), negz64, 4503599627370497)
	eqF64(t, "f64 ceil", F64x2Ceil(vf64(-0.5, 1.1)), negz64, 2)
	eqF64(t, "f64 floor", F64x2Floor(vf64(-0.5, 1.9)), -1, 1)
	eqF64(t, "f64 trunc", F64x2Trunc(vf64(-0.7, 1.9)), negz64, 1)
}

func TestSIMDFloatArith(t *testing.T) {
	f32p1, f32p2 := float32(0.1), float32(0.2)
	f64p1, f64p2 := 0.1, 0.2
	eqF32(t, "f32 add", F32x4Add(vf32(1, inf32, 0.1, 3e38), vf32(2, -inf32, 0.2, 3e38)), 3, tnan32, f32p1+f32p2, inf32)
	eqF32(t, "f32 sub", F32x4Sub(vf32(1, 0, negz32, 1), vf32(2, 0, 0, 1)), -1, 0, negz32, 0)
	eqF32(t, "f32 mul", F32x4Mul(vf32(1.5, 0, -2, 1e30), vf32(2, inf32, 3, 1e30)), 3, tnan32, -6, inf32)
	eqF32(t, "f32 div", F32x4Div(vf32(1, 0, -1, 1), vf32(4, 0, 0, 3)), 0.25, tnan32, -inf32, float32(1)/float32(3))
	eqF32(t, "f32 sqrt", F32x4Sqrt(vf32(4, -1, negz32, 2)), 2, tnan32, negz32, float32(math.Sqrt(2)))
	eqF64(t, "f64 add", F64x2Add(vf64(0.1, 1), vf64(0.2, 2)), f64p1+f64p2, 3)
	eqF64(t, "f64 sub", F64x2Sub(vf64(1, negz64), vf64(2, 0)), -1, negz64)
	eqF64(t, "f64 mul", F64x2Mul(vf64(1.5, 0), vf64(2, inf64)), 3, nan64v)
	eqF64(t, "f64 div", F64x2Div(vf64(1, -1), vf64(3, 0)), 1.0/3, -inf64)
	eqF64(t, "f64 sqrt", F64x2Sqrt(vf64(9, -1)), 3, nan64v)

	// abs/neg are pure bit operations that preserve NaN payloads.
	in := v32(0xffa00001, 0x7fc00000, 0x80000000, 0x3f800000)
	eqV(t, "f32 abs", F32x4Abs(in), v32(0x7fa00001, 0x7fc00000, 0, 0x3f800000))
	eqV(t, "f32 neg", F32x4Neg(in), v32(0x7fa00001, 0xffc00000, 0, 0xbf800000))
	in64 := V128{0xfff4000000000001, 0x3ff0000000000000}
	eqV(t, "f64 abs", F64x2Abs(in64), V128{0x7ff4000000000001, 0x3ff0000000000000})
	eqV(t, "f64 neg", F64x2Neg(in64), V128{0x7ff4000000000001, 0xbff0000000000000})
}

func TestSIMDMadd(t *testing.T) {
	// Values where fused and unfused results differ.
	x := float32(1 + 1.0/(1<<12))
	z := -float32(1 + 1.0/(1<<11))
	// x*x = 1 + 2^-11 + 2^-24 ; rounded to float32 = 1 + 2^-11 (tie to even), so result 0.
	eqF32(t, "f32 madd", F32x4RelaxedMadd(vf32(x, 2, 1, 0), vf32(x, 3, 1, 0), vf32(z, 1, -1, 5)), 0, 7, 0, 5)
	eqF32(t, "f32 nmadd", F32x4RelaxedNmadd(vf32(x, 2, 1, 0), vf32(x, 3, 1, 0), vf32(-z, 1, 1, 5)), 0, -5, 0, 5)
	y := 1 + 1.0/(1<<27)
	w := -(1 + 1.0/(1<<26))
	eqF64(t, "f64 madd", F64x2RelaxedMadd(vf64(y, 2), vf64(y, 3), vf64(w, 1)), 0, 7)
	eqF64(t, "f64 nmadd", F64x2RelaxedNmadd(vf64(y, 2), vf64(y, 3), vf64(-w, 1)), 0, -5)
}

func TestSIMDConversions(t *testing.T) {
	// trunc_sat f32 -> i32 signed
	big := float32(2147483648)
	ts := vf32(tnan32, big, -big*2, -1.9)
	want := [4]int32{0, math.MaxInt32, math.MinInt32, -1}
	if got := gs32(I32x4TruncSatF32x4S(ts)); got != want {
		t.Errorf("trunc_sat_f32_s %v", got)
	}
	if got := gs32(I32x4RelaxedTruncF32x4S(ts)); got != want {
		t.Errorf("relaxed_trunc_f32_s %v", got)
	}
	if got := gs32(I32x4TruncSatF32x4S(vf32(-big, 2147483520, inf32, -inf32))); got != [4]int32{math.MinInt32, 2147483520, math.MaxInt32, math.MinInt32} {
		t.Errorf("trunc_sat_f32_s edges %v", got)
	}
	tu := vf32(tnan32, -0.9, 4294967296, 4294967040)
	wantU := [4]uint32{0, 0, math.MaxUint32, 4294967040}
	if got := g32(I32x4TruncSatF32x4U(tu)); got != wantU {
		t.Errorf("trunc_sat_f32_u %v", got)
	}
	if got := g32(I32x4RelaxedTruncF32x4U(tu)); got != wantU {
		t.Errorf("relaxed_trunc_f32_u %v", got)
	}
	if got := g32(I32x4TruncSatF32x4U(vf32(-inf32, inf32, 1.9, -5))); got != [4]uint32{0, math.MaxUint32, 1, 0} {
		t.Errorf("trunc_sat_f32_u edges %v", got)
	}

	// f64 -> i32, high lanes zero.
	if got := gs32(I32x4TruncSatF64x2SZero(vf64(2147483647.9, -2147483648.9))); got != [4]int32{math.MaxInt32, math.MinInt32, 0, 0} {
		t.Errorf("trunc_sat_f64_s_zero %v", got)
	}
	if got := gs32(I32x4TruncSatF64x2SZero(vf64(nan64v, 2147483648))); got != [4]int32{0, math.MaxInt32, 0, 0} {
		t.Errorf("trunc_sat_f64_s_zero nan %v", got)
	}
	if got := gs32(I32x4RelaxedTruncF64x2SZero(vf64(-1.5, inf64))); got != [4]int32{-1, math.MaxInt32, 0, 0} {
		t.Errorf("relaxed_trunc_f64_s_zero %v", got)
	}
	if got := g32(I32x4TruncSatF64x2UZero(vf64(4294967295.9, -0.5))); got != [4]uint32{math.MaxUint32, 0, 0, 0} {
		t.Errorf("trunc_sat_f64_u_zero %v", got)
	}
	if got := g32(I32x4TruncSatF64x2UZero(vf64(nan64v, 4294967296))); got != [4]uint32{0, math.MaxUint32, 0, 0} {
		t.Errorf("trunc_sat_f64_u_zero nan %v", got)
	}
	if got := g32(I32x4RelaxedTruncF64x2UZero(vf64(7.9, -inf64))); got != [4]uint32{7, 0, 0, 0} {
		t.Errorf("relaxed_trunc_f64_u_zero %v", got)
	}

	// convert
	eqF32(t, "convert_s", F32x4ConvertI32x4S(vs32(-1, math.MinInt32, 16777217, math.MaxInt32)), -1, -2147483648, 16777216, 2147483648)
	eqF32(t, "convert_u", F32x4ConvertI32x4U(v32(0xffffffff, 16777217, 16777219, 1)), 4294967296, 16777216, 16777220, 1)
	eqF64(t, "convert_low_s", F64x2ConvertLowI32x4S(vs32(-1, math.MinInt32, 5, 6)), -1, -2147483648)
	eqF64(t, "convert_low_u", F64x2ConvertLowI32x4U(v32(0xffffffff, 2, 5, 6)), 4294967295, 2)

	// demote / promote
	d := F32x4DemoteF64x2Zero(vf64(1e300, 1+1.0/(1<<24)))
	eqF32(t, "demote", d, inf32, 1, 0, 0)
	if d.Hi != 0 {
		t.Error("demote high lanes not zero")
	}
	// Tie that rounds up to even: 1 + 3*2^-24 -> 1 + 2^-22.
	eqF32(t, "demote tie", F32x4DemoteF64x2Zero(vf64(1+3.0/(1<<24), nan64v)), 1+1.0/(1<<22), tnan32)
	eqF64(t, "promote", F64x2PromoteLowF32x4(vf32(1.5, negz32, 99, 100)), 1.5, negz64)
	eqF64(t, "promote nan", F64x2PromoteLowF32x4(vf32(tnan32, inf32)), nan64v, inf64)
}

func TestSIMDRelaxedDot(t *testing.T) {
	a := vs8(-128, -128, 127, 127, -1, 2, 3, 4, 10, -10, 0, 0, 1, 1, 1, 1)
	b := vs8(-128, -128, 63, 63, 5, 6, 7, 8, 10, 10, 0, 0, -64, 63, 2, 2)
	got := gs16(I16x8RelaxedDotI8x16I7x16S(a, b))
	want := [8]int16{-32768, 127 * 63 * 2, -5 + 12, 21 + 32, 0, 0, -64 + 63, 4}
	if got != want {
		t.Errorf("relaxed_dot %v want %v", got, want)
	}
	c := vs32(1, -1, 100, math.MaxInt32)
	got2 := gs32(I32x4RelaxedDotI8x16I7x16AddS(a, b, c))
	want2 := [4]int32{1 + 32768 + 127*63*2, -1 + 7 + 53, 100 + 0, math.MinInt32 + 2} // last lane wraps: MaxInt32 + 3
	if got2 != want2 {
		t.Errorf("relaxed_dot_add %v want %v", got2, want2)
	}
}
