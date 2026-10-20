package rt

// SIMD (v128) operations following the WebAssembly 2.0 spec, plus
// deterministic choices for the relaxed-SIMD instructions.
//
// V128 is reinterpreted as lane arrays via unsafe; this assumes a
// little-endian host.

import (
	"math"
	"math/bits"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Lane helpers

type simdElem interface {
	~int8 | ~uint8 | ~int16 | ~uint16 | ~int32 | ~uint32 | ~int64 | ~uint64 | ~float32 | ~float64
}

// simdLanes returns a slice aliasing the lanes of *v.
func simdLanes[E simdElem](v *V128) []E {
	var e E
	return unsafe.Slice((*E)(unsafe.Pointer(v)), 16/int(unsafe.Sizeof(e)))
}

func simdUn[E simdElem](a V128, f func(E) E) V128 {
	var r V128
	x, d := simdLanes[E](&a), simdLanes[E](&r)
	for i := range d {
		d[i] = f(x[i])
	}
	return r
}

func simdBin[E simdElem](a, b V128, f func(E, E) E) V128 {
	var r V128
	x, y, d := simdLanes[E](&a), simdLanes[E](&b), simdLanes[E](&r)
	for i := range d {
		d[i] = f(x[i], y[i])
	}
	return r
}

func simdTern[E simdElem](a, b, c V128, f func(E, E, E) E) V128 {
	var r V128
	x, y, z, d := simdLanes[E](&a), simdLanes[E](&b), simdLanes[E](&c), simdLanes[E](&r)
	for i := range d {
		d[i] = f(x[i], y[i], z[i])
	}
	return r
}

// simdCmp produces all-ones lanes where f is true and all-zeros otherwise.
func simdCmp[E simdElem](a, b V128, f func(E, E) bool) V128 {
	var r V128
	x, y := simdLanes[E](&a), simdLanes[E](&b)
	w := 16 / len(x)
	d := simdLanes[uint8](&r)
	for i := range x {
		if f(x[i], y[i]) {
			for j := 0; j < w; j++ {
				d[i*w+j] = 0xFF
			}
		}
	}
	return r
}

// simdSplat fills every lane with x.
func simdSplat[E simdElem](x E) V128 {
	var r V128
	d := simdLanes[E](&r)
	for i := range d {
		d[i] = x
	}
	return r
}

func simdExtract[E simdElem](v V128, lane int) E { return simdLanes[E](&v)[lane] }

func simdReplace[E simdElem](v V128, lane int, x E) V128 {
	simdLanes[E](&v)[lane] = x
	return v
}

// simdBitmask collects the top bit of each lane (lanes viewed as unsigned
// via signed comparison).
func simdBitmask[E ~int8 | ~int16 | ~int32 | ~int64](v V128) int32 {
	var m int32
	for i, x := range simdLanes[E](&v) {
		if x < 0 {
			m |= 1 << i
		}
	}
	return m
}

func simdAllTrue[E simdElem](v V128) int32 {
	for _, x := range simdLanes[E](&v) {
		if x == 0 {
			return 0
		}
	}
	return 1
}

// simdWiden builds a vector of wide lanes from the narrow lanes [off, off+n).
func simdWiden[N, W simdElem](v V128, off int, f func(N) W) V128 {
	var r V128
	x, d := simdLanes[N](&v), simdLanes[W](&r)
	for i := range d {
		d[i] = f(x[off+i])
	}
	return r
}

// simdExtmul multiplies the narrow lanes [off, off+n) of a and b into wide lanes.
func simdExtmul[N, W simdElem](a, b V128, off int, f func(N, N) W) V128 {
	var r V128
	x, y, d := simdLanes[N](&a), simdLanes[N](&b), simdLanes[W](&r)
	for i := range d {
		d[i] = f(x[off+i], y[off+i])
	}
	return r
}

// simdPairwise combines adjacent narrow lanes into wide lanes.
func simdPairwise[N, W simdElem](v V128, f func(N, N) W) V128 {
	var r V128
	x, d := simdLanes[N](&v), simdLanes[W](&r)
	for i := range d {
		d[i] = f(x[2*i], x[2*i+1])
	}
	return r
}

func sat8s(x int32) int8 {
	if x < math.MinInt8 {
		return math.MinInt8
	}
	if x > math.MaxInt8 {
		return math.MaxInt8
	}
	return int8(x)
}

func sat8u(x int32) uint8 {
	if x < 0 {
		return 0
	}
	if x > math.MaxUint8 {
		return math.MaxUint8
	}
	return uint8(x)
}

func sat16s(x int32) int16 {
	if x < math.MinInt16 {
		return math.MinInt16
	}
	if x > math.MaxInt16 {
		return math.MaxInt16
	}
	return int16(x)
}

func sat16u(x int32) uint16 {
	if x < 0 {
		return 0
	}
	if x > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(x)
}

func simdTruncSatS(x float64) int32 {
	switch {
	case x != x:
		return 0
	case x <= math.MinInt32:
		return math.MinInt32
	case x >= 2147483648.0:
		return math.MaxInt32
	}
	return int32(x)
}

func simdTruncSatU(x float64) uint32 {
	switch {
	case !(x > 0): // NaN, negative, zero
		return 0
	case x >= 4294967296.0:
		return math.MaxUint32
	}
	return uint32(x)
}

// Wasm float min/max: NaN-propagating, -0 < +0.
func simdFmin32(a, b float32) float32 { return F32Min(a, b) }

func simdFmax32(a, b float32) float32 { return F32Max(a, b) }

func simdFmin64(a, b float64) float64 { return F64Min(a, b) }

func simdFmax64(a, b float64) float64 { return F64Max(a, b) }

// ---------------------------------------------------------------------------
// Shuffles, splats, lanes

func I8x16Shuffle(a, b V128, lanes [16]byte) V128 {
	var r V128
	x, y, d := simdLanes[uint8](&a), simdLanes[uint8](&b), simdLanes[uint8](&r)
	for i, l := range lanes {
		l &= 31
		if l < 16 {
			d[i] = x[l]
		} else {
			d[i] = y[l-16]
		}
	}
	return r
}

func I8x16Swizzle(a, s V128) V128 {
	var r V128
	x, idx, d := simdLanes[uint8](&a), simdLanes[uint8](&s), simdLanes[uint8](&r)
	for i, l := range idx {
		if l < 16 {
			d[i] = x[l]
		}
	}
	return r
}

func I8x16Splat(x int32) V128   { return simdSplat(uint8(x)) }
func I16x8Splat(x int32) V128   { return simdSplat(uint16(x)) }
func I32x4Splat(x int32) V128   { return simdSplat(uint32(x)) }
func I64x2Splat(x int64) V128   { return V128{uint64(x), uint64(x)} }
func F32x4Splat(x float32) V128 { return simdSplat(math.Float32bits(x)) }
func F64x2Splat(x float64) V128 { b := math.Float64bits(x); return V128{b, b} }

func I8x16ExtractLaneS(v V128, lane int) int32 { return int32(simdExtract[int8](v, lane)) }
func I8x16ExtractLaneU(v V128, lane int) int32 { return int32(simdExtract[uint8](v, lane)) }
func I16x8ExtractLaneS(v V128, lane int) int32 { return int32(simdExtract[int16](v, lane)) }
func I16x8ExtractLaneU(v V128, lane int) int32 { return int32(simdExtract[uint16](v, lane)) }
func I32x4ExtractLane(v V128, lane int) int32  { return simdExtract[int32](v, lane) }
func I64x2ExtractLane(v V128, lane int) int64  { return simdExtract[int64](v, lane) }
func F32x4ExtractLane(v V128, lane int) float32 {
	return math.Float32frombits(simdExtract[uint32](v, lane))
}
func F64x2ExtractLane(v V128, lane int) float64 {
	return math.Float64frombits(simdExtract[uint64](v, lane))
}

func I8x16ReplaceLane(v V128, lane int, x int32) V128 { return simdReplace(v, lane, uint8(x)) }
func I16x8ReplaceLane(v V128, lane int, x int32) V128 { return simdReplace(v, lane, uint16(x)) }
func I32x4ReplaceLane(v V128, lane int, x int32) V128 { return simdReplace(v, lane, uint32(x)) }
func I64x2ReplaceLane(v V128, lane int, x int64) V128 { return simdReplace(v, lane, uint64(x)) }
func F32x4ReplaceLane(v V128, lane int, x float32) V128 {
	return simdReplace(v, lane, math.Float32bits(x))
}
func F64x2ReplaceLane(v V128, lane int, x float64) V128 {
	return simdReplace(v, lane, math.Float64bits(x))
}

// ---------------------------------------------------------------------------
// Loads (operate on the 64 raw little-endian bits read from memory)

func V128Load8x8S(x uint64) V128 {
	return simdWiden(V128{Lo: x}, 0, func(a int8) int16 { return int16(a) })
}
func V128Load8x8U(x uint64) V128 {
	return simdWiden(V128{Lo: x}, 0, func(a uint8) uint16 { return uint16(a) })
}
func V128Load16x4S(x uint64) V128 {
	return simdWiden(V128{Lo: x}, 0, func(a int16) int32 { return int32(a) })
}
func V128Load16x4U(x uint64) V128 {
	return simdWiden(V128{Lo: x}, 0, func(a uint16) uint32 { return uint32(a) })
}
func V128Load32x2S(x uint64) V128 {
	return V128{uint64(int64(int32(x))), uint64(int64(int32(x >> 32)))}
}
func V128Load32x2U(x uint64) V128 {
	return V128{uint64(uint32(x)), x >> 32}
}

// ---------------------------------------------------------------------------
// Comparisons

func I8x16Eq(a, b V128) V128  { return simdCmp(a, b, func(x, y uint8) bool { return x == y }) }
func I8x16Ne(a, b V128) V128  { return simdCmp(a, b, func(x, y uint8) bool { return x != y }) }
func I8x16LtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int8) bool { return x < y }) }
func I8x16LtU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint8) bool { return x < y }) }
func I8x16GtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int8) bool { return x > y }) }
func I8x16GtU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint8) bool { return x > y }) }
func I8x16LeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int8) bool { return x <= y }) }
func I8x16LeU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint8) bool { return x <= y }) }
func I8x16GeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int8) bool { return x >= y }) }
func I8x16GeU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint8) bool { return x >= y }) }

