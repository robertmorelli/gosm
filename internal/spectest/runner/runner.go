// Package runner executes WebAssembly spec test scripts (converted to JSON
// by wasm-tools json-from-wast) against modules compiled by gosm.
package runner

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"

	"gosm/rt"
)

// Ctor instantiates a compiled module, resolving imports via resolve.
type Ctor func(resolve func(module, name string) any) (map[string]any, error)

type value struct {
	Type     string          `json:"type"`
	LaneType string          `json:"lane_type"`
	Value    json.RawMessage `json:"value"`
	Values   []value         `json:"values"`
}

type action struct {
	Type   string  `json:"type"`
	Module string  `json:"module"`
	Field  string  `json:"field"`
	Args   []value `json:"args"`
}

type command struct {
	Type     string  `json:"type"`
	Line     int     `json:"line"`
	Name     string  `json:"name"`
	Filename string  `json:"filename"`
	As       string  `json:"as"`
	Instance string  `json:"instance"`
	Module   string  `json:"module"`
	Action   action  `json:"action"`
	Expected []value `json:"expected"`
	Text     string  `json:"text"`
}

type script struct {
	Commands []command `json:"commands"`
}

// HostRef is the host value produced by (ref.extern n) / (ref.host n).
type HostRef uint32

type Result struct {
	Passed, Failed, Skipped int
	Failures                []string
}

type state struct {
	ctors      map[string]Ctor
	cur        map[string]any
	named      map[string]map[string]any
	registered map[string]map[string]any
	defs       map[string]string
	res        Result
	spectest   map[string]any
}

// Run executes the script at path.
func Run(path string, ctors map[string]Ctor) Result {
	b, err := os.ReadFile(path)
	if err != nil {
		return Result{Failed: 1, Failures: []string{err.Error()}}
	}
	var s script
	if err := json.Unmarshal(b, &s); err != nil {
		return Result{Failed: 1, Failures: []string{err.Error()}}
	}
	st := &state{
		ctors:      ctors,
		named:      map[string]map[string]any{},
		registered: map[string]map[string]any{},
		defs:       map[string]string{},
	}
	st.spectest = spectest()
	for _, c := range s.Commands {
		st.run(c)
	}
	return st.res
}

func (s *state) fail(c command, format string, args ...any) {
	s.res.Failed++
	s.res.Failures = append(s.res.Failures, fmt.Sprintf("line %d (%s): %s", c.Line, c.Type, fmt.Sprintf(format, args...)))
}

func (s *state) resolve(module, name string) any {
	if module == "spectest" {
		return s.spectest[name]
	}
	if m, ok := s.registered[module]; ok {
		return m[name]
	}
	return nil
}

func (s *state) instantiate(file string) (map[string]any, error) {
	ctor, ok := s.ctors[file]
	if !ok {
		return nil, fmt.Errorf("module %s was not compiled", file)
	}
	var ex map[string]any
	err := rt.Catch(func() {
		var err error
		ex, err = ctor(s.resolve)
		if err != nil {
			panic(err)
		}
	})
	if err == nil {
		return ex, nil
	}
	if e, ok := err.(interface{ Unwrap() error }); ok && e.Unwrap() != nil {
		return nil, e.Unwrap()
	}
	return nil, err
}

