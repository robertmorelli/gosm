// Package wasm decodes WebAssembly 3.0 binary modules (including GC,
// exceptions, tail calls, memory64, multi-memory, SIMD and relaxed SIMD).
package wasm

import "fmt"

// Value type kinds.
const (
	I32  byte = 0x7F
	I64  byte = 0x7E
	F32  byte = 0x7D
	F64  byte = 0x7C
	V128 byte = 0x7B
	Ref  byte = 0x64 // generic reference; see ValType.Heap / Nullable

	// Packed storage types (only valid as field types).
	I8  byte = 0x78
	I16 byte = 0x77
)

// HeapType is either a non-negative type index or one of the abstract
// heap type constants below (negative).
type HeapType int32

const (
	HeapNoExn    HeapType = -0x0C // 0x74
	HeapNoFunc   HeapType = -0x0D // 0x73
	HeapNoExtern HeapType = -0x0E // 0x72
	HeapNone     HeapType = -0x0F // 0x71
	HeapFunc     HeapType = -0x10 // 0x70
	HeapExtern   HeapType = -0x11 // 0x6F
	HeapAny      HeapType = -0x12 // 0x6E
	HeapEq       HeapType = -0x13 // 0x6D
	HeapI31      HeapType = -0x14 // 0x6C
	HeapStruct   HeapType = -0x15 // 0x6B
	HeapArray    HeapType = -0x16 // 0x6A
	HeapExn      HeapType = -0x17 // 0x69
)

func (h HeapType) IsAbstract() bool { return h < 0 }

func (h HeapType) String() string {
	switch h {
	case HeapNoExn:
		return "noexn"
	case HeapNoFunc:
		return "nofunc"
	case HeapNoExtern:
		return "noextern"
	case HeapNone:
		return "none"
	case HeapFunc:
		return "func"
	case HeapExtern:
		return "extern"
	case HeapAny:
		return "any"
	case HeapEq:
		return "eq"
	case HeapI31:
		return "i31"
	case HeapStruct:
		return "struct"
	case HeapArray:
		return "array"
	case HeapExn:
		return "exn"
	}
	return fmt.Sprintf("$%d", int32(h))
}

// ValType is a value type. For Kind==Ref, Heap and Nullable are meaningful.
type ValType struct {
	Kind     byte
	Nullable bool
	Heap     HeapType
}

var (
	TI32  = ValType{Kind: I32}
	TI64  = ValType{Kind: I64}
	TF32  = ValType{Kind: F32}
	TF64  = ValType{Kind: F64}
	TV128 = ValType{Kind: V128}
)

func RefType(h HeapType, nullable bool) ValType {
	return ValType{Kind: Ref, Heap: h, Nullable: nullable}
}

func (v ValType) IsRef() bool { return v.Kind == Ref }

func (v ValType) String() string {
	switch v.Kind {
	case I32:
		return "i32"
	case I64:
		return "i64"
	case F32:
		return "f32"
	case F64:
		return "f64"
	case V128:
		return "v128"
	case I8:
		return "i8"
	case I16:
		return "i16"
	case Ref:
		if v.Nullable {
			return "(ref null " + v.Heap.String() + ")"
		}
		return "(ref " + v.Heap.String() + ")"
	}
	return fmt.Sprintf("?%x", v.Kind)
}

// Composite type kinds.
const (
	KindFunc   byte = 0x60
	KindStruct byte = 0x5F
	KindArray  byte = 0x5E
)

// FieldType is a struct field or array element type.
type FieldType struct {
	Type    ValType // Kind may be I8/I16 for packed storage
	Mutable bool
}

func (f FieldType) Packed() bool { return f.Type.Kind == I8 || f.Type.Kind == I16 }

// Unpacked returns the value type obtained when reading the field.
func (f FieldType) Unpacked() ValType {
	if f.Packed() {
		return TI32
	}
	return f.Type
}

type FuncType struct {
	Params  []ValType
	Results []ValType
}

// CompType is a composite type (func, struct or array).
type CompType struct {
	Kind   byte
	Func   FuncType
	Fields []FieldType // struct fields; for arrays Fields[0] is the element
}

// SubType is a type definition with optional declared supertype.
type SubType struct {
	Final  bool
	Supers []uint32
	Comp   CompType
	Group  int // index of the rec group this type belongs to
}

// RecGroup is a range of type indices [Start, Start+Len).
type RecGroup struct {
	Start, Len int
}

type Limits struct {
	Min    uint64
	Max    uint64
	HasMax bool
	Is64   bool
	Shared bool
}

type TableType struct {
	Elem   ValType
	Limits Limits
}

type MemType struct {
	Limits Limits
}

