package compiler

import (
	"fmt"
	"strings"

	"gosm/internal/wasm"
)

type opTmpl struct {
	n     int // operand count
	res   wasm.ValType
	tmpl  string
	eager bool // traps or has effects: evaluate immediately
}

var (
	ti32 = wasm.TI32
	ti64 = wasm.TI64
	tf32 = wasm.TF32
	tf64 = wasm.TF64
)

// Templates: {a} {b} are operands; {ua} {ub} are operands as uint32/uint64.
var numOps = map[wasm.Op]opTmpl{
	wasm.OpI32Eqz:    {1, ti32, "rt.B2I({a} == 0)", false},
	wasm.OpI32Eq:     {2, ti32, "rt.B2I({a} == {b})", false},
	wasm.OpI32Ne:     {2, ti32, "rt.B2I({a} != {b})", false},
	wasm.OpI32LtS:    {2, ti32, "rt.B2I({a} < {b})", false},
	wasm.OpI32LtU:    {2, ti32, "rt.B2I({ua} < {ub})", false},
	wasm.OpI32GtS:    {2, ti32, "rt.B2I({a} > {b})", false},
	wasm.OpI32GtU:    {2, ti32, "rt.B2I({ua} > {ub})", false},
	wasm.OpI32LeS:    {2, ti32, "rt.B2I({a} <= {b})", false},
	wasm.OpI32LeU:    {2, ti32, "rt.B2I({ua} <= {ub})", false},
	wasm.OpI32GeS:    {2, ti32, "rt.B2I({a} >= {b})", false},
	wasm.OpI32GeU:    {2, ti32, "rt.B2I({ua} >= {ub})", false},
	wasm.OpI64Eqz:    {1, ti32, "rt.B2I({a} == 0)", false},
	wasm.OpI64Eq:     {2, ti32, "rt.B2I({a} == {b})", false},
	wasm.OpI64Ne:     {2, ti32, "rt.B2I({a} != {b})", false},
	wasm.OpI64LtS:    {2, ti32, "rt.B2I({a} < {b})", false},
	wasm.OpI64LtU:    {2, ti32, "rt.B2I({ua} < {ub})", false},
	wasm.OpI64GtS:    {2, ti32, "rt.B2I({a} > {b})", false},
	wasm.OpI64GtU:    {2, ti32, "rt.B2I({ua} > {ub})", false},
	wasm.OpI64LeS:    {2, ti32, "rt.B2I({a} <= {b})", false},
	wasm.OpI64LeU:    {2, ti32, "rt.B2I({ua} <= {ub})", false},
	wasm.OpI64GeS:    {2, ti32, "rt.B2I({a} >= {b})", false},
	wasm.OpI64GeU:    {2, ti32, "rt.B2I({ua} >= {ub})", false},
	wasm.OpF32Eq:     {2, ti32, "rt.B2I({a} == {b})", false},
	wasm.OpF32Ne:     {2, ti32, "rt.B2I({a} != {b})", false},
	wasm.OpF32Lt:     {2, ti32, "rt.B2I({a} < {b})", false},
	wasm.OpF32Gt:     {2, ti32, "rt.B2I({a} > {b})", false},
	wasm.OpF32Le:     {2, ti32, "rt.B2I({a} <= {b})", false},
	wasm.OpF32Ge:     {2, ti32, "rt.B2I({a} >= {b})", false},
	wasm.OpF64Eq:     {2, ti32, "rt.B2I({a} == {b})", false},
	wasm.OpF64Ne:     {2, ti32, "rt.B2I({a} != {b})", false},
	wasm.OpF64Lt:     {2, ti32, "rt.B2I({a} < {b})", false},
	wasm.OpF64Gt:     {2, ti32, "rt.B2I({a} > {b})", false},
	wasm.OpF64Le:     {2, ti32, "rt.B2I({a} <= {b})", false},
	wasm.OpF64Ge:     {2, ti32, "rt.B2I({a} >= {b})", false},
	wasm.OpI32Clz:    {1, ti32, "int32(bits.LeadingZeros32({ua}))", false},
	wasm.OpI32Ctz:    {1, ti32, "int32(bits.TrailingZeros32({ua}))", false},
	wasm.OpI32Popcnt: {1, ti32, "int32(bits.OnesCount32({ua}))", false},
	wasm.OpI32Add:    {2, ti32, "({a} + {b})", false},
	wasm.OpI32Sub:    {2, ti32, "({a} - {b})", false},
	wasm.OpI32Mul:    {2, ti32, "({a} * {b})", false},
	wasm.OpI32DivS:   {2, ti32, "rt.I32DivS({a}, {b})", true},
	wasm.OpI32DivU:   {2, ti32, "rt.I32DivU({a}, {b})", true},
	wasm.OpI32RemS:   {2, ti32, "rt.I32RemS({a}, {b})", true},
	wasm.OpI32RemU:   {2, ti32, "rt.I32RemU({a}, {b})", true},
	wasm.OpI32And:    {2, ti32, "({a} & {b})", false},
	wasm.OpI32Or:     {2, ti32, "({a} | {b})", false},
	wasm.OpI32Xor:    {2, ti32, "({a} ^ {b})", false},
	wasm.OpI32Shl:    {2, ti32, "({a} << ({ub} & 31))", false},
	wasm.OpI32ShrS:   {2, ti32, "({a} >> ({ub} & 31))", false},
	wasm.OpI32ShrU:   {2, ti32, "int32({ua} >> ({ub} & 31))", false},
	wasm.OpI32Rotl:   {2, ti32, "int32(bits.RotateLeft32({ua}, int({ub}&31)))", false},
	wasm.OpI32Rotr:   {2, ti32, "int32(bits.RotateLeft32({ua}, -int({ub}&31)))", false},
	wasm.OpI64Clz:    {1, ti64, "int64(bits.LeadingZeros64({ua}))", false},
	wasm.OpI64Ctz:    {1, ti64, "int64(bits.TrailingZeros64({ua}))", false},
	wasm.OpI64Popcnt: {1, ti64, "int64(bits.OnesCount64({ua}))", false},
	wasm.OpI64Add:    {2, ti64, "({a} + {b})", false},
	wasm.OpI64Sub:    {2, ti64, "({a} - {b})", false},
	wasm.OpI64Mul:    {2, ti64, "({a} * {b})", false},
	wasm.OpI64DivS:   {2, ti64, "rt.I64DivS({a}, {b})", true},
	wasm.OpI64DivU:   {2, ti64, "rt.I64DivU({a}, {b})", true},
	wasm.OpI64RemS:   {2, ti64, "rt.I64RemS({a}, {b})", true},
	wasm.OpI64RemU:   {2, ti64, "rt.I64RemU({a}, {b})", true},
	wasm.OpI64And:    {2, ti64, "({a} & {b})", false},
	wasm.OpI64Or:     {2, ti64, "({a} | {b})", false},
	wasm.OpI64Xor:    {2, ti64, "({a} ^ {b})", false},
	wasm.OpI64Shl:    {2, ti64, "({a} << ({ub} & 63))", false},
	wasm.OpI64ShrS:   {2, ti64, "({a} >> ({ub} & 63))", false},
	wasm.OpI64ShrU:   {2, ti64, "int64({ua} >> ({ub} & 63))", false},
	wasm.OpI64Rotl:   {2, ti64, "int64(bits.RotateLeft64({ua}, int({ub}&63)))", false},
	wasm.OpI64Rotr:   {2, ti64, "int64(bits.RotateLeft64({ua}, -int({ub}&63)))", false},

	wasm.OpF32Abs:      {1, tf32, "rt.F32Abs({a})", false},
	wasm.OpF32Neg:      {1, tf32, "rt.F32Neg({a})", false},
	wasm.OpF32Ceil:     {1, tf32, "rt.F32Ceil({a})", false},
	wasm.OpF32Floor:    {1, tf32, "rt.F32Floor({a})", false},
	wasm.OpF32Trunc:    {1, tf32, "rt.F32Trunc({a})", false},
	wasm.OpF32Nearest:  {1, tf32, "rt.F32Nearest({a})", false},
	wasm.OpF32Sqrt:     {1, tf32, "rt.F32Sqrt({a})", false},
	wasm.OpF32Add:      {2, tf32, "float32({a} + {b})", false},
	wasm.OpF32Sub:      {2, tf32, "float32({a} - {b})", false},
	wasm.OpF32Mul:      {2, tf32, "float32({a} * {b})", false},
	wasm.OpF32Div:      {2, tf32, "float32({a} / {b})", false},
	wasm.OpF32Min:      {2, tf32, "rt.F32Min({a}, {b})", false},
	wasm.OpF32Max:      {2, tf32, "rt.F32Max({a}, {b})", false},
	wasm.OpF32Copysign: {2, tf32, "rt.F32Copysign({a}, {b})", false},
	wasm.OpF64Abs:      {1, tf64, "rt.F64Abs({a})", false},
	wasm.OpF64Neg:      {1, tf64, "rt.F64Neg({a})", false},
	wasm.OpF64Ceil:     {1, tf64, "rt.F64Ceil({a})", false},
	wasm.OpF64Floor:    {1, tf64, "rt.F64Floor({a})", false},
	wasm.OpF64Trunc:    {1, tf64, "rt.F64Trunc({a})", false},
	wasm.OpF64Nearest:  {1, tf64, "rt.F64Nearest({a})", false},
	wasm.OpF64Sqrt:     {1, tf64, "rt.F64Sqrt({a})", false},
	wasm.OpF64Add:      {2, tf64, "float64({a} + {b})", false},
	wasm.OpF64Sub:      {2, tf64, "float64({a} - {b})", false},
	wasm.OpF64Mul:      {2, tf64, "float64({a} * {b})", false},
	wasm.OpF64Div:      {2, tf64, "float64({a} / {b})", false},
	wasm.OpF64Min:      {2, tf64, "rt.F64Min({a}, {b})", false},
	wasm.OpF64Max:      {2, tf64, "rt.F64Max({a}, {b})", false},
	wasm.OpF64Copysign: {2, tf64, "rt.F64Copysign({a}, {b})", false},

	wasm.OpI32WrapI64:        {1, ti32, "int32({a})", false},
	wasm.OpI32TruncF32S:      {1, ti32, "rt.I32TruncF32S({a})", true},
	wasm.OpI32TruncF32U:      {1, ti32, "rt.I32TruncF32U({a})", true},
	wasm.OpI32TruncF64S:      {1, ti32, "rt.I32TruncF64S({a})", true},
	wasm.OpI32TruncF64U:      {1, ti32, "rt.I32TruncF64U({a})", true},
	wasm.OpI64ExtendI32S:     {1, ti64, "int64({a})", false},
	wasm.OpI64ExtendI32U:     {1, ti64, "int64({ua})", false},
	wasm.OpI64TruncF32S:      {1, ti64, "rt.I64TruncF32S({a})", true},
	wasm.OpI64TruncF32U:      {1, ti64, "rt.I64TruncF32U({a})", true},
	wasm.OpI64TruncF64S:      {1, ti64, "rt.I64TruncF64S({a})", true},
	wasm.OpI64TruncF64U:      {1, ti64, "rt.I64TruncF64U({a})", true},
	wasm.OpF32ConvertI32S:    {1, tf32, "float32({a})", false},
	wasm.OpF32ConvertI32U:    {1, tf32, "float32({ua})", false},
	wasm.OpF32ConvertI64S:    {1, tf32, "float32({a})", false},
	wasm.OpF32ConvertI64U:    {1, tf32, "rt.F32ConvertU64({a})", false},
	wasm.OpF32DemoteF64:      {1, tf32, "float32({a})", false},
	wasm.OpF64ConvertI32S:    {1, tf64, "float64({a})", false},
	wasm.OpF64ConvertI32U:    {1, tf64, "float64({ua})", false},
	wasm.OpF64ConvertI64S:    {1, tf64, "float64({a})", false},
	wasm.OpF64ConvertI64U:    {1, tf64, "rt.F64ConvertU64({a})", false},
	wasm.OpF64PromoteF32:     {1, tf64, "float64({a})", false},
	wasm.OpI32ReinterpretF32: {1, ti32, "int32(math.Float32bits({a}))", false},
	wasm.OpI64ReinterpretF64: {1, ti64, "int64(math.Float64bits({a}))", false},
	wasm.OpF32ReinterpretI32: {1, tf32, "math.Float32frombits({ua})", false},
	wasm.OpF64ReinterpretI64: {1, tf64, "math.Float64frombits({ua})", false},
	wasm.OpI32Extend8S:       {1, ti32, "int32(int8({a}))", false},
	wasm.OpI32Extend16S:      {1, ti32, "int32(int16({a}))", false},
	wasm.OpI64Extend8S:       {1, ti64, "int64(int8({a}))", false},
	wasm.OpI64Extend16S:      {1, ti64, "int64(int16({a}))", false},
	wasm.OpI64Extend32S:      {1, ti64, "int64(int32({a}))", false},

	wasm.OpI32TruncSatF32S: {1, ti32, "rt.I32TruncSatF32S({a})", false},
	wasm.OpI32TruncSatF32U: {1, ti32, "rt.I32TruncSatF32U({a})", false},
	wasm.OpI32TruncSatF64S: {1, ti32, "rt.I32TruncSatF64S({a})", false},
	wasm.OpI32TruncSatF64U: {1, ti32, "rt.I32TruncSatF64U({a})", false},
	wasm.OpI64TruncSatF32S: {1, ti64, "rt.I64TruncSatF32S({a})", false},
	wasm.OpI64TruncSatF32U: {1, ti64, "rt.I64TruncSatF32U({a})", false},
	wasm.OpI64TruncSatF64S: {1, ti64, "rt.I64TruncSatF64S({a})", false},
	wasm.OpI64TruncSatF64U: {1, ti64, "rt.I64TruncSatF64U({a})", false},
}

