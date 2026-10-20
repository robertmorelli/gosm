package rt

import (
	"encoding/binary"
	"math"
)

// MaxArrayLen caps array allocation so huge lengths trap instead of
// exhausting memory.
var MaxArrayLen uint64 = 1 << 28

// MakeSlice allocates n elements of T initialized to v.
func MakeSlice[T comparable](n int32, v T) []T {
	if uint64(uint32(n)) > MaxArrayLen {
		panic(ErrArrayTooLarge)
	}
	s := make([]T, uint32(n))
	var z T
	if v != z {
		for i := range s {
			s[i] = v
		}
	}
	return s
}

// MakeSliceV128 is MakeSlice for non-comparable-zero element types.
func MakeSliceAny[T any](n int32, v T) []T {
	if uint64(uint32(n)) > MaxArrayLen {
		panic(ErrArrayTooLarge)
	}
	s := make([]T, uint32(n))
	for i := range s {
		s[i] = v
	}
	return s
}

func checkRange(length int, off, n uint32) {
	if uint64(off)+uint64(n) > uint64(length) {
		panic(ErrArrayOOB)
	}
}

// ArrayFill implements array.fill.
func ArrayFill[T any](s []T, off int32, v T, n int32) {
	checkRange(len(s), uint32(off), uint32(n))
	for i := uint32(0); i < uint32(n); i++ {
		s[uint32(off)+i] = v
	}
}

// ArrayCopy implements array.copy for identical element types.
func ArrayCopy[T any](dst []T, d int32, src []T, s int32, n int32) {
	checkRange(len(dst), uint32(d), uint32(n))
	checkRange(len(src), uint32(s), uint32(n))
	copy(dst[uint32(d):uint32(d)+uint32(n)], src[uint32(s):uint32(s)+uint32(n)])
}

// ArrayCopyConv implements array.copy when the element Go types differ.
func ArrayCopyConv[D, S any](dst []D, d int32, src []S, s int32, n int32, conv func(S) D) {
	checkRange(len(dst), uint32(d), uint32(n))
	checkRange(len(src), uint32(s), uint32(n))
	tmp := make([]D, uint32(n))
	for i := range tmp {
		tmp[i] = conv(src[uint32(s)+uint32(i)])
	}
	copy(dst[uint32(d):], tmp)
}

// SegSlice returns n elements of seg starting at s, trapping if out of bounds.
func SegSlice[T any](seg []T, s, n int32) []T {
	if uint64(uint32(s))+uint64(uint32(n)) > uint64(len(seg)) {
		panic(ErrArrayOOB)
	}
	return seg[uint32(s) : uint32(s)+uint32(n)]
}

// ArrayInit implements array.init_elem for identical element types.
func ArrayInit[T any](dst []T, d int32, seg []T, s, n int32) {
	checkRange(len(dst), uint32(d), uint32(n))
	if uint64(uint32(s))+uint64(uint32(n)) > uint64(len(seg)) {
		panic(ErrArrayOOB)
	}
	copy(dst[uint32(d):], seg[uint32(s):uint32(s)+uint32(n)])
}

// DataBytes returns the bytes for n elements of the given size taken from a
// data segment at offset s, trapping if out of bounds.
func DataBytes(seg []byte, s int32, n int32, size int) []byte {
	total := uint64(uint32(n)) * uint64(size)
	if uint64(uint32(s))+total > uint64(len(seg)) {
		panic(ErrMemOOB)
	}
	return seg[uint32(s) : uint64(uint32(s))+total]
}

// Element decoders for array.new_data / array.init_data.

func DecI8(b []byte) []int8 {
	out := make([]int8, len(b))
	for i, c := range b {
		out[i] = int8(c)
	}
	return out
}

func DecI16(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
	}
	return out
}

func DecI32(b []byte) []int32 {
	out := make([]int32, len(b)/4)
	for i := range out {
		out[i] = int32(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

func DecI64(b []byte) []int64 {
	out := make([]int64, len(b)/8)
	for i := range out {
		out[i] = int64(binary.LittleEndian.Uint64(b[8*i:]))
	}
	return out
}

func DecF32(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

func DecF64(b []byte) []float64 {
	out := make([]float64, len(b)/8)
	for i := range out {
		out[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[8*i:]))
	}
	return out
}

func DecV128(b []byte) []V128 {
	out := make([]V128, len(b)/16)
	for i := range out {
		out[i] = V128{binary.LittleEndian.Uint64(b[16*i:]), binary.LittleEndian.Uint64(b[16*i+8:])}
	}
	return out
}

// CheckArrayLen traps if n exceeds the allocation limit.
func CheckArrayLen(n int32) uint32 {
	if uint64(uint32(n)) > MaxArrayLen {
		panic(ErrArrayTooLarge)
	}
	return uint32(n)
}

// Lener is implemented by every generated array type.
type Lener interface {
	WasmLen() int
}

// ArrayLen implements array.len on an abstract arrayref.
func ArrayLen(x any) int32 {
	if x == nil {
		panic(ErrNullArray)
	}
	return int32(x.(Lener).WasmLen())
}

// ConvSlice converts a slice element-wise.
func ConvSlice[S, D any](src []S, conv func(S) D) []D {
	out := make([]D, len(src))
	for i, v := range src {
		out[i] = conv(v)
	}
	return out
}
