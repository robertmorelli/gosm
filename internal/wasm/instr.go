package wasm

// Op identifies an instruction. Single-byte opcodes are their byte value;
// prefixed opcodes are prefix<<16 | subopcode.
type Op uint32

const (
	PrefixGC   = 0xFB
	PrefixMisc = 0xFC
	PrefixSIMD = 0xFD
)

func gc(n uint32) Op   { return Op(PrefixGC<<16 | n) }
func misc(n uint32) Op { return Op(PrefixMisc<<16 | n) }

// SIMDOp returns the Op for SIMD subopcode n.
func SIMDOp(n uint32) Op { return Op(PrefixSIMD<<16 | n) }

func (o Op) Prefix() uint32 { return uint32(o) >> 16 }
func (o Op) Sub() uint32    { return uint32(o) & 0xFFFF }

const (
	OpUnreachable        Op = 0x00
	OpNop                Op = 0x01
	OpBlock              Op = 0x02
	OpLoop               Op = 0x03
	OpIf                 Op = 0x04
	OpElse               Op = 0x05
	OpTry                Op = 0x06 // legacy
	OpCatch              Op = 0x07 // legacy
	OpThrow              Op = 0x08
	OpRethrow            Op = 0x09 // legacy
	OpThrowRef           Op = 0x0A
	OpEnd                Op = 0x0B
	OpBr                 Op = 0x0C
	OpBrIf               Op = 0x0D
	OpBrTable            Op = 0x0E
	OpReturn             Op = 0x0F
	OpCall               Op = 0x10
	OpCallIndirect       Op = 0x11
	OpReturnCall         Op = 0x12
	OpReturnCallIndirect Op = 0x13
	OpCallRef            Op = 0x14
	OpReturnCallRef      Op = 0x15
	OpDelegate           Op = 0x18 // legacy
	OpCatchAll           Op = 0x19 // legacy
	OpDrop               Op = 0x1A
	OpSelect             Op = 0x1B
	OpSelectT            Op = 0x1C
	OpTryTable           Op = 0x1F
	OpLocalGet           Op = 0x20
	OpLocalSet           Op = 0x21
	OpLocalTee           Op = 0x22
	OpGlobalGet          Op = 0x23
	OpGlobalSet          Op = 0x24
	OpTableGet           Op = 0x25
	OpTableSet           Op = 0x26

	OpI32Load    Op = 0x28
	OpI64Load    Op = 0x29
	OpF32Load    Op = 0x2A
	OpF64Load    Op = 0x2B
	OpI32Load8S  Op = 0x2C
	OpI32Load8U  Op = 0x2D
	OpI32Load16S Op = 0x2E
	OpI32Load16U Op = 0x2F
	OpI64Load8S  Op = 0x30
	OpI64Load8U  Op = 0x31
	OpI64Load16S Op = 0x32
	OpI64Load16U Op = 0x33
	OpI64Load32S Op = 0x34
	OpI64Load32U Op = 0x35
	OpI32Store   Op = 0x36
	OpI64Store   Op = 0x37
	OpF32Store   Op = 0x38
	OpF64Store   Op = 0x39
	OpI32Store8  Op = 0x3A
	OpI32Store16 Op = 0x3B
	OpI64Store8  Op = 0x3C
	OpI64Store16 Op = 0x3D
	OpI64Store32 Op = 0x3E
	OpMemorySize Op = 0x3F
	OpMemoryGrow Op = 0x40

	OpI32Const Op = 0x41
	OpI64Const Op = 0x42
	OpF32Const Op = 0x43
	OpF64Const Op = 0x44

	OpI32Eqz Op = 0x45
	OpI32Eq  Op = 0x46
	OpI32Ne  Op = 0x47
	OpI32LtS Op = 0x48
	OpI32LtU Op = 0x49
	OpI32GtS Op = 0x4A
	OpI32GtU Op = 0x4B
	OpI32LeS Op = 0x4C
	OpI32LeU Op = 0x4D
	OpI32GeS Op = 0x4E
	OpI32GeU Op = 0x4F
	OpI64Eqz Op = 0x50
	OpI64Eq  Op = 0x51
	OpI64Ne  Op = 0x52
	OpI64LtS Op = 0x53
	OpI64LtU Op = 0x54
	OpI64GtS Op = 0x55
	OpI64GtU Op = 0x56
	OpI64LeS Op = 0x57
	OpI64LeU Op = 0x58
	OpI64GeS Op = 0x59
	OpI64GeU Op = 0x5A
	OpF32Eq  Op = 0x5B
	OpF32Ne  Op = 0x5C
	OpF32Lt  Op = 0x5D
	OpF32Gt  Op = 0x5E
	OpF32Le  Op = 0x5F
	OpF32Ge  Op = 0x60
	OpF64Eq  Op = 0x61
	OpF64Ne  Op = 0x62
	OpF64Lt  Op = 0x63
	OpF64Gt  Op = 0x64
	OpF64Le  Op = 0x65
	OpF64Ge  Op = 0x66

	OpI32Clz    Op = 0x67
	OpI32Ctz    Op = 0x68
	OpI32Popcnt Op = 0x69
	OpI32Add    Op = 0x6A
	OpI32Sub    Op = 0x6B
	OpI32Mul    Op = 0x6C
	OpI32DivS   Op = 0x6D
	OpI32DivU   Op = 0x6E
	OpI32RemS   Op = 0x6F
	OpI32RemU   Op = 0x70
	OpI32And    Op = 0x71
	OpI32Or     Op = 0x72
	OpI32Xor    Op = 0x73
	OpI32Shl    Op = 0x74
	OpI32ShrS   Op = 0x75
	OpI32ShrU   Op = 0x76
	OpI32Rotl   Op = 0x77
	OpI32Rotr   Op = 0x78
	OpI64Clz    Op = 0x79
	OpI64Ctz    Op = 0x7A
	OpI64Popcnt Op = 0x7B
	OpI64Add    Op = 0x7C
	OpI64Sub    Op = 0x7D
	OpI64Mul    Op = 0x7E
	OpI64DivS   Op = 0x7F
	OpI64DivU   Op = 0x80
	OpI64RemS   Op = 0x81
	OpI64RemU   Op = 0x82
	OpI64And    Op = 0x83
	OpI64Or     Op = 0x84
	OpI64Xor    Op = 0x85
	OpI64Shl    Op = 0x86
	OpI64ShrS   Op = 0x87
	OpI64ShrU   Op = 0x88
	OpI64Rotl   Op = 0x89
	OpI64Rotr   Op = 0x8A

	OpF32Abs      Op = 0x8B
	OpF32Neg      Op = 0x8C
	OpF32Ceil     Op = 0x8D
	OpF32Floor    Op = 0x8E
	OpF32Trunc    Op = 0x8F
	OpF32Nearest  Op = 0x90
	OpF32Sqrt     Op = 0x91
	OpF32Add      Op = 0x92
	OpF32Sub      Op = 0x93
	OpF32Mul      Op = 0x94
	OpF32Div      Op = 0x95
	OpF32Min      Op = 0x96
	OpF32Max      Op = 0x97
	OpF32Copysign Op = 0x98
	OpF64Abs      Op = 0x99
	OpF64Neg      Op = 0x9A
	OpF64Ceil     Op = 0x9B
	OpF64Floor    Op = 0x9C
	OpF64Trunc    Op = 0x9D
	OpF64Nearest  Op = 0x9E
	OpF64Sqrt     Op = 0x9F
	OpF64Add      Op = 0xA0
	OpF64Sub      Op = 0xA1
	OpF64Mul      Op = 0xA2
	OpF64Div      Op = 0xA3
	OpF64Min      Op = 0xA4
	OpF64Max      Op = 0xA5
	OpF64Copysign Op = 0xA6

	OpI32WrapI64        Op = 0xA7
	OpI32TruncF32S      Op = 0xA8
	OpI32TruncF32U      Op = 0xA9
	OpI32TruncF64S      Op = 0xAA
	OpI32TruncF64U      Op = 0xAB
	OpI64ExtendI32S     Op = 0xAC
	OpI64ExtendI32U     Op = 0xAD
	OpI64TruncF32S      Op = 0xAE
	OpI64TruncF32U      Op = 0xAF
	OpI64TruncF64S      Op = 0xB0
	OpI64TruncF64U      Op = 0xB1
	OpF32ConvertI32S    Op = 0xB2
	OpF32ConvertI32U    Op = 0xB3
	OpF32ConvertI64S    Op = 0xB4
	OpF32ConvertI64U    Op = 0xB5
	OpF32DemoteF64      Op = 0xB6
	OpF64ConvertI32S    Op = 0xB7
	OpF64ConvertI32U    Op = 0xB8
	OpF64ConvertI64S    Op = 0xB9
	OpF64ConvertI64U    Op = 0xBA
	OpF64PromoteF32     Op = 0xBB
	OpI32ReinterpretF32 Op = 0xBC
	OpI64ReinterpretF64 Op = 0xBD
	OpF32ReinterpretI32 Op = 0xBE
	OpF64ReinterpretI64 Op = 0xBF
	OpI32Extend8S       Op = 0xC0
	OpI32Extend16S      Op = 0xC1
	OpI64Extend8S       Op = 0xC2
	OpI64Extend16S      Op = 0xC3
	OpI64Extend32S      Op = 0xC4

	OpRefNull      Op = 0xD0
	OpRefIsNull    Op = 0xD1
	OpRefFunc      Op = 0xD2
	OpRefEq        Op = 0xD3
	OpRefAsNonNull Op = 0xD4
	OpBrOnNull     Op = 0xD5
	OpBrOnNonNull  Op = 0xD6
)