func (f *fn) uOf(e entry) string {
	if e.t.Kind == wasm.I64 {
		return f.u64(e)
	}
	return f.u32(e)
}

func (f *fn) numeric(in *wasm.Instr) bool {
	op, ok := numOps[in.Op]
	if !ok {
		return false
	}
	if !op.eager {
		f.constGuard(op.n)
	}
	ops := f.popN(op.n)
	switch in.Op {
	case wasm.OpF32Add, wasm.OpF32Sub, wasm.OpF32Mul, wasm.OpF32Div,
		wasm.OpF64Add, wasm.OpF64Sub, wasm.OpF64Mul, wasm.OpF64Div:
		// Hide constant operands from the Go optimizer, which would
		// otherwise fold x*1 to x and lose NaN quieting.
		for i := range ops {
			if ops[i].kind == kConst {
				ops[i].expr = f.c.floatConstVar(ops[i].bits, ops[i].t.Kind == wasm.F64)
			}
		}
	}
	r := op.tmpl
	if strings.Contains(r, "{ua}") {
		r = strings.ReplaceAll(r, "{ua}", f.uOf(ops[0]))
	}
	if strings.Contains(r, "{a}") {
		r = strings.ReplaceAll(r, "{a}", f.ex(ops[0]))
	}
	if op.n > 1 {
		if strings.Contains(r, "{ub}") {
			r = strings.ReplaceAll(r, "{ub}", f.uOf(ops[1]))
		}
		if strings.Contains(r, "{b}") {
			r = strings.ReplaceAll(r, "{b}", f.ex(ops[1]))
		}
	}
	if strings.Contains(r, "bits.") {
		f.c.useImport("math/bits")
	}
	if strings.Contains(r, "math.") {
		f.c.useImport("math")
	}
	if op.eager {
		f.pushEager(op.res, r)
	} else {
		f.pushPure(op.res, r, ops...)
	}
	return true
}

