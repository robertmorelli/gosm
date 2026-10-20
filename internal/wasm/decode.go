package wasm

import (
	"bytes"
	"fmt"
)

// Decode parses a binary WebAssembly module.
func Decode(b []byte) (m *Module, err error) {
	defer func() {
		if r := recover(); r != nil {
			if de, ok := r.(decodeError); ok {
				err = fmt.Errorf("wasm: %s", de.msg)
				m = nil
				return
			}
			panic(r)
		}
	}()
	r := &reader{b: b}
	if len(b) < 8 || !bytes.Equal(b[:4], []byte("\x00asm")) {
		return nil, fmt.Errorf("wasm: magic header not detected")
	}
	if !bytes.Equal(b[4:8], []byte{1, 0, 0, 0}) {
		return nil, fmt.Errorf("wasm: unknown binary version")
	}
	r.pos = 8
	m = &Module{}
	var funcDecls []uint32
	lastRank := 0
	for !r.eof() {
		id := r.byte()
		size := r.u32()
		body := r.bytes(int(size))
		if id != 0 {
			rank, ok := sectionRank[id]
			if !ok {
				r.fail("malformed section id")
			}
			if rank <= lastRank {
				r.fail("unexpected content after last section")
			}
			lastRank = rank
		}
		sr := &reader{b: body}
		base := r.pos - int(size)
		switch id {
		case 0:
			name := sr.name()
			if name == "name" {
				func() {
					defer func() { recover() }()
					decodeNames(&reader{b: sr.b[sr.pos:]}, &m.Names)
				}()
			}
			continue
		case 1:
			decodeTypes(sr, m)
		case 2:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				im := Import{Module: sr.name(), Name: sr.name(), Kind: sr.byte()}
				switch im.Kind {
				case ExtFunc:
					im.TypeIdx = sr.u32()
					m.NumImportedFuncs++
				case ExtTable:
					im.Table = readTableType(sr)
					m.NumImportedTables++
				case ExtMem:
					im.Mem = MemType{readLimits(sr)}
					m.NumImportedMems++
				case ExtGlobal:
					im.Global = readGlobalType(sr)
					m.NumImportedGlobals++
				case ExtTag:
					if sr.byte() != 0 {
						sr.fail("malformed tag attribute")
					}
					im.TypeIdx = sr.u32()
					m.NumImportedTags++
				default:
					sr.fail("malformed import kind")
				}
				m.Imports = append(m.Imports, im)
			}
		case 3:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				funcDecls = append(funcDecls, sr.u32())
			}
			m.Funcs = funcDecls
		case 4:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				var t Table
				if sr.peek() == 0x40 {
					sr.byte()
					if sr.byte() != 0 {
						sr.fail("malformed table")
					}
					t.Type = readTableType(sr)
					t.Init = readConstExpr(sr)
				} else {
					t.Type = readTableType(sr)
				}
				m.Tables = append(m.Tables, t)
			}
		case 5:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				m.Mems = append(m.Mems, MemType{readLimits(sr)})
			}
		case 6:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				gt := readGlobalType(sr)
				m.Globals = append(m.Globals, Global{Type: gt, Init: readConstExpr(sr)})
			}
		case 7:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				e := Export{Name: sr.name(), Kind: sr.byte()}
				e.Index = sr.u32()
				m.Exports = append(m.Exports, e)
			}
		case 8:
			s := sr.u32()
			m.Start = &s
		case 9:
			decodeElems(sr, m)
		case 10:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				size := sr.u32()
				start := sr.pos
				cr := &reader{b: sr.bytes(int(size))}
				var c Code
				c.Offset = base + start
				nl := cr.u32()
				var total uint64
				for j := uint32(0); j < nl; j++ {
					cnt := cr.u32()
					total += uint64(cnt)
					if total > 50000 {
						cr.fail("too many locals")
					}
					t := readValType(cr)
					for k := uint32(0); k < cnt; k++ {
						c.Locals = append(c.Locals, t)
					}
				}
				c.Body = readBody(cr)
				if !cr.eof() {
					cr.fail("section size mismatch")
				}
				m.Codes = append(m.Codes, c)
			}
		case 11:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				var d Data
				flag := sr.u32()
				switch flag {
				case 0:
					d.Offset = readConstExpr(sr)
				case 1:
					d.Mode = SegPassive
				case 2:
					d.Mem = sr.u32()
					d.Offset = readConstExpr(sr)
				default:
					sr.fail("malformed data segment kind")
				}
				ln := sr.u32()
				d.Bytes = sr.bytes(int(ln))
				m.Datas = append(m.Datas, d)
			}
		case 12:
			c := sr.u32()
			m.DataCount = &c
		case 13:
			n := sr.u32()
			for i := uint32(0); i < n; i++ {
				if sr.byte() != 0 {
					sr.fail("malformed tag attribute")
				}
				m.Tags = append(m.Tags, sr.u32())
			}
		default:
			r.fail("malformed section id")
		}
		if !sr.eof() {
			sr.fail("section size mismatch")
		}
	}
	if len(m.Funcs) != len(m.Codes) {
		return nil, fmt.Errorf("wasm: function and code section have inconsistent lengths")
	}
	return m, nil
}