func (s *state) run(c command) {
	defer func() {
		if r := recover(); r != nil {
			s.fail(c, "harness panic: %v", r)
		}
	}()
	switch c.Type {
	case "module":
		ex, err := s.instantiate(c.Filename)
		if err != nil {
			s.cur = nil
			s.fail(c, "instantiate %s: %v", c.Filename, err)
			return
		}
		s.cur = ex
		if c.Name != "" {
			s.named[c.Name] = ex
		}
		s.res.Passed++
	case "module_definition":
		if c.Name != "" {
			s.defs[c.Name] = c.Filename
		}
		s.res.Skipped++
	case "module_instance":
		ex, err := s.instantiate(s.defs[c.Module])
		if err != nil {
			s.fail(c, "instantiate %s: %v", c.Module, err)
			return
		}
		s.named[c.Instance] = ex
		s.cur = ex
		s.res.Passed++
	case "register":
		m := s.cur
		if c.Name != "" {
			m = s.named[c.Name]
		}
		s.registered[c.As] = m
	case "action":
		_, err := s.act(c.Action)
		if err != nil {
			s.fail(c, "%v", err)
			return
		}
		s.res.Passed++
	case "assert_return":
		got, err := s.act(c.Action)
		if err != nil {
			s.fail(c, "%s: %v", c.Action.Field, err)
			return
		}
		if len(got) != len(c.Expected) {
			s.fail(c, "%s: got %d results, want %d", c.Action.Field, len(got), len(c.Expected))
			return
		}
		for i, e := range c.Expected {
			if !match(got[i], e) {
				s.fail(c, "%s: result %d: got %s, want %s", c.Action.Field, i, show(got[i]), showExp(e))
				return
			}
		}
		s.res.Passed++
	case "assert_trap", "assert_exhaustion":
		if c.Filename != "" {
			_, err := s.instantiate(c.Filename)
			if err == nil {
				s.fail(c, "expected instantiation trap %q", c.Text)
				return
			}
			s.res.Passed++
			return
		}
		_, err := s.act(c.Action)
		if _, ok := err.(*rt.Trap); !ok {
			s.fail(c, "%s: expected trap %q, got %v", c.Action.Field, c.Text, err)
			return
		}
		s.res.Passed++
	case "assert_exception":
		_, err := s.act(c.Action)
		if _, ok := err.(*rt.Exception); !ok {
			s.fail(c, "%s: expected exception, got %v", c.Action.Field, err)
			return
		}
		s.res.Passed++
	case "assert_uninstantiable", "assert_unlinkable":
		if _, err := s.instantiate(c.Filename); err == nil {
			s.fail(c, "expected failure %q", c.Text)
			return
		}
		s.res.Passed++
	case "assert_invalid", "assert_malformed":
		s.res.Skipped++
	default:
		s.res.Skipped++
	}
}

func (s *state) act(a action) (results []any, err error) {
	m := s.cur
	if a.Module != "" {
		m = s.named[a.Module]
	}
	if m == nil {
		return nil, fmt.Errorf("no module")
	}
	x, ok := m[a.Field]
	if !ok {
		return nil, fmt.Errorf("no export %q", a.Field)
	}
	if a.Type == "get" {
		v := reflect.ValueOf(x).Elem().FieldByName("V")
		return []any{v.Interface()}, nil
	}
	f, ok := x.(*rt.Func)
	if !ok {
		return nil, fmt.Errorf("export %q is not a function", a.Field)
	}
	fv := reflect.ValueOf(f.Fn)
	ft := fv.Type()
	if ft.NumIn() != len(a.Args)+1 {
		return nil, fmt.Errorf("arity mismatch")
	}
	in := []reflect.Value{reflect.ValueOf(0)}
	for i, arg := range a.Args {
		v, err := parseArg(arg)
		if err != nil {
			return nil, err
		}
		pt := ft.In(i + 1)
		if v == nil {
			in = append(in, reflect.Zero(pt))
		} else {
			in = append(in, reflect.ValueOf(v).Convert(pt))
		}
	}
	err = rt.Catch(func() {
		out := fv.Call(in)
		for _, o := range out {
			results = append(results, o.Interface())
		}
	})
	return results, err
}

func rawString(r json.RawMessage) string {
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	return string(r)
}