// ea builds the effective address expression for a memory access.
func (f *fn) ea(mem uint32, addr entry, off uint64) string {
	if f.c.m.MemType(mem).Limits.Is64 {
		if off == 0 {
			return f.u64(addr)
		}
		if addr.kind == kConst && addr.isNum {
			s := addr.bits + off
			if s < addr.bits {
				return fmt.Sprintf("rt.EA64(%d, %d)", addr.bits, off)
			}
			return fmt.Sprintf("uint64(%d)", s)
		}
		return fmt.Sprintf("rt.EA64(uint64(%s), %d)", f.ex(addr), off)
	}
	if addr.kind == kConst && addr.isNum {
		return fmt.Sprintf("uint64(%d)", uint64(uint32(addr.bits))+off)
	}
	if off == 0 {
		return f.u64(addr)
	}
	return fmt.Sprintf("%s + %d", f.u64(addr), off)
}

type memTmpl struct {
	res  wasm.ValType
	tmpl string // {m} memory, {ea} address
}

var loadOps = map[wasm.Op]memTmpl{
	wasm.OpI32Load:    {ti32, "int32({m}.L32({ea}))"},
	wasm.OpI64Load:    {ti64, "int64({m}.L64({ea}))"},
	wasm.OpF32Load:    {tf32, "{m}.LF32({ea})"},
	wasm.OpF64Load:    {tf64, "{m}.LF64({ea})"},
	wasm.OpI32Load8S:  {ti32, "int32(int8({m}.L8({ea})))"},
	wasm.OpI32Load8U:  {ti32, "int32({m}.L8({ea}))"},
	wasm.OpI32Load16S: {ti32, "int32(int16({m}.L16({ea})))"},
	wasm.OpI32Load16U: {ti32, "int32({m}.L16({ea}))"},
	wasm.OpI64Load8S:  {ti64, "int64(int8({m}.L8({ea})))"},
	wasm.OpI64Load8U:  {ti64, "int64({m}.L8({ea}))"},
	wasm.OpI64Load16S: {ti64, "int64(int16({m}.L16({ea})))"},
	wasm.OpI64Load16U: {ti64, "int64({m}.L16({ea}))"},
	wasm.OpI64Load32S: {ti64, "int64(int32({m}.L32({ea})))"},
	wasm.OpI64Load32U: {ti64, "int64({m}.L32({ea}))"},
}

