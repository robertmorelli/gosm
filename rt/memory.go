package rt

import (
	"encoding/binary"
	"math"
)

const PageSize = 65536

// MaxMemoryPages caps how far any memory may grow (default 4 GiB) so that
// memory.grow fails gracefully instead of exhausting the Go heap.
var MaxMemoryPages uint64 = 65536

// Memory is a linear memory. Data is replaced when the memory grows, so
// hosts must re-read it after calling into wasm.
type Memory struct {
	Data   []byte
	Max    uint64 // maximum pages (valid if HasMax)
	HasMax bool
	Is64   bool
}

// NewMemory allocates a memory with min pages.
func NewMemory(min, max uint64, hasMax, is64 bool) *Memory {
	if min > MaxMemoryPages {
		panic(&Trap{"memory size exceeds implementation limit"})
	}
	return &Memory{Data: make([]byte, min*PageSize), Max: max, HasMax: hasMax, Is64: is64}
}

func (m *Memory) Pages() uint64 { return uint64(len(m.Data)) / PageSize }

// Grow grows the memory by delta pages, returning the old size in pages or
// -1 on failure.
func (m *Memory) Grow(delta uint64) int64 {
	old := m.Pages()
	if delta == 0 {
		return int64(old)
	}
	limit := MaxMemoryPages
	if !m.Is64 && limit > 65536 {
		limit = 65536
	}
	if m.HasMax && m.Max < limit {
		limit = m.Max
	}
	if delta > limit || old+delta > limit {
		return -1
	}
	n := (old + delta) * PageSize
	if uint64(cap(m.Data)) >= n {
		m.Data = m.Data[:n]
	} else {
		c := uint64(cap(m.Data)) * 2
		if c < n {
			c = n
		}
		if c > limit*PageSize {
			c = limit * PageSize
		}
		nd := make([]byte, n, c)
		copy(nd, m.Data)
		m.Data = nd
	}
	return int64(old)
}

// EA64 computes a 64-bit effective address, trapping on overflow.
func EA64(addr, off uint64) uint64 {
	ea := addr + off
	if ea < addr {
		panic(ErrMemOOB)
	}
	return ea
}

func (m *Memory) check(ea, n uint64) []byte {
	if ea > uint64(len(m.Data)) || uint64(len(m.Data))-ea < n {
		panic(ErrMemOOB)
	}
	return m.Data[ea : ea+n]
}

func (m *Memory) L8(ea uint64) uint8 {
	if ea >= uint64(len(m.Data)) {
		panic(ErrMemOOB)
	}
	return m.Data[ea]
}

func (m *Memory) L16(ea uint64) uint16 { return binary.LittleEndian.Uint16(m.check(ea, 2)) }
func (m *Memory) L32(ea uint64) uint32 { return binary.LittleEndian.Uint32(m.check(ea, 4)) }
func (m *Memory) L64(ea uint64) uint64 { return binary.LittleEndian.Uint64(m.check(ea, 8)) }

func (m *Memory) LF32(ea uint64) float32 { return math.Float32frombits(m.L32(ea)) }
func (m *Memory) LF64(ea uint64) float64 { return math.Float64frombits(m.L64(ea)) }

func (m *Memory) LV128(ea uint64) V128 {
	b := m.check(ea, 16)
	return V128{binary.LittleEndian.Uint64(b), binary.LittleEndian.Uint64(b[8:])}
}

func (m *Memory) S8(ea uint64, v uint8) {
	if ea >= uint64(len(m.Data)) {
		panic(ErrMemOOB)
	}
	m.Data[ea] = v
}

func (m *Memory) S16(ea uint64, v uint16) { binary.LittleEndian.PutUint16(m.check(ea, 2), v) }
func (m *Memory) S32(ea uint64, v uint32) { binary.LittleEndian.PutUint32(m.check(ea, 4), v) }
func (m *Memory) S64(ea uint64, v uint64) { binary.LittleEndian.PutUint64(m.check(ea, 8), v) }

func (m *Memory) SF32(ea uint64, v float32) { m.S32(ea, math.Float32bits(v)) }
func (m *Memory) SF64(ea uint64, v float64) { m.S64(ea, math.Float64bits(v)) }

func (m *Memory) SV128(ea uint64, v V128) {
	b := m.check(ea, 16)
	binary.LittleEndian.PutUint64(b, v.Lo)
	binary.LittleEndian.PutUint64(b[8:], v.Hi)
}

// Fill implements memory.fill.
func (m *Memory) Fill(d uint64, val byte, n uint64) {
	b := m.check(d, n)
	for i := range b {
		b[i] = val
	}
}

// MemCopy implements memory.copy (possibly between distinct memories).
func MemCopy(dst *Memory, d uint64, src *Memory, s, n uint64) {
	db := dst.check(d, n)
	sb := src.check(s, n)
	copy(db, sb)
}

// Init implements memory.init from a (possibly dropped, i.e. nil) segment.
func (m *Memory) Init(d uint64, seg []byte, s, n uint64) {
	if s > uint64(len(seg)) || uint64(len(seg))-s < n {
		panic(ErrMemOOB)
	}
	db := m.check(d, n)
	copy(db, seg[s:s+n])
}

// String reads a byte string from memory (a convenience for hosts).
func (m *Memory) String(ptr, n uint64) string { return string(m.check(ptr, n)) }