func I16x8Eq(a, b V128) V128  { return simdCmp(a, b, func(x, y uint16) bool { return x == y }) }
func I16x8Ne(a, b V128) V128  { return simdCmp(a, b, func(x, y uint16) bool { return x != y }) }
func I16x8LtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int16) bool { return x < y }) }
func I16x8LtU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint16) bool { return x < y }) }
func I16x8GtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int16) bool { return x > y }) }
func I16x8GtU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint16) bool { return x > y }) }
func I16x8LeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int16) bool { return x <= y }) }
func I16x8LeU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint16) bool { return x <= y }) }
func I16x8GeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int16) bool { return x >= y }) }
func I16x8GeU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint16) bool { return x >= y }) }

func I32x4Eq(a, b V128) V128  { return simdCmp(a, b, func(x, y uint32) bool { return x == y }) }
func I32x4Ne(a, b V128) V128  { return simdCmp(a, b, func(x, y uint32) bool { return x != y }) }
func I32x4LtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int32) bool { return x < y }) }
func I32x4LtU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint32) bool { return x < y }) }
func I32x4GtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int32) bool { return x > y }) }
func I32x4GtU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint32) bool { return x > y }) }
func I32x4LeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int32) bool { return x <= y }) }
func I32x4LeU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint32) bool { return x <= y }) }
func I32x4GeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int32) bool { return x >= y }) }
func I32x4GeU(a, b V128) V128 { return simdCmp(a, b, func(x, y uint32) bool { return x >= y }) }