func parseArg(v value) (any, error) {
	s := rawString(v.Value)
	switch v.Type {
	case "i32":
		n, err := parseBits(s, 32)
		return int32(uint32(n)), err
	case "i64":
		n, err := parseBits(s, 64)
		return int64(n), err
	case "f32":
		n, err := parseBits(s, 32)
		return math.Float32frombits(uint32(n)), err
	case "f64":
		n, err := parseBits(s, 64)
		return math.Float64frombits(n), err
	case "v128":
		return parseV128(v)
	case "externref", "anyref":
		if s == "null" {
			return nil, nil
		}
		n, err := parseBits(s, 32)
		return any(HostRef(n)), err
	case "funcref", "exnref", "nullfuncref", "nullref", "nullexternref":
		return nil, nil
	}
	return nil, fmt.Errorf("unsupported arg type %s", v.Type)
}

func parseV128(v value) (rt.V128, error) {
	var lanes []string
	if err := json.Unmarshal(v.Value, &lanes); err != nil {
		return rt.V128{}, err
	}
	var b [16]byte
	size := map[string]int{"i8": 1, "i16": 2, "i32": 4, "i64": 8, "f32": 4, "f64": 8}[v.LaneType]
	for i, l := range lanes {
		var n uint64
		if strings.HasPrefix(l, "nan") {
			if size == 4 {
				n = 0x7FC00000
			} else {
				n = 0x7FF8000000000000
			}
		} else {
			var err error
			n, err = strconv.ParseUint(l, 10, 64)
			if err != nil {
				x, err2 := strconv.ParseInt(l, 10, 64)
				if err2 != nil {
					return rt.V128{}, err
				}
				n = uint64(x)
			}
		}
		for k := 0; k < size; k++ {
			b[i*size+k] = byte(n >> (8 * k))
		}
	}
	var r rt.V128
	for k := 0; k < 8; k++ {
		r.Lo |= uint64(b[k]) << (8 * k)
		r.Hi |= uint64(b[8+k]) << (8 * k)
	}
	return r, nil
}

