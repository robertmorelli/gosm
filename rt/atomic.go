package rt

// Threads: shared memories, atomic accesses, wait and notify.
//
// A wasm thread is a goroutine running its own Instance; instances share
// memories by importing the same *Memory. A shared memory reserves its
// maximum size up front, so its backing array never moves while other
// goroutines access it.

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	ErrUnaligned = &Trap{"unaligned atomic"}
	ErrNotShared = &Trap{"expected shared memory"}
)

// NewSharedMemory allocates a shared memory with min pages, able to grow to
// max pages in place.
func NewSharedMemory(min, max uint64, is64 bool) *Memory {
	limit := max
	if limit > MaxMemoryPages {
		limit = MaxMemoryPages
	}
	if min > limit {
		panic(&Trap{"memory size exceeds implementation limit"})
	}
	return &Memory{
		Data:   make([]byte, min*PageSize, limit*PageSize),
		Max:    max,
		HasMax: true,
		Is64:   is64,
		Shared: true,
	}
}

type waiter struct{ ch chan struct{} }

type waitQueue struct {
	mu      sync.Mutex
	waiters map[uint64][]*waiter
}

func (m *Memory) queue() *waitQueue {
	if q := m.wq.Load(); q != nil {
		return q
	}
	m.wq.CompareAndSwap(nil, &waitQueue{waiters: map[uint64][]*waiter{}})
	return m.wq.Load()
}

// atomicAddr checks bounds and alignment and returns a pointer to the
// accessed bytes.
func (m *Memory) atomicAddr(ea uint64, size uint64) unsafe.Pointer {
	if ea&(size-1) != 0 {
		panic(ErrUnaligned)
	}
	b := m.check(ea, size)
	return unsafe.Pointer(&b[0])
}

// word returns the aligned 32-bit word containing the sub-word access at ea
// and the bit shift of the accessed bytes within it.
func (m *Memory) word(ea uint64) (*uint32, uint) {
	base := ea &^ 3
	p := (*uint32)(unsafe.Pointer(&m.Data[base]))
	return p, uint(ea-base) * 8
}

// ALoad implements atomic loads of size 1, 2, 4 or 8 bytes.
func (m *Memory) ALoad(ea uint64, size uint64) uint64 {
	p := m.atomicAddr(ea, size)
	switch size {
	case 8:
		return atomic.LoadUint64((*uint64)(p))
	case 4:
		return uint64(atomic.LoadUint32((*uint32)(p)))
	}
	w, sh := m.word(ea)
	mask := uint32(1)<<(8*size) - 1
	return uint64(atomic.LoadUint32(w) >> sh & mask)
}

// AStore implements atomic stores.
func (m *Memory) AStore(ea uint64, size uint64, v uint64) {
	m.ARMW(ea, size, rmwXchg, v)
}

// RMW operation codes for ARMW (match wasm.RMW*).
const (
	rmwAdd = iota
	rmwSub
	rmwAnd
	rmwOr
	rmwXor
	rmwXchg
)

func rmwApply(op int, old, v uint64) uint64 {
	switch op {
	case rmwAdd:
		return old + v
	case rmwSub:
		return old - v
	case rmwAnd:
		return old & v
	case rmwOr:
		return old | v
	case rmwXor:
		return old ^ v
	}
	return v
}