// sectionRank gives the required relative order of non-custom sections.
var sectionRank = map[byte]int{
	1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 13: 6, 6: 7, 7: 8, 8: 9, 9: 10, 12: 11, 10: 12, 11: 13,
}

func decodeTypes(r *reader, m *Module) {
	n := r.u32()
	for i := uint32(0); i < n; i++ {
		g := len(m.RecGroups)
		if r.peek() == 0x4E {
			r.byte()
			cnt := r.u32()
			m.RecGroups = append(m.RecGroups, RecGroup{Start: len(m.Types), Len: int(cnt)})
			for j := uint32(0); j < cnt; j++ {
				st := readSubType(r)
				st.Group = g
				m.Types = append(m.Types, st)
			}
		} else {
			m.RecGroups = append(m.RecGroups, RecGroup{Start: len(m.Types), Len: 1})
			st := readSubType(r)
			st.Group = g
			m.Types = append(m.Types, st)
		}
	}
}

func readSubType(r *reader) SubType {
	var st SubType
	switch r.peek() {
	case 0x50, 0x4F:
		st.Final = r.byte() == 0x4F
		n := r.u32()
		for i := uint32(0); i < n; i++ {
			st.Supers = append(st.Supers, r.u32())
		}
	default:
		st.Final = true
	}
	st.Comp = readCompType(r)
	return st
}

func readCompType(r *reader) CompType {
	var ct CompType
	ct.Kind = r.byte()
	switch ct.Kind {
	case KindFunc:
		n := r.u32()
		for i := uint32(0); i < n; i++ {
			ct.Func.Params = append(ct.Func.Params, readValType(r))
		}
		n = r.u32()
		for i := uint32(0); i < n; i++ {
			ct.Func.Results = append(ct.Func.Results, readValType(r))
		}
	case KindStruct:
		n := r.u32()
		for i := uint32(0); i < n; i++ {
			ct.Fields = append(ct.Fields, readFieldType(r))
		}
	case KindArray:
		ct.Fields = []FieldType{readFieldType(r)}
	default:
		r.fail("malformed composite type 0x%x", ct.Kind)
	}
	return ct
}

func readFieldType(r *reader) FieldType {
	var ft FieldType
	switch r.peek() {
	case I8, I16:
		ft.Type = ValType{Kind: r.byte()}
	default:
		ft.Type = readValType(r)
	}
	switch r.byte() {
	case 0:
	case 1:
		ft.Mutable = true
	default:
		r.fail("malformed mutability")
	}
	return ft
}

func readHeapType(r *reader) HeapType {
	v := r.s33()
	if v >= 0 {
		return HeapType(v)
	}
	h := HeapType(v)
	switch h {
	case HeapNoExn, HeapNoFunc, HeapNoExtern, HeapNone, HeapFunc, HeapExtern,
		HeapAny, HeapEq, HeapI31, HeapStruct, HeapArray, HeapExn:
		return h
	}
	r.fail("malformed heap type")
	return 0
}

func readValType(r *reader) ValType {
	c := r.byte()
	switch c {
	case I32, I64, F32, F64, V128:
		return ValType{Kind: c}
	case 0x63:
		return RefType(readHeapType(r), true)
	case 0x64:
		return RefType(readHeapType(r), false)
	case 0x70, 0x6F, 0x6E, 0x6D, 0x6C, 0x6B, 0x6A, 0x69, 0x74, 0x73, 0x72, 0x71:
		return RefType(HeapType(int32(c)-0x80), true)
	}
	r.pos--
	r.fail("malformed value type 0x%x", c)
	return ValType{}
}

func readLimits(r *reader) Limits {
	var l Limits
	flag := r.byte()
	if flag > 7 {
		r.fail("malformed limits flags")
	}
	l.Is64 = flag&4 != 0
	l.Shared = flag&2 != 0
	l.HasMax = flag&1 != 0
	if l.Is64 {
		l.Min = r.u64()
		if l.HasMax {
			l.Max = r.u64()
		}
	} else {
		l.Min = uint64(r.u32())
		if l.HasMax {
			l.Max = uint64(r.u32())
		}
	}
	return l
}

func readTableType(r *reader) TableType {
	t := TableType{Elem: readValType(r)}
	if !t.Elem.IsRef() {
		r.fail("malformed reference type")
	}
	t.Limits = readLimits(r)
	if t.Limits.Shared {
		r.fail("malformed limits flags")
	}
	return t
}