type GlobalType struct {
	Type    ValType
	Mutable bool
}

// External kinds.
const (
	ExtFunc   byte = 0
	ExtTable  byte = 1
	ExtMem    byte = 2
	ExtGlobal byte = 3
	ExtTag    byte = 4
)

type Import struct {
	Module, Name string
	Kind         byte
	TypeIdx      uint32 // func / tag
	Table        TableType
	Mem          MemType
	Global       GlobalType
}

type Export struct {
	Name  string
	Kind  byte
	Index uint32
}

type Table struct {
	Type TableType
	Init []Instr // optional init expression (nil = null)
}

type Global struct {
	Type GlobalType
	Init []Instr
}

// Element segment modes.
const (
	SegActive      = 0
	SegPassive     = 1
	SegDeclarative = 2
)

type Elem struct {
	Mode   int
	Table  uint32
	Offset []Instr
	Type   ValType
	Inits  [][]Instr // each an expression yielding a reference
}

type Data struct {
	Mode   int
	Mem    uint32
	Offset []Instr
	Bytes  []byte
}

type Code struct {
	Locals []ValType // expanded, not including params
	Body   []Instr
	Offset int // byte offset of the body in the module, for diagnostics
}

// Names holds the decoded "name" custom section (subsections we use).
type Names struct {
	Module  string
	Funcs   map[uint32]string
	Locals  map[uint32]map[uint32]string
	Types   map[uint32]string
	Fields  map[uint32]map[uint32]string
	Tables  map[uint32]string
	Mems    map[uint32]string
	Globals map[uint32]string
	Tags    map[uint32]string
}

type Module struct {
	Types     []SubType
	RecGroups []RecGroup
	Imports   []Import
	Funcs     []uint32 // type indices of defined functions
	Tables    []Table
	Mems      []MemType
	Globals   []Global
	Tags      []uint32 // type indices of defined tags
	Exports   []Export
	Start     *uint32
	Elems     []Elem
	Datas     []Data
	Codes     []Code
	DataCount *uint32
	Names     Names

	// Derived index spaces (imports first).
	NumImportedFuncs   int
	NumImportedTables  int
	NumImportedMems    int
	NumImportedGlobals int
	NumImportedTags    int
}

// FuncTypeIdx returns the type index of function index f (imports included).
func (m *Module) FuncTypeIdx(f uint32) uint32 {
	if int(f) < m.NumImportedFuncs {
		n := 0
		for _, im := range m.Imports {
			if im.Kind == ExtFunc {
				if n == int(f) {
					return im.TypeIdx
				}
				n++
			}
		}
	}
	return m.Funcs[int(f)-m.NumImportedFuncs]
}

func (m *Module) FuncType(f uint32) *FuncType {
	return &m.Types[m.FuncTypeIdx(f)].Comp.Func
}

func (m *Module) TagTypeIdx(t uint32) uint32 {
	if int(t) < m.NumImportedTags {
		n := 0
		for _, im := range m.Imports {
			if im.Kind == ExtTag {
				if n == int(t) {
					return im.TypeIdx
				}
				n++
			}
		}
	}
	return m.Tags[int(t)-m.NumImportedTags]
}

func (m *Module) TableType(t uint32) TableType {
	if int(t) < m.NumImportedTables {
		n := 0
		for _, im := range m.Imports {
			if im.Kind == ExtTable {
				if n == int(t) {
					return im.Table
				}
				n++
			}
		}
	}
	return m.Tables[int(t)-m.NumImportedTables].Type
}

func (m *Module) MemType(i uint32) MemType {
	if int(i) < m.NumImportedMems {
		n := 0
		for _, im := range m.Imports {
			if im.Kind == ExtMem {
				if n == int(i) {
					return im.Mem
				}
				n++
			}
		}
	}
	return m.Mems[int(i)-m.NumImportedMems]
}

func (m *Module) GlobalType(i uint32) GlobalType {
	if int(i) < m.NumImportedGlobals {
		n := 0
		for _, im := range m.Imports {
			if im.Kind == ExtGlobal {
				if n == int(i) {
					return im.Global
				}
				n++
			}
		}
	}
	return m.Globals[int(i)-m.NumImportedGlobals].Type
}

func (m *Module) NumFuncs() int   { return m.NumImportedFuncs + len(m.Funcs) }
func (m *Module) NumTables() int  { return m.NumImportedTables + len(m.Tables) }
func (m *Module) NumMems() int    { return m.NumImportedMems + len(m.Mems) }
func (m *Module) NumGlobals() int { return m.NumImportedGlobals + len(m.Globals) }
func (m *Module) NumTags() int    { return m.NumImportedTags + len(m.Tags) }