// ARMW implements atomic read-modify-write operations, returning the old
// value (zero-extended).
func (m *Memory) ARMW(ea uint64, size uint64, op int, v uint64) uint64 {
	p := m.atomicAddr(ea, size)
	switch size {
	case 8:
		q := (*uint64)(p)
		switch op {
		case rmwAdd:
			return atomic.AddUint64(q, v) - v
		case rmwXchg:
			return atomic.SwapUint64(q, v)
		}
		for {
			old := atomic.LoadUint64(q)
			if atomic.CompareAndSwapUint64(q, old, rmwApply(op, old, v)) {
				return old
			}
		}
	case 4:
		q := (*uint32)(p)
		v32 := uint32(v)
		switch op {
		case rmwAdd:
			return uint64(atomic.AddUint32(q, v32) - v32)
		case rmwXchg:
			return uint64(atomic.SwapUint32(q, v32))
		}
		for {
			old := atomic.LoadUint32(q)
			if atomic.CompareAndSwapUint32(q, old, uint32(rmwApply(op, uint64(old), v))) {
				return uint64(old)
			}
		}
	}
	w, sh := m.word(ea)
	mask := uint32(1)<<(8*size) - 1
	for {
		old := atomic.LoadUint32(w)
		cur := uint64(old >> sh & mask)
		nv := uint32(rmwApply(op, cur, v)) & mask
		if atomic.CompareAndSwapUint32(w, old, old&^(mask<<sh)|nv<<sh) {
			return cur
		}
	}
}

// ACmpxchg implements atomic compare-exchange, returning the old value.
func (m *Memory) ACmpxchg(ea uint64, size uint64, expected, repl uint64) uint64 {
	p := m.atomicAddr(ea, size)
	switch size {
	case 8:
		q := (*uint64)(p)
		for {
			old := atomic.LoadUint64(q)
			if old != expected || atomic.CompareAndSwapUint64(q, old, repl) {
				return old
			}
		}
	case 4:
		q := (*uint32)(p)
		e, r := uint32(expected), uint32(repl)
		for {
			old := atomic.LoadUint32(q)
			if old != e || atomic.CompareAndSwapUint32(q, old, r) {
				return uint64(old)
			}
		}
	}
	w, sh := m.word(ea)
	mask := uint32(1)<<(8*size) - 1
	e, r := uint32(expected)&mask, uint32(repl)&mask
	for {
		old := atomic.LoadUint32(w)
		cur := old >> sh & mask
		if cur != e || atomic.CompareAndSwapUint32(w, old, old&^(mask<<sh)|r<<sh) {
			return uint64(cur)
		}
	}
}

// Wait implements memory.atomic.wait32/64: it returns 0 when woken by
// notify, 1 if the value did not match expected, and 2 on timeout
// (timeout < 0 waits forever; it is in nanoseconds).
func (m *Memory) Wait(ea uint64, size uint64, expected uint64, timeout int64) int32 {
	m.atomicAddr(ea, size)
	if !m.Shared {
		panic(ErrNotShared)
	}
	q := m.queue()
	q.mu.Lock()
	if m.ALoad(ea, size) != expected {
		q.mu.Unlock()
		return 1
	}
	w := &waiter{ch: make(chan struct{})}
	q.waiters[ea] = append(q.waiters[ea], w)
	q.mu.Unlock()
	if timeout < 0 {
		<-w.ch
		return 0
	}
	t := time.NewTimer(time.Duration(timeout))
	defer t.Stop()
	select {
	case <-w.ch:
		return 0
	case <-t.C:
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	ws := q.waiters[ea]
	for i, x := range ws {
		if x == w {
			q.waiters[ea] = append(ws[:i:i], ws[i+1:]...)
			return 2
		}
	}
	return 0 // notified concurrently with the timeout
}

// Notify implements memory.atomic.notify, returning the number of waiters
// woken.
func (m *Memory) Notify(ea uint64, count uint32) int32 {
	m.atomicAddr(ea, 4)
	if !m.Shared {
		return 0
	}
	q := m.queue()
	q.mu.Lock()
	defer q.mu.Unlock()
	ws := q.waiters[ea]
	n := 0
	for n < len(ws) && uint32(n) < count {
		close(ws[n].ch)
		n++
	}
	if n == len(ws) {
		delete(q.waiters, ea)
	} else {
		q.waiters[ea] = ws[n:]
	}
	return int32(n)
}

// Fence implements atomic.fence.
func Fence() {
	var x atomic.Int32
	x.Add(0)
}