func I64x2Eq(a, b V128) V128  { return simdCmp(a, b, func(x, y uint64) bool { return x == y }) }
func I64x2Ne(a, b V128) V128  { return simdCmp(a, b, func(x, y uint64) bool { return x != y }) }
func I64x2LtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int64) bool { return x < y }) }
func I64x2GtS(a, b V128) V128 { return simdCmp(a, b, func(x, y int64) bool { return x > y }) }
func I64x2LeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int64) bool { return x <= y }) }
func I64x2GeS(a, b V128) V128 { return simdCmp(a, b, func(x, y int64) bool { return x >= y }) }

func F32x4Eq(a, b V128) V128 { return simdCmp(a, b, func(x, y float32) bool { return x == y }) }
func F32x4Ne(a, b V128) V128 { return simdCmp(a, b, func(x, y float32) bool { return x != y }) }
func F32x4Lt(a, b V128) V128 { return simdCmp(a, b, func(x, y float32) bool { return x < y }) }
func F32x4Gt(a, b V128) V128 { return simdCmp(a, b, func(x, y float32) bool { return x > y }) }
func F32x4Le(a, b V128) V128 { return simdCmp(a, b, func(x, y float32) bool { return x <= y }) }
func F32x4Ge(a, b V128) V128 { return simdCmp(a, b, func(x, y float32) bool { return x >= y }) }

func F64x2Eq(a, b V128) V128 { return simdCmp(a, b, func(x, y float64) bool { return x == y }) }
func F64x2Ne(a, b V128) V128 { return simdCmp(a, b, func(x, y float64) bool { return x != y }) }
func F64x2Lt(a, b V128) V128 { return simdCmp(a, b, func(x, y float64) bool { return x < y }) }
func F64x2Gt(a, b V128) V128 { return simdCmp(a, b, func(x, y float64) bool { return x > y }) }
func F64x2Le(a, b V128) V128 { return simdCmp(a, b, func(x, y float64) bool { return x <= y }) }
func F64x2Ge(a, b V128) V128 { return simdCmp(a, b, func(x, y float64) bool { return x >= y }) }

// ---------------------------------------------------------------------------
// Bitwise

func V128Not(a V128) V128       { return V128{^a.Lo, ^a.Hi} }
func V128And(a, b V128) V128    { return V128{a.Lo & b.Lo, a.Hi & b.Hi} }
func V128Andnot(a, b V128) V128 { return V128{a.Lo &^ b.Lo, a.Hi &^ b.Hi} }
func V128Or(a, b V128) V128     { return V128{a.Lo | b.Lo, a.Hi | b.Hi} }
func V128Xor(a, b V128) V128    { return V128{a.Lo ^ b.Lo, a.Hi ^ b.Hi} }

// V128Bitselect takes bits from a where c is 1 and from b where c is 0.
func V128Bitselect(a, b, c V128) V128 {
	return V128{(a.Lo & c.Lo) | (b.Lo &^ c.Lo), (a.Hi & c.Hi) | (b.Hi &^ c.Hi)}
}

