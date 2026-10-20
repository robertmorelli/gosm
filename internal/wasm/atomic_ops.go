package wasm

// Threads proposal: atomic memory instructions (prefix 0xFE).

const PrefixAtomic = 0xFE

// AtomicOp returns the Op for atomic subopcode n.
func AtomicOp(n uint32) Op { return Op(PrefixAtomic<<16 | n) }

// Atomic instruction kinds.
const (
	AtomicNotify = iota
	AtomicWait
	AtomicFence
	AtomicLoad
	AtomicStore
	AtomicRMW
	AtomicCmpxchg
)

// RMW operations.
const (
	RMWAdd = iota
	RMWSub
	RMWAnd
	RMWOr
	RMWXor
	RMWXchg
)

// AtomicInfo describes an atomic instruction.
type AtomicInfo struct {
	Name string
	Kind int
	Type byte // I32 or I64: the value type
	Size int  // access size in bytes
	RMW  int
}

// AtomicOps maps subopcodes to instruction descriptions.
var AtomicOps = map[uint32]AtomicInfo{}

func init() {
	AtomicOps[0x00] = AtomicInfo{"memory.atomic.notify", AtomicNotify, I32, 4, 0}
	AtomicOps[0x01] = AtomicInfo{"memory.atomic.wait32", AtomicWait, I32, 4, 0}
	AtomicOps[0x02] = AtomicInfo{"memory.atomic.wait64", AtomicWait, I64, 8, 0}
	AtomicOps[0x03] = AtomicInfo{"atomic.fence", AtomicFence, 0, 0, 0}
	type shape struct {
		prefix string
		t      byte
		size   int
		suffix string
	}
	loads := []shape{{"i32.atomic.load", I32, 4, ""}, {"i64.atomic.load", I64, 8, ""},
		{"i32.atomic.load8", I32, 1, "_u"}, {"i32.atomic.load16", I32, 2, "_u"},
		{"i64.atomic.load8", I64, 1, "_u"}, {"i64.atomic.load16", I64, 2, "_u"}, {"i64.atomic.load32", I64, 4, "_u"}}
	for i, s := range loads {
		AtomicOps[uint32(0x10+i)] = AtomicInfo{s.prefix + s.suffix, AtomicLoad, s.t, s.size, 0}
	}
	stores := []shape{{"i32.atomic.store", I32, 4, ""}, {"i64.atomic.store", I64, 8, ""},
		{"i32.atomic.store8", I32, 1, ""}, {"i32.atomic.store16", I32, 2, ""},
		{"i64.atomic.store8", I64, 1, ""}, {"i64.atomic.store16", I64, 2, ""}, {"i64.atomic.store32", I64, 4, ""}}
	for i, s := range stores {
		AtomicOps[uint32(0x17+i)] = AtomicInfo{s.prefix, AtomicStore, s.t, s.size, 0}
	}
	rmwShapes := []shape{{"i32.atomic.rmw", I32, 4, ""}, {"i64.atomic.rmw", I64, 8, ""},
		{"i32.atomic.rmw8", I32, 1, "_u"}, {"i32.atomic.rmw16", I32, 2, "_u"},
		{"i64.atomic.rmw8", I64, 1, "_u"}, {"i64.atomic.rmw16", I64, 2, "_u"}, {"i64.atomic.rmw32", I64, 4, "_u"}}
	for g, op := range []string{"add", "sub", "and", "or", "xor", "xchg", "cmpxchg"} {
		for i, s := range rmwShapes {
			info := AtomicInfo{s.prefix + "." + op + s.suffix, AtomicRMW, s.t, s.size, g}
			if op == "cmpxchg" {
				info.Kind = AtomicCmpxchg
			}
			AtomicOps[uint32(0x1E+7*g+i)] = info
		}
	}
}

func readAtomicInstr(r *reader, in *Instr) {
	sub := r.u32()
	in.Op = AtomicOp(sub)
	info, ok := AtomicOps[sub]
	if !ok {
		r.fail("illegal opcode 0xfe %d", sub)
	}
	if info.Kind == AtomicFence {
		if r.byte() != 0 {
			r.fail("malformed atomic.fence")
		}
		return
	}
	readMemArg(r, in)
}
