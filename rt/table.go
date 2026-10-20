package rt

// MaxTableSize caps table growth.
var MaxTableSize uint64 = 10_000_000

// Table is a wasm table whose elements have Go type T (*Func for function
// references, any for externref/anyref, *S for concrete struct refs, ...).
type Table[T comparable] struct {
	Elems  []T
	Max    uint64
	HasMax bool
	Is64   bool
	// ElemType is the canonical description of the element reference type,
	// used to check imports.
	ElemType string
}

func NewTable[T comparable](min, max uint64, hasMax, is64 bool, elemType string, init T) *Table[T] {
	if min > MaxTableSize {
		panic(&Trap{"table size exceeds implementation limit"})
	}
	t := &Table[T]{Elems: make([]T, min), Max: max, HasMax: hasMax, Is64: is64, ElemType: elemType}
	var z T
	if init != z {
		for i := range t.Elems {
			t.Elems[i] = init
		}
	}
	return t
}

func (t *Table[T]) Size() uint64 { return uint64(len(t.Elems)) }

func (t *Table[T]) Get(i uint64) T {
	if i >= uint64(len(t.Elems)) {
		panic(ErrTableOOB)
	}
	return t.Elems[i]
}

func (t *Table[T]) Set(i uint64, v T) {
	if i >= uint64(len(t.Elems)) {
		panic(ErrTableOOB)
	}
	t.Elems[i] = v
}

// Grow implements table.grow, returning the old size or -1.
func (t *Table[T]) Grow(n uint64, v T) int64 {
	old := uint64(len(t.Elems))
	limit := MaxTableSize
	if !t.Is64 && limit > 0xFFFFFFFF {
		limit = 0xFFFFFFFF
	}
	if t.HasMax && t.Max < limit {
		limit = t.Max
	}
	if n > limit || old+n > limit {
		return -1
	}
	for i := uint64(0); i < n; i++ {
		t.Elems = append(t.Elems, v)
	}
	return int64(old)
}

func (t *Table[T]) Fill(i uint64, v T, n uint64) {
	if i > uint64(len(t.Elems)) || uint64(len(t.Elems))-i < n {
		panic(ErrTableOOB)
	}
	for k := uint64(0); k < n; k++ {
		t.Elems[i+k] = v
	}
}

// TableCopy implements table.copy between tables of the same Go element type.
func TableCopy[T comparable](dst *Table[T], d uint64, src *Table[T], s, n uint64) {
	if s > uint64(len(src.Elems)) || uint64(len(src.Elems))-s < n ||
		d > uint64(len(dst.Elems)) || uint64(len(dst.Elems))-d < n {
		panic(ErrTableOOB)
	}
	copy(dst.Elems[d:d+n], src.Elems[s:s+n])
}

// TableCopyConv implements table.copy when element Go types differ.
func TableCopyConv[D, S comparable](dst *Table[D], d uint64, src *Table[S], s, n uint64, conv func(S) D) {
	if s > uint64(len(src.Elems)) || uint64(len(src.Elems))-s < n ||
		d > uint64(len(dst.Elems)) || uint64(len(dst.Elems))-d < n {
		panic(ErrTableOOB)
	}
	tmp := make([]D, n)
	for i := range tmp {
		tmp[i] = conv(src.Elems[s+uint64(i)])
	}
	copy(dst.Elems[d:], tmp)
}

// Init implements table.init from an element segment.
func (t *Table[T]) Init(d uint64, seg []T, s, n uint64) {
	if s > uint64(len(seg)) || uint64(len(seg))-s < n ||
		d > uint64(len(t.Elems)) || uint64(len(t.Elems))-d < n {
		panic(ErrTableOOB)
	}
	copy(t.Elems[d:d+n], seg[s:s+n])
}

// Global is a shared (imported or exported) global cell.
type Global[T any] struct {
	V       T
	Mutable bool
	// Type is the canonical description of the value type.
	Type string
}