func isNull(x any) bool {
	if x == nil {
		return true
	}
	v := reflect.ValueOf(x)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func match(got any, e value) bool {
	if e.Type == "either" {
		for _, alt := range e.Values {
			if match(got, alt) {
				return true
			}
		}
		return false
	}
	s := rawString(e.Value)
	switch e.Type {
	case "i32":
		n, _ := parseBits(s, 32)
		g, ok := got.(int32)
		return ok && uint32(g) == uint32(n)
	case "i64":
		n, _ := parseBits(s, 64)
		g, ok := got.(int64)
		return ok && uint64(g) == n
	case "f32":
		g, ok := got.(float32)
		if !ok {
			return false
		}
		bits := math.Float32bits(g)
		switch s {
		case "nan:canonical":
			return bits&0x7FFFFFFF == 0x7FC00000
		case "nan:arithmetic":
			return bits&0x7FC00000 == 0x7FC00000
		}
		n, _ := parseBits(s, 32)
		return bits == uint32(n)
	case "f64":
		g, ok := got.(float64)
		if !ok {
			return false
		}
		bits := math.Float64bits(g)
		switch s {
		case "nan:canonical":
			return bits&0x7FFFFFFFFFFFFFFF == 0x7FF8000000000000
		case "nan:arithmetic":
			return bits&0x7FF8000000000000 == 0x7FF8000000000000
		}
		n, _ := parseBits(s, 64)
		return bits == n
	case "v128":
		g, ok := got.(rt.V128)
		if !ok {
			return false
		}
		return matchV128(g, e)
	}
	// references
	if s == "null" {
		return isNull(got)
	}
	switch e.Type {
	case "refnull", "nullref", "nullfuncref", "nullexternref", "nullexnref":
		return isNull(got)
	case "externref", "anyref":
		if len(e.Value) == 0 {
			return !isNull(got)
		}
		n, _ := parseBits(s, 32)
		h, ok := got.(HostRef)
		return ok && uint32(h) == uint32(n)
	case "funcref":
		f, ok := got.(*rt.Func)
		return ok && f != nil
	case "exnref":
		x, ok := got.(*rt.Exception)
		return ok && x != nil
	case "eqref":
		return !isNull(got) && rt.IsEq(got)
	case "i31ref":
		return rt.IsI31(got)
	case "structref":
		return !isNull(got) && rt.IsStruct(got)
	case "arrayref":
		return !isNull(got) && rt.IsArray(got)
	}
	return false
}

func matchV128(g rt.V128, e value) bool {
	var lanes []string
	if err := json.Unmarshal(e.Value, &lanes); err != nil {
		return false
	}
	var b [16]byte
	for k := 0; k < 8; k++ {
		b[k] = byte(g.Lo >> (8 * k))
		b[8+k] = byte(g.Hi >> (8 * k))
	}
	size := map[string]int{"i8": 1, "i16": 2, "i32": 4, "i64": 8, "f32": 4, "f64": 8}[e.LaneType]
	for i, l := range lanes {
		var lane uint64
		for k := 0; k < size; k++ {
			lane |= uint64(b[i*size+k]) << (8 * k)
		}
		switch {
		case l == "nan:canonical" && size == 4:
			if uint32(lane)&0x7FFFFFFF != 0x7FC00000 {
				return false
			}
		case l == "nan:arithmetic" && size == 4:
			if uint32(lane)&0x7FC00000 != 0x7FC00000 {
				return false
			}
		case l == "nan:canonical":
			if lane&0x7FFFFFFFFFFFFFFF != 0x7FF8000000000000 {
				return false
			}
		case l == "nan:arithmetic":
			if lane&0x7FF8000000000000 != 0x7FF8000000000000 {
				return false
			}
		default:
			n, err := strconv.ParseUint(l, 10, 64)
			if err != nil {
				x, _ := strconv.ParseInt(l, 10, 64)
				n = uint64(x)
			}
			if size < 8 {
				n &= 1<<(8*size) - 1
			}
			if lane != n {
				return false
			}
		}
	}
	return true
}

func show(x any) string {
	switch v := x.(type) {
	case float32:
		return fmt.Sprintf("f32:%v(0x%08x)", v, math.Float32bits(v))
	case float64:
		return fmt.Sprintf("f64:%v(0x%016x)", v, math.Float64bits(v))
	case rt.V128:
		return fmt.Sprintf("v128:%016x_%016x", v.Hi, v.Lo)
	}
	return fmt.Sprintf("%T:%v", x, x)
}

func showExp(e value) string {
	if e.Type == "either" {
		var s []string
		for _, v := range e.Values {
			s = append(s, showExp(v))
		}
		return "either(" + strings.Join(s, " | ") + ")"
	}
	return e.Type + ":" + string(e.Value)
}

func spectest() map[string]any {
	print0 := func(params ...string) *rt.Func {
		return rt.NewHostFunc(params, nil)
	}
	mem := rt.NewMemory(1, 2, true, false)
	tab := rt.NewTable[*rt.Func](10, 20, true, false, "ref null func", nil)
	tab64 := rt.NewTable[*rt.Func](10, 20, true, true, "ref null func", nil)
	return map[string]any{
		"print":         print0(),
		"print_i32":     print0("i32"),
		"print_i64":     print0("i64"),
		"print_f32":     print0("f32"),
		"print_f64":     print0("f64"),
		"print_i32_f32": print0("i32", "f32"),
		"print_f64_f64": print0("f64", "f64"),
		"global_i32":    &rt.Global[int32]{V: 666, Type: "i32"},
		"global_i64":    &rt.Global[int64]{V: 666, Type: "i64"},
		"global_f32":    &rt.Global[float32]{V: 666.6, Type: "f32"},
		"global_f64":    &rt.Global[float64]{V: 666.6, Type: "f64"},
		"table":         tab,
		"table64":       tab64,
		"memory":        mem,
	}
}

// parseBits parses a signed or unsigned decimal integer into its bit pattern.
func parseBits(s string, size int) (uint64, error) {
	if strings.HasPrefix(s, "-") {
		n, err := strconv.ParseInt(s, 10, size)
		return uint64(n), err
	}
	return strconv.ParseUint(s, 10, size)
}