var (
	OpStructNew        = gc(0)
	OpStructNewDefault = gc(1)
	OpStructGet        = gc(2)
	OpStructGetS       = gc(3)
	OpStructGetU       = gc(4)
	OpStructSet        = gc(5)
	OpArrayNew         = gc(6)
	OpArrayNewDefault  = gc(7)
	OpArrayNewFixed    = gc(8)
	OpArrayNewData     = gc(9)
	OpArrayNewElem     = gc(10)
	OpArrayGet         = gc(11)
	OpArrayGetS        = gc(12)
	OpArrayGetU        = gc(13)
	OpArraySet         = gc(14)
	OpArrayLen         = gc(15)
	OpArrayFill        = gc(16)
	OpArrayCopy        = gc(17)
	OpArrayInitData    = gc(18)
	OpArrayInitElem    = gc(19)
	OpRefTest          = gc(20)
	OpRefTestNull      = gc(21)
	OpRefCast          = gc(22)
	OpRefCastNull      = gc(23)
	OpBrOnCast         = gc(24)
	OpBrOnCastFail     = gc(25)
	OpAnyConvertExtern = gc(26)
	OpExternConvertAny = gc(27)
	OpRefI31           = gc(28)
	OpI31GetS          = gc(29)
	OpI31GetU          = gc(30)

	OpI32TruncSatF32S = misc(0)
	OpI32TruncSatF32U = misc(1)
	OpI32TruncSatF64S = misc(2)
	OpI32TruncSatF64U = misc(3)
	OpI64TruncSatF32S = misc(4)
	OpI64TruncSatF32U = misc(5)
	OpI64TruncSatF64S = misc(6)
	OpI64TruncSatF64U = misc(7)
	OpMemoryInit      = misc(8)
	OpDataDrop        = misc(9)
	OpMemoryCopy      = misc(10)
	OpMemoryFill      = misc(11)
	OpTableInit       = misc(12)
	OpElemDrop        = misc(13)
	OpTableCopy       = misc(14)
	OpTableGrow       = misc(15)
	OpTableSize       = misc(16)
	OpTableFill       = misc(17)
)