func V128AnyTrue(a V128) int32 {
	if a.Lo|a.Hi != 0 {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// i8x16

func I8x16Abs(a V128) V128 {
	return simdUn(a, func(x int8) int8 {
		if x < 0 {
			return -x
		}
		return x
	})
}
func I8x16Neg(a V128) V128 { return simdUn(a, func(x int8) int8 { return -x }) }
func I8x16Popcnt(a V128) V128 {
	return simdUn(a, func(x uint8) uint8 { return uint8(bits.OnesCount8(x)) })
}
func I8x16AllTrue(a V128) int32 { return simdAllTrue[uint8](a) }
func I8x16Bitmask(a V128) int32 { return simdBitmask[int8](a) }

func I8x16NarrowI16x8S(a, b V128) V128 {
	var r V128
	x, y, d := simdLanes[int16](&a), simdLanes[int16](&b), simdLanes[int8](&r)
	for i := 0; i < 8; i++ {
		d[i] = sat8s(int32(x[i]))
		d[8+i] = sat8s(int32(y[i]))
	}
	return r
}

func I8x16NarrowI16x8U(a, b V128) V128 {
	var r V128
	x, y, d := simdLanes[int16](&a), simdLanes[int16](&b), simdLanes[uint8](&r)
	for i := 0; i < 8; i++ {
		d[i] = sat8u(int32(x[i]))
		d[8+i] = sat8u(int32(y[i]))
	}
	return r
}

func I8x16Shl(a V128, n int32) V128 {
	s := uint(n) & 7
	return simdUn(a, func(x uint8) uint8 { return x << s })
}
func I8x16ShrS(a V128, n int32) V128 {
	s := uint(n) & 7
	return simdUn(a, func(x int8) int8 { return x >> s })
}
func I8x16ShrU(a V128, n int32) V128 {
	s := uint(n) & 7
	return simdUn(a, func(x uint8) uint8 { return x >> s })
}

func I8x16Add(a, b V128) V128 { return simdBin(a, b, func(x, y uint8) uint8 { return x + y }) }
func I8x16Sub(a, b V128) V128 { return simdBin(a, b, func(x, y uint8) uint8 { return x - y }) }
func I8x16AddSatS(a, b V128) V128 {
	return simdBin(a, b, func(x, y int8) int8 { return sat8s(int32(x) + int32(y)) })
}
func I8x16AddSatU(a, b V128) V128 {
	return simdBin(a, b, func(x, y uint8) uint8 { return sat8u(int32(x) + int32(y)) })
}
func I8x16SubSatS(a, b V128) V128 {
	return simdBin(a, b, func(x, y int8) int8 { return sat8s(int32(x) - int32(y)) })
}
func I8x16SubSatU(a, b V128) V128 {
	return simdBin(a, b, func(x, y uint8) uint8 { return sat8u(int32(x) - int32(y)) })
}
func I8x16MinS(a, b V128) V128 { return simdBin(a, b, func(x, y int8) int8 { return min(x, y) }) }
func I8x16MinU(a, b V128) V128 { return simdBin(a, b, func(x, y uint8) uint8 { return min(x, y) }) }
func I8x16MaxS(a, b V128) V128 { return simdBin(a, b, func(x, y int8) int8 { return max(x, y) }) }
func I8x16MaxU(a, b V128) V128 { return simdBin(a, b, func(x, y uint8) uint8 { return max(x, y) }) }
func I8x16AvgrU(a, b V128) V128 {
	return simdBin(a, b, func(x, y uint8) uint8 { return uint8((uint16(x) + uint16(y) + 1) / 2) })
}

// ---------------------------------------------------------------------------
// i16x8

func I16x8ExtaddPairwiseI8x16S(a V128) V128 {
	return simdPairwise(a, func(x, y int8) int16 { return int16(x) + int16(y) })
}
func I16x8ExtaddPairwiseI8x16U(a V128) V128 {
	return simdPairwise(a, func(x, y uint8) uint16 { return uint16(x) + uint16(y) })
}

func I16x8Abs(a V128) V128 {
	return simdUn(a, func(x int16) int16 {
		if x < 0 {
			return -x
		}
		return x
	})
}
func I16x8Neg(a V128) V128 { return simdUn(a, func(x int16) int16 { return -x }) }

func I16x8Q15mulrSatS(a, b V128) V128 {
	return simdBin(a, b, func(x, y int16) int16 {
		return sat16s((int32(x)*int32(y) + 0x4000) >> 15)
	})
}

func I16x8AllTrue(a V128) int32 { return simdAllTrue[uint16](a) }
func I16x8Bitmask(a V128) int32 { return simdBitmask[int16](a) }

func I16x8NarrowI32x4S(a, b V128) V128 {
	var r V128
	x, y, d := simdLanes[int32](&a), simdLanes[int32](&b), simdLanes[int16](&r)
	for i := 0; i < 4; i++ {
		d[i] = sat16s(x[i])
		d[4+i] = sat16s(y[i])
	}
	return r
}

func I16x8NarrowI32x4U(a, b V128) V128 {
	var r V128
	x, y, d := simdLanes[int32](&a), simdLanes[int32](&b), simdLanes[uint16](&r)
	for i := 0; i < 4; i++ {
		d[i] = sat16u(x[i])
		d[4+i] = sat16u(y[i])
	}
	return r
}

func I16x8ExtendLowI8x16S(a V128) V128 {
	return simdWiden(a, 0, func(x int8) int16 { return int16(x) })
}
func I16x8ExtendHighI8x16S(a V128) V128 {
	return simdWiden(a, 8, func(x int8) int16 { return int16(x) })
}
func I16x8ExtendLowI8x16U(a V128) V128 {
	return simdWiden(a, 0, func(x uint8) uint16 { return uint16(x) })
}
func I16x8ExtendHighI8x16U(a V128) V128 {
	return simdWiden(a, 8, func(x uint8) uint16 { return uint16(x) })
}

func I16x8Shl(a V128, n int32) V128 {
	s := uint(n) & 15
	return simdUn(a, func(x uint16) uint16 { return x << s })
}
func I16x8ShrS(a V128, n int32) V128 {
	s := uint(n) & 15
	return simdUn(a, func(x int16) int16 { return x >> s })
}
func I16x8ShrU(a V128, n int32) V128 {
	s := uint(n) & 15
	return simdUn(a, func(x uint16) uint16 { return x >> s })
}

func I16x8Add(a, b V128) V128 { return simdBin(a, b, func(x, y uint16) uint16 { return x + y }) }
func I16x8Sub(a, b V128) V128 { return simdBin(a, b, func(x, y uint16) uint16 { return x - y }) }
func I16x8Mul(a, b V128) V128 { return simdBin(a, b, func(x, y uint16) uint16 { return x * y }) }
func I16x8AddSatS(a, b V128) V128 {
	return simdBin(a, b, func(x, y int16) int16 { return sat16s(int32(x) + int32(y)) })
}
func I16x8AddSatU(a, b V128) V128 {
	return simdBin(a, b, func(x, y uint16) uint16 { return sat16u(int32(x) + int32(y)) })
}
func I16x8SubSatS(a, b V128) V128 {
	return simdBin(a, b, func(x, y int16) int16 { return sat16s(int32(x) - int32(y)) })
}
func I16x8SubSatU(a, b V128) V128 {
	return simdBin(a, b, func(x, y uint16) uint16 { return sat16u(int32(x) - int32(y)) })
}
func I16x8MinS(a, b V128) V128 { return simdBin(a, b, func(x, y int16) int16 { return min(x, y) }) }
func I16x8MinU(a, b V128) V128 { return simdBin(a, b, func(x, y uint16) uint16 { return min(x, y) }) }
func I16x8MaxS(a, b V128) V128 { return simdBin(a, b, func(x, y int16) int16 { return max(x, y) }) }
func I16x8MaxU(a, b V128) V128 { return simdBin(a, b, func(x, y uint16) uint16 { return max(x, y) }) }
func I16x8AvgrU(a, b V128) V128 {
	return simdBin(a, b, func(x, y uint16) uint16 { return uint16((uint32(x) + uint32(y) + 1) / 2) })
}

func I16x8ExtmulLowI8x16S(a, b V128) V128 {
	return simdExtmul(a, b, 0, func(x, y int8) int16 { return int16(x) * int16(y) })
}
func I16x8ExtmulHighI8x16S(a, b V128) V128 {
	return simdExtmul(a, b, 8, func(x, y int8) int16 { return int16(x) * int16(y) })
}
func I16x8ExtmulLowI8x16U(a, b V128) V128 {
	return simdExtmul(a, b, 0, func(x, y uint8) uint16 { return uint16(x) * uint16(y) })
}
func I16x8ExtmulHighI8x16U(a, b V128) V128 {
	return simdExtmul(a, b, 8, func(x, y uint8) uint16 { return uint16(x) * uint16(y) })
}

// ---------------------------------------------------------------------------
// i32x4

func I32x4ExtaddPairwiseI16x8S(a V128) V128 {
	return simdPairwise(a, func(x, y int16) int32 { return int32(x) + int32(y) })
}
func I32x4ExtaddPairwiseI16x8U(a V128) V128 {
	return simdPairwise(a, func(x, y uint16) uint32 { return uint32(x) + uint32(y) })
}

func I32x4Abs(a V128) V128 {
	return simdUn(a, func(x int32) int32 {
		if x < 0 {
			return -x
		}
		return x
	})
}
func I32x4Neg(a V128) V128      { return simdUn(a, func(x int32) int32 { return -x }) }
func I32x4AllTrue(a V128) int32 { return simdAllTrue[uint32](a) }
func I32x4Bitmask(a V128) int32 { return simdBitmask[int32](a) }

func I32x4ExtendLowI16x8S(a V128) V128 {
	return simdWiden(a, 0, func(x int16) int32 { return int32(x) })
}
func I32x4ExtendHighI16x8S(a V128) V128 {
	return simdWiden(a, 4, func(x int16) int32 { return int32(x) })
}
func I32x4ExtendLowI16x8U(a V128) V128 {
	return simdWiden(a, 0, func(x uint16) uint32 { return uint32(x) })
}
func I32x4ExtendHighI16x8U(a V128) V128 {
	return simdWiden(a, 4, func(x uint16) uint32 { return uint32(x) })
}

func I32x4Shl(a V128, n int32) V128 {
	s := uint(n) & 31
	return simdUn(a, func(x uint32) uint32 { return x << s })
}
func I32x4ShrS(a V128, n int32) V128 {
	s := uint(n) & 31
	return simdUn(a, func(x int32) int32 { return x >> s })
}
func I32x4ShrU(a V128, n int32) V128 {
	s := uint(n) & 31
	return simdUn(a, func(x uint32) uint32 { return x >> s })
}

func I32x4Add(a, b V128) V128  { return simdBin(a, b, func(x, y uint32) uint32 { return x + y }) }
func I32x4Sub(a, b V128) V128  { return simdBin(a, b, func(x, y uint32) uint32 { return x - y }) }
func I32x4Mul(a, b V128) V128  { return simdBin(a, b, func(x, y uint32) uint32 { return x * y }) }
func I32x4MinS(a, b V128) V128 { return simdBin(a, b, func(x, y int32) int32 { return min(x, y) }) }
func I32x4MinU(a, b V128) V128 { return simdBin(a, b, func(x, y uint32) uint32 { return min(x, y) }) }
func I32x4MaxS(a, b V128) V128 { return simdBin(a, b, func(x, y int32) int32 { return max(x, y) }) }
func I32x4MaxU(a, b V128) V128 { return simdBin(a, b, func(x, y uint32) uint32 { return max(x, y) }) }

func I32x4DotI16x8S(a, b V128) V128 {
	var r V128
	x, y, d := simdLanes[int16](&a), simdLanes[int16](&b), simdLanes[int32](&r)
	for i := range d {
		d[i] = int32(x[2*i])*int32(y[2*i]) + int32(x[2*i+1])*int32(y[2*i+1]) // wraps
	}
	return r
}

func I32x4ExtmulLowI16x8S(a, b V128) V128 {
	return simdExtmul(a, b, 0, func(x, y int16) int32 { return int32(x) * int32(y) })
}
func I32x4ExtmulHighI16x8S(a, b V128) V128 {
	return simdExtmul(a, b, 4, func(x, y int16) int32 { return int32(x) * int32(y) })
}
func I32x4ExtmulLowI16x8U(a, b V128) V128 {
	return simdExtmul(a, b, 0, func(x, y uint16) uint32 { return uint32(x) * uint32(y) })
}
func I32x4ExtmulHighI16x8U(a, b V128) V128 {
	return simdExtmul(a, b, 4, func(x, y uint16) uint32 { return uint32(x) * uint32(y) })
}

func I32x4TruncSatF32x4S(a V128) V128 {
	var r V128
	x, d := simdLanes[float32](&a), simdLanes[int32](&r)
	for i := range d {
		d[i] = simdTruncSatS(float64(x[i]))
	}
	return r
}

func I32x4TruncSatF32x4U(a V128) V128 {
	var r V128
	x, d := simdLanes[float32](&a), simdLanes[uint32](&r)
	for i := range d {
		d[i] = simdTruncSatU(float64(x[i]))
	}
	return r
}

func I32x4TruncSatF64x2SZero(a V128) V128 {
	var r V128
	x, d := simdLanes[float64](&a), simdLanes[int32](&r)
	d[0], d[1] = simdTruncSatS(x[0]), simdTruncSatS(x[1])
	return r
}

func I32x4TruncSatF64x2UZero(a V128) V128 {
	var r V128
	x, d := simdLanes[float64](&a), simdLanes[uint32](&r)
	d[0], d[1] = simdTruncSatU(x[0]), simdTruncSatU(x[1])
	return r
}

// ---------------------------------------------------------------------------
// i64x2

func I64x2Abs(a V128) V128 {
	return simdUn(a, func(x int64) int64 {
		if x < 0 {
			return -x
		}
		return x
	})
}
func I64x2Neg(a V128) V128      { return V128{-a.Lo, -a.Hi} }
func I64x2AllTrue(a V128) int32 { return simdAllTrue[uint64](a) }
func I64x2Bitmask(a V128) int32 { return int32(a.Lo>>63 | (a.Hi>>63)<<1) }

func I64x2ExtendLowI32x4S(a V128) V128  { return V128Load32x2S(a.Lo) }
func I64x2ExtendHighI32x4S(a V128) V128 { return V128Load32x2S(a.Hi) }
func I64x2ExtendLowI32x4U(a V128) V128  { return V128Load32x2U(a.Lo) }
func I64x2ExtendHighI32x4U(a V128) V128 { return V128Load32x2U(a.Hi) }

func I64x2Shl(a V128, n int32) V128 {
	s := uint(n) & 63
	return V128{a.Lo << s, a.Hi << s}
}
func I64x2ShrS(a V128, n int32) V128 {
	s := uint(n) & 63
	return V128{uint64(int64(a.Lo) >> s), uint64(int64(a.Hi) >> s)}
}
func I64x2ShrU(a V128, n int32) V128 {
	s := uint(n) & 63
	return V128{a.Lo >> s, a.Hi >> s}
}

func I64x2Add(a, b V128) V128 { return V128{a.Lo + b.Lo, a.Hi + b.Hi} }
func I64x2Sub(a, b V128) V128 { return V128{a.Lo - b.Lo, a.Hi - b.Hi} }
func I64x2Mul(a, b V128) V128 { return V128{a.Lo * b.Lo, a.Hi * b.Hi} }

func I64x2ExtmulLowI32x4S(a, b V128) V128 {
	return simdExtmul(a, b, 0, func(x, y int32) int64 { return int64(x) * int64(y) })
}
func I64x2ExtmulHighI32x4S(a, b V128) V128 {
	return simdExtmul(a, b, 2, func(x, y int32) int64 { return int64(x) * int64(y) })
}
func I64x2ExtmulLowI32x4U(a, b V128) V128 {
	return simdExtmul(a, b, 0, func(x, y uint32) uint64 { return uint64(x) * uint64(y) })
}
func I64x2ExtmulHighI32x4U(a, b V128) V128 {
	return simdExtmul(a, b, 2, func(x, y uint32) uint64 { return uint64(x) * uint64(y) })
}

// ---------------------------------------------------------------------------
// f32x4

const (
	simdSign32 = 0x8000000080000000
	simdSign64 = 0x8000000000000000
)

func F32x4Abs(a V128) V128 { return V128{a.Lo &^ simdSign32, a.Hi &^ simdSign32} }
func F32x4Neg(a V128) V128 { return V128{a.Lo ^ simdSign32, a.Hi ^ simdSign32} }

func F32x4Ceil(a V128) V128 {
	return simdUn(a, F32Ceil)
}
func F32x4Floor(a V128) V128 {
	return simdUn(a, F32Floor)
}
func F32x4Trunc(a V128) V128 {
	return simdUn(a, F32Trunc)
}
func F32x4Nearest(a V128) V128 {
	return simdUn(a, F32Nearest)
}
func F32x4Sqrt(a V128) V128 {
	return simdUn(a, F32Sqrt)
}

func F32x4Add(a, b V128) V128 { return simdBin(a, b, func(x, y float32) float32 { return x + y }) }
func F32x4Sub(a, b V128) V128 { return simdBin(a, b, func(x, y float32) float32 { return x - y }) }
func F32x4Mul(a, b V128) V128 { return simdBin(a, b, func(x, y float32) float32 { return x * y }) }
func F32x4Div(a, b V128) V128 { return simdBin(a, b, func(x, y float32) float32 { return x / y }) }
func F32x4Min(a, b V128) V128 { return simdBin(a, b, simdFmin32) }
func F32x4Max(a, b V128) V128 { return simdBin(a, b, simdFmax32) }
func F32x4Pmin(a, b V128) V128 {
	return simdBin(a, b, func(x, y float32) float32 {
		if y < x {
			return y
		}
		return x
	})
}
func F32x4Pmax(a, b V128) V128 {
	return simdBin(a, b, func(x, y float32) float32 {
		if x < y {
			return y
		}
		return x
	})
}

func F32x4DemoteF64x2Zero(a V128) V128 {
	var r V128
	x, d := simdLanes[float64](&a), simdLanes[float32](&r)
	d[0], d[1] = float32(x[0]), float32(x[1])
	return r
}

func F32x4ConvertI32x4S(a V128) V128 {
	var r V128
	x, d := simdLanes[int32](&a), simdLanes[float32](&r)
	for i := range d {
		d[i] = float32(x[i])
	}
	return r
}

func F32x4ConvertI32x4U(a V128) V128 {
	var r V128
	x, d := simdLanes[uint32](&a), simdLanes[float32](&r)
	for i := range d {
		d[i] = float32(x[i])
	}
	return r
}

// ---------------------------------------------------------------------------
// f64x2

func F64x2Abs(a V128) V128 { return V128{a.Lo &^ simdSign64, a.Hi &^ simdSign64} }
func F64x2Neg(a V128) V128 { return V128{a.Lo ^ simdSign64, a.Hi ^ simdSign64} }

func F64x2Ceil(a V128) V128    { return simdUn(a, F64Ceil) }
func F64x2Floor(a V128) V128   { return simdUn(a, F64Floor) }
func F64x2Trunc(a V128) V128   { return simdUn(a, F64Trunc) }
func F64x2Nearest(a V128) V128 { return simdUn(a, F64Nearest) }
func F64x2Sqrt(a V128) V128    { return simdUn(a, F64Sqrt) }

func F64x2Add(a, b V128) V128 { return simdBin(a, b, func(x, y float64) float64 { return x + y }) }
func F64x2Sub(a, b V128) V128 { return simdBin(a, b, func(x, y float64) float64 { return x - y }) }
func F64x2Mul(a, b V128) V128 { return simdBin(a, b, func(x, y float64) float64 { return x * y }) }
func F64x2Div(a, b V128) V128 { return simdBin(a, b, func(x, y float64) float64 { return x / y }) }
func F64x2Min(a, b V128) V128 { return simdBin(a, b, simdFmin64) }
func F64x2Max(a, b V128) V128 { return simdBin(a, b, simdFmax64) }
func F64x2Pmin(a, b V128) V128 {
	return simdBin(a, b, func(x, y float64) float64 {
		if y < x {
			return y
		}
		return x
	})
}
func F64x2Pmax(a, b V128) V128 {
	return simdBin(a, b, func(x, y float64) float64 {
		if x < y {
			return y
		}
		return x
	})
}

func F64x2PromoteLowF32x4(a V128) V128 {
	return simdWiden(a, 0, func(x float32) float64 { return float64(x) })
}
func F64x2ConvertLowI32x4S(a V128) V128 {
	return simdWiden(a, 0, func(x int32) float64 { return float64(x) })
}
func F64x2ConvertLowI32x4U(a V128) V128 {
	return simdWiden(a, 0, func(x uint32) float64 { return float64(x) })
}

// ---------------------------------------------------------------------------
// Relaxed SIMD (deterministic choices)

func I8x16RelaxedSwizzle(a, s V128) V128       { return I8x16Swizzle(a, s) }
func I32x4RelaxedTruncF32x4S(a V128) V128      { return I32x4TruncSatF32x4S(a) }
func I32x4RelaxedTruncF32x4U(a V128) V128      { return I32x4TruncSatF32x4U(a) }
func I32x4RelaxedTruncF64x2SZero(a V128) V128  { return I32x4TruncSatF64x2SZero(a) }
func I32x4RelaxedTruncF64x2UZero(a V128) V128  { return I32x4TruncSatF64x2UZero(a) }
func I8x16RelaxedLaneselect(a, b, m V128) V128 { return V128Bitselect(a, b, m) }
func I16x8RelaxedLaneselect(a, b, m V128) V128 { return V128Bitselect(a, b, m) }
func I32x4RelaxedLaneselect(a, b, m V128) V128 { return V128Bitselect(a, b, m) }
func I64x2RelaxedLaneselect(a, b, m V128) V128 { return V128Bitselect(a, b, m) }
func F32x4RelaxedMin(a, b V128) V128           { return F32x4Min(a, b) }
func F32x4RelaxedMax(a, b V128) V128           { return F32x4Max(a, b) }
func F64x2RelaxedMin(a, b V128) V128           { return F64x2Min(a, b) }
func F64x2RelaxedMax(a, b V128) V128           { return F64x2Max(a, b) }
func I16x8RelaxedQ15mulrS(a, b V128) V128      { return I16x8Q15mulrSatS(a, b) }

// F32x4RelaxedMadd computes a*b+c with the product rounded (unfused).
func F32x4RelaxedMadd(a, b, c V128) V128 {
	return simdTern(a, b, c, func(x, y, z float32) float32 { return float32(x*y) + z })
}

// F32x4RelaxedNmadd computes -(a*b)+c with the product rounded (unfused).
func F32x4RelaxedNmadd(a, b, c V128) V128 {
	return simdTern(a, b, c, func(x, y, z float32) float32 { return -float32(x*y) + z })
}

func F64x2RelaxedMadd(a, b, c V128) V128 {
	return simdTern(a, b, c, func(x, y, z float64) float64 { return float64(x*y) + z })
}

func F64x2RelaxedNmadd(a, b, c V128) V128 {
	return simdTern(a, b, c, func(x, y, z float64) float64 { return -float64(x*y) + z })
}

// I16x8RelaxedDotI8x16I7x16S treats both inputs as signed i8 and sums
// adjacent products into i16 lanes with signed saturation (only reachable
// when b lanes fall outside the i7 range).
func I16x8RelaxedDotI8x16I7x16S(a, b V128) V128 {
	var r V128
	x, y, d := simdLanes[int8](&a), simdLanes[int8](&b), simdLanes[int16](&r)
	for i := range d {
		d[i] = int16(int32(x[2*i])*int32(y[2*i]) + int32(x[2*i+1])*int32(y[2*i+1]))
	}
	return r
}

// I32x4RelaxedDotI8x16I7x16AddS sums groups of four signed i8 products into
// i32 lanes and adds c (wrapping).
func I32x4RelaxedDotI8x16I7x16AddS(a, b, c V128) V128 {
	var r V128
	x, y := simdLanes[int8](&a), simdLanes[int8](&b)
	z, d := simdLanes[int32](&c), simdLanes[int32](&r)
	for i := range d {
		var s int32
		for j := 4 * i; j < 4*i+4; j++ {
			s += int32(x[j]) * int32(y[j])
		}
		d[i] = s + z[i]
	}
	return r
}