func readGlobalType(r *reader) GlobalType {
	g := GlobalType{Type: readValType(r)}
	switch r.byte() {
	case 0:
	case 1:
		g.Mutable = true
	default:
		r.fail("malformed mutability")
	}
	return g
}

func funcIdxExpr(f uint32) []Instr {
	return []Instr{{Op: OpRefFunc, A: f}, {Op: OpEnd}}
}

func decodeElems(r *reader, m *Module) {
	n := r.u32()
	for i := uint32(0); i < n; i++ {
		var e Elem
		flag := r.u32()
		if flag > 7 {
			r.fail("malformed elements segment kind")
		}
		switch flag {
		case 0, 4:
			e.Mode = SegActive
			e.Offset = readConstExpr(r)
		case 1, 5:
			e.Mode = SegPassive
		case 2, 6:
			e.Mode = SegActive
			e.Table = r.u32()
			e.Offset = readConstExpr(r)
		case 3, 7:
			e.Mode = SegDeclarative
		}
		if flag&4 == 0 {
			// function index vector
			if flag&3 != 0 {
				if r.byte() != 0 {
					r.fail("malformed element kind")
				}
			}
			e.Type = RefType(HeapFunc, false)
			cnt := r.u32()
			for j := uint32(0); j < cnt; j++ {
				e.Inits = append(e.Inits, funcIdxExpr(r.u32()))
			}
		} else {
			if flag&3 != 0 {
				e.Type = readValType(r)
			} else {
				e.Type = RefType(HeapFunc, true)
			}
			cnt := r.u32()
			for j := uint32(0); j < cnt; j++ {
				e.Inits = append(e.Inits, readConstExpr(r))
			}
		}
		m.Elems = append(m.Elems, e)
	}
}

func readConstExpr(r *reader) []Instr {
	var out []Instr
	for {
		in := readInstr(r)
		out = append(out, in)
		if in.Op == OpEnd {
			return out
		}
	}
}

func readBody(r *reader) []Instr {
	var out []Instr
	depth := 0
	for {
		in := readInstr(r)
		out = append(out, in)
		switch in.Op {
		case OpBlock, OpLoop, OpIf, OpTry, OpTryTable:
			depth++
		case OpEnd:
			if depth == 0 {
				return out
			}
			depth--
		case OpDelegate:
			// delegate terminates a legacy try block
			depth--
		}
	}
}

func readBlockType(r *reader) BlockType {
	c := r.peek()
	if c == 0x40 {
		r.byte()
		return BlockType{}
	}
	switch c {
	case I32, I64, F32, F64, V128, 0x63, 0x64, 0x70, 0x6F, 0x6E, 0x6D, 0x6C, 0x6B, 0x6A, 0x69, 0x74, 0x73, 0x72, 0x71:
		return BlockType{Kind: 1, Val: readValType(r)}
	}
	v := r.s33()
	if v < 0 {
		r.fail("malformed block type")
	}
	return BlockType{Kind: 2, Idx: uint32(v)}
}

func readMemArg(r *reader, in *Instr) {
	align := r.u32()
	if align&0x40 != 0 {
		align &^= 0x40
		in.B = r.u32() // memory index
	}
	in.Align = align
	in.Offset = r.u64()
}

func readInstr(r *reader) Instr {
	c := r.byte()
	in := Instr{Op: Op(c)}
	switch {
	case c == 0x02 || c == 0x03 || c == 0x04 || c == 0x06:
		in.Block = readBlockType(r)
	case c == 0x07 || c == 0x08 || c == 0x09 || c == 0x0C || c == 0x0D || c == 0x10 || c == 0x12 ||
		c == 0x14 || c == 0x15 || c == 0x18 || (c >= 0x20 && c <= 0x26) || c == 0xD2 || c == 0xD5 || c == 0xD6:
		in.A = r.u32()
	case c == 0x0E:
		n := r.u32()
		for i := uint32(0); i <= n; i++ {
			in.Labels = append(in.Labels, r.u32())
		}
	case c == 0x11 || c == 0x13:
		in.A = r.u32()
		in.B = r.u32()
	case c == 0x1C:
		n := r.u32()
		for i := uint32(0); i < n; i++ {
			in.Types = append(in.Types, readValType(r))
		}
	case c == 0x1F:
		in.Block = readBlockType(r)
		n := r.u32()
		for i := uint32(0); i < n; i++ {
			k := r.byte()
			if k > 3 {
				r.fail("malformed catch clause")
			}
			cc := Catch{Kind: k}
			if k < 2 {
				cc.Tag = r.u32()
			}
			cc.Label = r.u32()
			in.Catch = append(in.Catch, cc)
		}
	case c >= 0x28 && c <= 0x3E:
		readMemArg(r, &in)
	case c == 0x3F || c == 0x40:
		in.A = r.u32()
	case c == 0x41:
		in.I64 = int64(r.s32())
	case c == 0x42:
		in.I64 = r.s64()
	case c == 0x43:
		in.I64 = int64(r.f32bits())
	case c == 0x44:
		in.I64 = int64(r.f64bits())
	case c == 0xD0:
		in.HT = readHeapType(r)
	case c == PrefixGC:
		readGCInstr(r, &in)
	case c == PrefixMisc:
		readMiscInstr(r, &in)
	case c == PrefixSIMD:
		readSIMDInstr(r, &in)
	case c <= 0x0B || c == 0x0F || c == 0x19 || c == 0x1A || c == 0x1B || (c >= 0x45 && c <= 0xC4) || c == 0xD1 || c == 0xD3 || c == 0xD4:
		// no immediates
	default:
		r.pos--
		r.fail("illegal opcode 0x%x", c)
	}
	return in
}

