package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gosm/internal/wasm"
)

func leb(n int) []byte {
	var out []byte
	for {
		b := byte(n & 0x7F)
		n >>= 7
		if n != 0 {
			out = append(out, b|0x80)
		} else {
			return append(out, b)
		}
	}
}

func section(id byte, payload ...byte) []byte {
	return append(append([]byte{id}, leb(len(payload))...), payload...)
}

func vec(items ...[]byte) []byte {
	out := leb(len(items))
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

func module(sections ...[]byte) []byte {
	out := []byte("\x00asm\x01\x00\x00\x00")
	for _, s := range sections {
		out = append(out, s...)
	}
	return out
}

// oneFunc builds a module with one function of type params->results whose
// body (without the final end) is code, with the given local declarations.
func oneFunc(params, results []byte, locals [][]byte, code ...byte) []byte {
	ft := append([]byte{0x60}, append(append(leb(len(params)), params...), append(leb(len(results)), results...)...)...)
	body := append(vec(locals...), code...)
	body = append(body, 0x0B)
	return module(
		section(1, vec(ft)...),
		section(3, vec([]byte{0})...),
		section(10, vec(append(leb(len(body)), body...))...),
	)
}

func check(t *testing.T, name string, b []byte, wantErr string) {
	t.Helper()
	m, err := wasm.Decode(b)
	if err != nil {
		t.Fatalf("%s: decode: %v", name, err)
	}
	err = Validate(m)
	switch {
	case wantErr == "" && err != nil:
		t.Errorf("%s: unexpected error: %v", name, err)
	case wantErr != "" && err == nil:
		t.Errorf("%s: expected error containing %q", name, wantErr)
	case wantErr != "" && !strings.Contains(err.Error(), wantErr):
		t.Errorf("%s: error %q does not contain %q", name, err, wantErr)
	}
}

func TestInstructions(t *testing.T) {
	i32 := []byte{0x7F}
	cases := []struct {
		name    string
		params  []byte
		results []byte
		locals  [][]byte
		code    []byte
		err     string
	}{
		{"const", nil, i32, nil, []byte{0x41, 1}, ""},
		{"wrong result", nil, i32, nil, []byte{0x42, 1}, "type mismatch"},
		{"extra value", nil, nil, nil, []byte{0x41, 1}, "type mismatch"},
		{"empty stack", nil, i32, nil, []byte{0x6A}, "type mismatch"},
		{"unreachable polymorphic", nil, i32, nil, []byte{0x00, 0x6A}, ""},
		{"br polymorphic", nil, i32, nil, []byte{0x41, 1, 0x0C, 0, 0x6A}, ""},
		{"br needs value", nil, i32, nil, []byte{0x0C, 0}, "type mismatch"},
		{"unknown label", nil, nil, nil, []byte{0x0C, 1}, "unknown label"},
		{"select numeric", nil, i32, nil, []byte{0x41, 1, 0x41, 2, 0x41, 0, 0x1B}, ""},
		{"select ref needs type", nil, nil, nil, []byte{0xD0, 0x70, 0xD0, 0x70, 0x41, 0, 0x1B, 0x1A}, "type mismatch"},
		{"select bot", nil, i32, nil, []byte{0x00, 0x1B}, ""},
		{"ref.as_non_null bot", nil, nil, nil, []byte{0x00, 0xD4, 0x1A}, ""},
		{"uninitialized local", nil, nil, [][]byte{{1, 0x64, 0x70}}, []byte{0x20, 0, 0x1A}, "uninitialized local"},
		{"local init in block", nil, nil, [][]byte{{1, 0x64, 0x70}},
			// block; ref.func 0 ... (undeclared) -> use ref.null+as_non_null instead
			[]byte{0x02, 0x40, 0xD0, 0x70, 0xD4, 0x21, 0, 0x20, 0, 0x1A, 0x0B}, ""},
		{"local init expires", nil, nil, [][]byte{{1, 0x64, 0x70}},
			[]byte{0x02, 0x40, 0xD0, 0x70, 0xD4, 0x21, 0, 0x0B, 0x20, 0, 0x1A}, "uninitialized local"},
		{"undeclared ref.func", nil, nil, nil, []byte{0xD2, 0, 0x1A}, "undeclared function reference"},
		{"no memory", nil, nil, nil, []byte{0x41, 0, 0x28, 2, 0, 0x1A}, "unknown memory"},
		{"if without else arity", nil, i32, nil, []byte{0x41, 0, 0x04, 0x7F, 0x41, 1, 0x0B}, "type mismatch"},
		{"br_table arity", nil, nil, nil, []byte{0x02, 0x7F, 0x41, 0, 0x41, 0, 0x0E, 1, 0, 1, 0x0B, 0x1A}, "arity"},
	}
	for _, c := range cases {
		check(t, c.name, oneFunc(c.params, c.results, c.locals, c.code...), c.err)
	}
}

func TestModule(t *testing.T) {
	// duplicate export names
	ft := []byte{0x60, 0, 0}
	code := vec([]byte{2, 0, 0x0B})
	check(t, "dup export", module(
		section(1, vec(ft)...),
		section(3, vec([]byte{0})...),
		section(7, vec(append([]byte{1, 'a', 0}, 0), append([]byte{1, 'a', 0}, 0))...),
		section(10, code...),
	), "duplicate export")
	// start function with params
	check(t, "start type", module(
		section(1, vec([]byte{0x60, 1, 0x7F, 0})...),
		section(3, vec([]byte{0})...),
		section(8, 0),
		section(10, vec([]byte{2, 0, 0x0B})...),
	), "start function")
	// memory too large
	check(t, "memory size", module(section(5, vec([]byte{0, 0x81, 0x80, 0x04})...)), "memory size")
	// subtype of a final type
	check(t, "final super", module(section(1, vec(
		[]byte{0x5F, 0},             // type 0: final struct
		[]byte{0x50, 1, 0, 0x5F, 0}, // type 1: sub 0 struct
	)...)), "sub type")
	// subtype of an open type is fine; field must match
	check(t, "open super", module(section(1, vec(
		[]byte{0x50, 0, 0x5F, 1, 0x7F, 0},
		[]byte{0x50, 1, 0, 0x5F, 2, 0x7F, 0, 0x7E, 1},
	)...)), "")
	check(t, "bad field", module(section(1, vec(
		[]byte{0x50, 0, 0x5F, 1, 0x7F, 0},
		[]byte{0x50, 1, 0, 0x5F, 1, 0x7E, 0},
	)...)), "sub type")
	// tag with results
	check(t, "tag results", module(
		section(1, vec([]byte{0x60, 0, 1, 0x7F})...),
		section(13, vec([]byte{0, 0})...),
	), "non-empty tag result type")
	// iso-recursive equivalence: two identical rec groups define equal types,
	// so a call_ref through either one type-checks.
	check(t, "iso-recursive", module(
		section(1, vec(
			[]byte{0x4E, 1, 0x60, 1, 0x63, 0, 0}, // rec (func (param (ref null 0)))
			[]byte{0x4E, 1, 0x60, 1, 0x63, 1, 0}, // rec (func (param (ref null 1)))
			[]byte{0x60, 1, 0x63, 0, 0},          // type 2: (func (param (ref null 0)))
		)...),
		section(3, vec([]byte{2})...),
		// local.get 0 is (ref null 0) == (ref null 1): pass it as (ref null 1)'s param
		section(10, vec(append(leb(8), 0, 0x20, 0, 0x20, 0, 0x14, 1, 0x0B))...),
	), "")
}

// FuzzValidate checks that Decode+Validate never panic.
func FuzzValidate(f *testing.F) {
	f.Add(oneFunc(nil, []byte{0x7F}, nil, 0x41, 1))
	if dir := os.Getenv("GOSM_SPECTEST_JSON"); dir != "" {
		files, _ := filepath.Glob(filepath.Join(dir, "*", "*.wasm"))
		for i, p := range files {
			if i%7 != 0 {
				continue
			}
			if b, err := os.ReadFile(p); err == nil && len(b) < 4096 {
				f.Add(b)
			}
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := wasm.Decode(b)
		if err == nil {
			_ = Validate(m)
		}
	})
}
