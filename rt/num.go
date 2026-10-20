package rt

import "math"

func I32DivS(a, b int32) int32 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	if a == math.MinInt32 && b == -1 {
		panic(ErrIntOverflow)
	}
	return a / b
}

func I32DivU(a, b int32) int32 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	return int32(uint32(a) / uint32(b))
}

func I32RemS(a, b int32) int32 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	if b == -1 {
		return 0
	}
	return a % b
}

func I32RemU(a, b int32) int32 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	return int32(uint32(a) % uint32(b))
}

func I64DivS(a, b int64) int64 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	if a == math.MinInt64 && b == -1 {
		panic(ErrIntOverflow)
	}
	return a / b
}

func I64DivU(a, b int64) int64 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	return int64(uint64(a) / uint64(b))
}

func I64RemS(a, b int64) int64 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	if b == -1 {
		return 0
	}
	return a % b
}

func I64RemU(a, b int64) int64 {
	if b == 0 {
		panic(ErrDivByZero)
	}
	return int64(uint64(a) % uint64(b))
}

// Float helpers operating on bit patterns so NaN payloads are preserved
// where the spec requires it.

func F32Abs(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) &^ (1 << 31))
}

func F32Neg(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) ^ (1 << 31))
}

func F32Copysign(x, y float32) float32 {
	return math.Float32frombits(math.Float32bits(x)&^(1<<31) | math.Float32bits(y)&(1<<31))
}

func F64Abs(x float64) float64 {
	return math.Float64frombits(math.Float64bits(x) &^ (1 << 63))
}

func F64Neg(x float64) float64 {
	return math.Float64frombits(math.Float64bits(x) ^ (1 << 63))
}

func F64Copysign(x, y float64) float64 {
	return math.Float64frombits(math.Float64bits(x)&^(1<<63) | math.Float64bits(y)&(1<<63))
}

const (
	canonNaN32 = 0x7FC00000
	canonNaN64 = 0x7FF8000000000000
)

func quietNaN32(x float32) float32 {
	return math.Float32frombits(math.Float32bits(x) | 0x00400000)
}

func quietNaN64(x float64) float64 {
	return math.Float64frombits(math.Float64bits(x) | 0x0008000000000000)
}

func F32Min(a, b float32) float32 {
	switch {
	case a != a:
		return quietNaN32(a)
	case b != b:
		return quietNaN32(b)
	case a == b:
		// handles -0 / +0
		return math.Float32frombits(math.Float32bits(a) | math.Float32bits(b))
	case a < b:
		return a
	}
	return b
}

func F32Max(a, b float32) float32 {
	switch {
	case a != a:
		return quietNaN32(a)
	case b != b:
		return quietNaN32(b)
	case a == b:
		return math.Float32frombits(math.Float32bits(a) & math.Float32bits(b))
	case a > b:
		return a
	}
	return b
}

func F64Min(a, b float64) float64 {
	switch {
	case a != a:
		return quietNaN64(a)
	case b != b:
		return quietNaN64(b)
	case a == b:
		return math.Float64frombits(math.Float64bits(a) | math.Float64bits(b))
	case a < b:
		return a
	}
	return b
}

func F64Max(a, b float64) float64 {
	switch {
	case a != a:
		return quietNaN64(a)
	case b != b:
		return quietNaN64(b)
	case a == b:
		return math.Float64frombits(math.Float64bits(a) & math.Float64bits(b))
	case a > b:
		return a
	}
	return b
}

func F32Ceil(x float32) float32 {
	if x != x {
		return quietNaN32(x)
	}
	return float32(math.Ceil(float64(x)))
}

func F32Floor(x float32) float32 {
	if x != x {
		return quietNaN32(x)
	}
	return float32(math.Floor(float64(x)))
}

func F32Trunc(x float32) float32 {
	if x != x {
		return quietNaN32(x)
	}
	return float32(math.Trunc(float64(x)))
}

func F32Nearest(x float32) float32 {
	if x != x {
		return quietNaN32(x)
	}
	return float32(math.RoundToEven(float64(x)))
}