func (f *fn) memoryOp(in *wasm.Instr) bool {
	c := f.c
	if t, ok := loadOps[in.Op]; ok {
		addr := f.pop()
		r := strings.ReplaceAll(t.tmpl, "{m}", c.memRef(in.B))
		r = strings.ReplaceAll(r, "{ea}", f.ea(in.B, addr, in.Offset))
		f.pushEager(t.res, r)
		return true
	}
	mem := c.memRef(in.B)
	switch in.Op {
	case wasm.OpI32Store, wasm.OpI64Store, wasm.OpF32Store, wasm.OpF64Store,
		wasm.OpI32Store8, wasm.OpI32Store16, wasm.OpI64Store8, wasm.OpI64Store16, wasm.OpI64Store32:
		v := f.pop()
		addr := f.pop()
		ea := f.ea(in.B, addr, in.Offset)
		switch in.Op {
		case wasm.OpI32Store:
			f.emit("%s.S32(%s, %s)", mem, ea, f.u32(v))
		case wasm.OpI64Store:
			f.emit("%s.S64(%s, %s)", mem, ea, f.u64(v))
		case wasm.OpF32Store:
			f.emit("%s.SF32(%s, %s)", mem, ea, f.ex(v))
		case wasm.OpF64Store:
			f.emit("%s.SF64(%s, %s)", mem, ea, f.ex(v))
		case wasm.OpI32Store8, wasm.OpI64Store8:
			f.emit("%s.S8(%s, %s)", mem, ea, f.u8(v))
		case wasm.OpI32Store16, wasm.OpI64Store16:
			f.emit("%s.S16(%s, %s)", mem, ea, f.u16(v))
		case wasm.OpI64Store32:
			f.emit("%s.S32(%s, %s)", mem, ea, f.u32of64(v))
		}
		return true
	case wasm.OpMemorySize:
		mem := c.memRef(in.A)
		if c.m.MemType(in.A).Limits.Is64 {
			f.pushEager(ti64, fmt.Sprintf("int64(%s.Pages())", mem))
		} else {
			f.pushEager(ti32, fmt.Sprintf("int32(%s.Pages())", mem))
		}
		return true
	case wasm.OpMemoryGrow:
		mem := c.memRef(in.A)
		n := f.pop()
		if c.m.MemType(in.A).Limits.Is64 {
			f.pushEager(ti64, fmt.Sprintf("%s.Grow(%s)", mem, f.u64(n)))
		} else {
			f.pushEager(ti32, fmt.Sprintf("int32(%s.Grow(%s))", mem, f.u64(n)))
		}
		return true
	}
	switch in.Op {
	case wasm.OpMemoryInit:
		vals := f.popN(3)
		f.emit("%s.Init(%s, %s, %s, %s)", c.memRef(in.B), f.u64(vals[0]), c.dataRef(in.A), f.u64(vals[1]), f.u64(vals[2]))
	case wasm.OpDataDrop:
		f.emit("%s = nil", c.dataRef(in.A))
	case wasm.OpMemoryCopy:
		vals := f.popN(3)
		f.emit("rt.MemCopy(%s, %s, %s, %s, %s)", c.memRef(in.A), f.u64(vals[0]), c.memRef(in.B), f.u64(vals[1]), f.u64(vals[2]))
	case wasm.OpMemoryFill:
		vals := f.popN(3)
		f.emit("%s.Fill(%s, %s, %s)", c.memRef(in.A), f.u64(vals[0]), f.u8(vals[1]), f.u64(vals[2]))
	default:
		return false
	}
	return true
}