func readGCInstr(r *reader, in *Instr) {
	sub := r.u32()
	in.Op = gc(sub)
	switch sub {
	case 0, 1, 6, 7, 11, 12, 13, 14, 16:
		in.A = r.u32()
	case 2, 3, 4, 5, 9, 10, 17, 18, 19:
		in.A = r.u32()
		in.B = r.u32()
	case 8:
		in.A = r.u32()
		in.B = r.u32()
	case 15, 26, 27, 28, 29, 30:
	case 20, 21, 22, 23:
		in.HT = readHeapType(r)
		in.Null2 = sub == 21 || sub == 23
	case 24, 25:
		flags := r.byte()
		if flags > 3 {
			r.fail("malformed br_on_cast flags")
		}
		in.A = r.u32()
		in.HT = readHeapType(r)
		in.HT2 = readHeapType(r)
		in.Null1 = flags&1 != 0
		in.Null2 = flags&2 != 0
	default:
		r.fail("illegal opcode 0xfb %d", sub)
	}
}

func readMiscInstr(r *reader, in *Instr) {
	sub := r.u32()
	in.Op = misc(sub)
	switch sub {
	case 0, 1, 2, 3, 4, 5, 6, 7:
	case 8:
		in.A = r.u32() // data
		in.B = r.u32() // mem
	case 9, 13:
		in.A = r.u32()
	case 10:
		in.A = r.u32() // dst mem
		in.B = r.u32() // src mem
	case 11:
		in.A = r.u32()
	case 12:
		in.A = r.u32() // elem
		in.B = r.u32() // table
	case 14:
		in.A = r.u32() // dst
		in.B = r.u32() // src
	case 15, 16, 17:
		in.A = r.u32()
	default:
		r.fail("illegal opcode 0xfc %d", sub)
	}
}

func readSIMDInstr(r *reader, in *Instr) {
	sub := r.u32()
	in.Op = SIMDOp(sub)
	if _, ok := SIMDOps[sub]; !ok {
		r.fail("illegal opcode 0xfd %d", sub)
	}
	switch {
	case simdHasMemArg(sub):
		readMemArg(r, in)
		if sub >= 84 && sub <= 91 {
			in.Lane = r.byte()
		}
	case sub == 12 || sub == 13:
		var v [16]byte
		copy(v[:], r.bytes(16))
		in.V128 = &v
	case simdHasLane(sub):
		in.Lane = r.byte()
	}
}

func decodeNameMap(r *reader) map[uint32]string {
	m := map[uint32]string{}
	n := r.u32()
	for i := uint32(0); i < n; i++ {
		idx := r.u32()
		m[idx] = r.name()
	}
	return m
}

func decodeIndirectNameMap(r *reader) map[uint32]map[uint32]string {
	m := map[uint32]map[uint32]string{}
	n := r.u32()
	for i := uint32(0); i < n; i++ {
		idx := r.u32()
		m[idx] = decodeNameMap(r)
	}
	return m
}

func decodeNames(r *reader, n *Names) {
	for !r.eof() {
		id := r.byte()
		size := r.u32()
		sr := &reader{b: r.bytes(int(size))}
		func() {
			defer func() { recover() }()
			switch id {
			case 0:
				n.Module = sr.name()
			case 1:
				n.Funcs = decodeNameMap(sr)
			case 2:
				n.Locals = decodeIndirectNameMap(sr)
			case 4:
				n.Types = decodeNameMap(sr)
			case 5:
				n.Tables = decodeNameMap(sr)
			case 6:
				n.Mems = decodeNameMap(sr)
			case 7:
				n.Globals = decodeNameMap(sr)
			case 10:
				n.Fields = decodeIndirectNameMap(sr)
			case 11:
				n.Tags = decodeNameMap(sr)
			}
		}()
	}
}