type BlockType struct {
	Kind byte // 0 = empty, 1 = single value, 2 = type index
	Val  ValType
	Idx  uint32
}

// Catch clause kinds for try_table.
const (
	CatchPlain  = 0
	CatchRef    = 1
	CatchAll    = 2
	CatchAllRef = 3
)

type Catch struct {
	Kind  byte
	Tag   uint32
	Label uint32
}

// Instr is a decoded instruction. Immediates are stored in the fields
// relevant to the opcode.
type Instr struct {
	Op     Op
	A, B   uint32 // generic indices (label, func, type, local, global, table, mem, field, data, elem, lane count...)
	I64    int64  // i32/i64 constants; f32/f64 constants as raw bits
	Offset uint64 // memarg offset
	Align  uint32
	Lane   byte
	Block  BlockType
	HT     HeapType // ref.null / ref.test / ref.cast / br_on_cast source
	HT2    HeapType // br_on_cast target
	Null1  bool     // br_on_cast source nullability
	Null2  bool     // br_on_cast target nullability / ref.test null
	Labels []uint32 // br_table targets; last is default
	Types  []ValType
	Catch  []Catch
	V128   *[16]byte // v128.const value / shuffle lanes
}

// MemArg-taking SIMD subopcodes.
func simdHasMemArg(sub uint32) bool {
	return sub <= 11 || (sub >= 84 && sub <= 93)
}

func simdHasLane(sub uint32) bool {
	return (sub >= 21 && sub <= 34) || (sub >= 84 && sub <= 91)
}