// --- calls ---

func (f *fn) emitCall(callee string, ft *wasm.FuncType, args []string, tail bool) {
	call := fmt.Sprintf("%s(%s)", callee, strings.Join(append([]string{"d + 1"}, args...), ", "))
	if tail && f.closure > 0 && sameTypes(f.c, ft.Results, f.results) {
		// Inside a try closure: evaluate the callee and arguments now, then
		// leave all handlers and perform the call at function level.
		f.ntail++
		fv := fmt.Sprintf("tc%d", f.ntail)
		f.declare(fv, f.goFuncType(ft))
		f.emit("%s = %s", fv, callee)
		var avs []string
		for i, p := range ft.Params {
			av := fmt.Sprintf("tc%d_%d", f.ntail, i)
			f.declare(av, f.c.goType(p))
			f.emit("%s = %s", av, args[i])
			avs = append(avs, av)
		}
		tcall := fmt.Sprintf("%s(%s)", fv, strings.Join(append([]string{"d"}, avs...), ", "))
		if len(ft.Results) > 0 {
			tcall = "return " + tcall
		} else {
			tcall += "\nreturn"
		}
		f.jump(&ctrl{kind: ctrlTail, closure: -1, tailCall: tcall})
		f.setUnreachable()
		return
	}
	if tail {
		call = fmt.Sprintf("%s(%s)", callee, strings.Join(append([]string{"d"}, args...), ", "))
		if f.closure == 0 && sameTypes(f.c, ft.Results, f.results) {
			if len(ft.Results) == 0 {
				f.emit("%s", call)
				f.emit("return")
			} else {
				f.emit("return %s", call)
			}
			f.setUnreachable()
			return
		}
	}
	h := f.height()
	var outs []string
	for i, r := range ft.Results {
		outs = append(outs, f.slot(h+i, f.c.goType(r)))
	}
	if len(outs) == 0 {
		f.emit("%s", call)
	} else {
		f.emit("%s = %s", strings.Join(outs, ", "), call)
	}
	for i, r := range ft.Results {
		f.pushSlotVar(r, outs[i])
	}
	if tail {
		f.branch(uint32(len(f.ctrls) - 1))
		f.setUnreachable()
	}
}