func F32Sqrt(x float32) float32 {
	if x != x {
		return quietNaN32(x)
	}
	return float32(math.Sqrt(float64(x)))
}

func F64Ceil(x float64) float64 {
	if x != x {
		return quietNaN64(x)
	}
	return math.Ceil(x)
}

func F64Floor(x float64) float64 {
	if x != x {
		return quietNaN64(x)
	}
	return math.Floor(x)
}

func F64Trunc(x float64) float64 {
	if x != x {
		return quietNaN64(x)
	}
	return math.Trunc(x)
}

func F64Nearest(x float64) float64 {
	if x != x {
		return quietNaN64(x)
	}
	return math.RoundToEven(x)
}

func F64Sqrt(x float64) float64 {
	if x != x {
		return quietNaN64(x)
	}
	return math.Sqrt(x)
}

// Trapping truncations.

func I32TruncF32S(x float32) int32 { return I32TruncF64S(float64(x)) }
func I32TruncF32U(x float32) int32 { return I32TruncF64U(float64(x)) }
func I64TruncF32S(x float32) int64 { return I64TruncF64S(float64(x)) }
func I64TruncF32U(x float32) int64 { return I64TruncF64U(float64(x)) }

func I32TruncF64S(x float64) int32 {
	if x != x {
		panic(ErrInvalidConv)
	}
	if !(x > -2147483649.0 && x < 2147483648.0) {
		panic(ErrIntOverflow)
	}
	return int32(x)
}

func I32TruncF64U(x float64) int32 {
	if x != x {
		panic(ErrInvalidConv)
	}
	if !(x > -1.0 && x < 4294967296.0) {
		panic(ErrIntOverflow)
	}
	return int32(uint32(x))
}

func I64TruncF64S(x float64) int64 {
	if x != x {
		panic(ErrInvalidConv)
	}
	if !(x >= -9223372036854775808.0 && x < 9223372036854775808.0) {
		panic(ErrIntOverflow)
	}
	return int64(x)
}

func I64TruncF64U(x float64) int64 {
	if x != x {
		panic(ErrInvalidConv)
	}
	if !(x > -1.0 && x < 18446744073709551616.0) {
		panic(ErrIntOverflow)
	}
	return int64(uint64(x))
}

// Saturating truncations.

func I32TruncSatF32S(x float32) int32 { return I32TruncSatF64S(float64(x)) }
func I32TruncSatF32U(x float32) int32 { return I32TruncSatF64U(float64(x)) }
func I64TruncSatF32S(x float32) int64 { return I64TruncSatF64S(float64(x)) }
func I64TruncSatF32U(x float32) int64 { return I64TruncSatF64U(float64(x)) }

func I32TruncSatF64S(x float64) int32 {
	switch {
	case x != x:
		return 0
	case x <= -2147483648.0:
		return math.MinInt32
	case x >= 2147483647.0:
		return math.MaxInt32
	}
	return int32(x)
}

func I32TruncSatF64U(x float64) int32 {
	switch {
	case x != x, x <= 0:
		return 0
	case x >= 4294967295.0:
		return -1
	}
	return int32(uint32(x))
}

func I64TruncSatF64S(x float64) int64 {
	switch {
	case x != x:
		return 0
	case x <= -9223372036854775808.0:
		return math.MinInt64
	case x >= 9223372036854775808.0:
		return math.MaxInt64
	}
	return int64(x)
}

func I64TruncSatF64U(x float64) int64 {
	switch {
	case x != x, x <= 0:
		return 0
	case x >= 18446744073709551616.0:
		return -1
	}
	return int64(uint64(x))
}

// F32ConvertU64 converts an unsigned 64-bit integer to float32 with a single
// rounding.
func F32ConvertU64(x int64) float32 { return float32(uint64(x)) }
func F64ConvertU64(x int64) float64 { return float64(uint64(x)) }

// U32 zero-extends an i32 address or index to uint64.
func U32(x int32) uint64 { return uint64(uint32(x)) }

// U64 reinterprets an i64 address or index as uint64.
func U64(x int64) uint64 { return uint64(x) }