func sameTypes(c *compiler, a, b []wasm.ValType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if c.goType(a[i]) != c.goType(b[i]) {
			return false
		}
	}
	return true
}

func (f *fn) args(ft *wasm.FuncType) []string {
	vals := f.popN(len(ft.Params))
	out := make([]string, len(vals))
	for i, p := range ft.Params {
		out[i] = f.c.conv(f.ex(vals[i]), vals[i].t, p)
	}
	return out
}

func (f *fn) call(fi uint32, tail bool) {
	c := f.c
	ft := c.m.FuncType(fi)
	if tail && int(fi) == f.idx && f.closure == 0 {
		// self tail call: rebind parameters and loop
		args := f.args(ft)
		if len(args) > 0 {
			var ps []string
			for i := range ft.Params {
				ps = append(ps, f.lname[i])
				f.read[f.lname[i]] = true
			}
			f.emit("%s = %s", strings.Join(ps, ", "), strings.Join(args, ", "))
		}
		// reset declared locals to zero
		for i := len(ft.Params); i < len(f.locals); i++ {
			f.emit("%s = %s", f.lname[i], c.zeroOf(f.locals[i]))
		}
		f.startUsed = true
		f.emit("goto %s", f.startLabel)
		f.setUnreachable()
		return
	}
	if b := c.builtin[fi]; (b == "charCodeAt" || b == "length") && !tail {
		f.inlineJSString(b)
		return
	}
	args := f.args(ft)
	f.emitCall("m."+c.funcName(fi), ft, args, tail)
}

// inlineJSString emits the JSCache fast path for charCodeAt/length directly
// at the call site: a pointer compare and a byte load for ASCII strings.
func (f *fn) inlineJSString(name string) {
	var idx entry
	if name == "charCodeAt" {
		idx = f.simpleTop()
		f.pop()
	}
	s := f.simpleTop()
	f.pop()
	h := f.height()
	out := f.slot(h, "int32")
	if name == "charCodeAt" {
		i := f.u32(idx)
		f.emit("if rt.EfaceKey(%s) == m.jsCache.Key && %s < uint32(len(m.jsCache.ASCII)) {", f.ex(s), i)
		f.emit("%s = int32(m.jsCache.ASCII[%s])", out, i)
		f.emit("} else {")
		f.emit("%s = m.jsCache.CharCodeAtSlow(%s, %s)", out, f.ex(s), f.ex(idx))
		f.emit("}")
	} else {
		f.emit("if rt.EfaceKey(%s) == m.jsCache.Key {", f.ex(s))
		f.emit("%s = int32(len(m.jsCache.ASCII))", out)
		f.emit("} else {")
		f.emit("%s = m.jsCache.LengthSlow(%s)", out, f.ex(s))
		f.emit("}")
	}
	f.pushSlotVar(ti32, out)
}

func (f *fn) goFuncType(ft *wasm.FuncType) string {
	var ps, rs []string
	ps = append(ps, "int")
	for _, p := range ft.Params {
		ps = append(ps, f.c.goType(p))
	}
	for _, r := range ft.Results {
		rs = append(rs, f.c.goType(r))
	}
	s := "func(" + strings.Join(ps, ", ") + ")"
	switch len(rs) {
	case 0:
	case 1:
		s += " " + rs[0]
	default:
		s += " (" + strings.Join(rs, ", ") + ")"
	}
	return s
}

func (f *fn) callIndirect(typeIdx, table uint32, tail bool) {
	c := f.c
	ft := &c.m.Types[typeIdx].Comp.Func
	idx := f.pop()
	args := f.args(ft)
	var ie string
	if c.m.TableType(table).Limits.Is64 {
		ie = f.u64(idx)
	} else {
		ie = f.u64(idx)
	}
	callee := fmt.Sprintf("rt.FnOf(%s.Get(%s), %s).(%s)", c.tableRef(table), ie, c.info(typeIdx).rtt, f.goFuncType(ft))
	f.emitCall(callee, ft, args, tail)
}

func (f *fn) callRef(typeIdx uint32, tail bool) {
	c := f.c
	ft := &c.m.Types[typeIdx].Comp.Func
	r := f.pop()
	args := f.args(ft)
	callee := fmt.Sprintf("rt.FnOfRef(%s, %s).(%s)", f.ex(r), c.info(typeIdx).rtt, f.goFuncType(ft))
	f.emitCall(callee, ft, args, tail)
}

// floatConstVar returns a package-level variable holding a float constant.
func (c *compiler) floatConstVar(bits uint64, is64 bool) string {
	key := fmt.Sprintf("%v:%x", is64, bits)
	if v, ok := c.fconsts[key]; ok {
		return v
	}
	v := c.pkgIdent(fmt.Sprintf("fconst%d", len(c.fconsts)))
	c.fconsts[key] = v
	if is64 {
		fmt.Fprintf(&c.extra, "var %s = math.Float64frombits(0x%016x)\n\n", v, bits)
	} else {
		fmt.Fprintf(&c.extra, "var %s = math.Float32frombits(0x%08x)\n\n", v, uint32(bits))
	}
	return v
}
