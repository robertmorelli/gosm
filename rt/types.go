package rt

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// Type kinds.
const (
	KFunc   byte = 0x60
	KStruct byte = 0x5F
	KArray  byte = 0x5E
)

// V128 is a 128-bit SIMD value (little-endian lanes: Lo holds bytes 0..7).
type V128 struct {
	Lo, Hi uint64
}

// RTT is the runtime type of a defined (concrete) wasm type. Subtyping is
// checked in O(1) via the supertype chain.
type RTT struct {
	Kind   byte
	Final  bool
	Key    string
	Supers []*RTT // root first, ending with this type
	// Box converts a pointer to an object whose dynamic type is this RTT
	// into an interface holding its exact Go type (struct/array types with
	// headers only).
	Box func(unsafe.Pointer) any
}

func (t *RTT) Depth() int { return len(t.Supers) - 1 }

// SubOf reports whether t is a subtype of s.
func (t *RTT) SubOf(s *RTT) bool {
	if t == s {
		return true
	}
	d := len(s.Supers) - 1
	return d < len(t.Supers) && t.Supers[d] == s
}

var byKey sync.Map // key -> *RTT (first registered), for link-time checks

func newRTT(kind byte, super *RTT, final bool, key string) *RTT {
	t := &RTT{Kind: kind, Final: final, Key: key}
	byKey.LoadOrStore(key, t)
	if super != nil {
		t.Supers = append(append([]*RTT{}, super.Supers...), t)
	} else {
		t.Supers = []*RTT{t}
	}
	return t
}

// NewRTT creates a package-local runtime type (used for struct and array
// types, whose Go representation is specific to one generated package).
func NewRTT(kind byte, super *RTT, final bool, key string, box func(unsafe.Pointer) any) *RTT {
	t := newRTT(kind, super, final, key)
	t.Box = box
	return t
}

var (
	canonMu sync.Mutex
	canon   = map[string]*RTT{}
)

// CanonRTT returns the process-wide canonical runtime type for key, creating
// it if necessary. Function types are canonicalized this way so that
// call_indirect type checks work across separately generated modules.
func CanonRTT(kind byte, super *RTT, final bool, key string) *RTT {
	canonMu.Lock()
	defer canonMu.Unlock()
	if t, ok := canon[key]; ok {
		return t
	}
	t := newRTT(kind, super, final, key)
	canon[key] = t
	return t
}

// Type keys. A key identifies a type up to iso-recursive equivalence.

// GroupKey hashes the serialization of a rec group.
func GroupKey(subtypes []string) string {
	h := sha256.Sum256([]byte(strings.Join(subtypes, ";")))
	return "g" + hex.EncodeToString(h[:12])
}

func TypeKey(group string, idx int) string { return group + "." + strconv.Itoa(idx) }

// SubtypeSer serializes one subtype; super is "" or a type reference.
func SubtypeSer(final bool, super string, comp string) string {
	f := "open"
	if final {
		f = "final"
	}
	return "sub " + f + " (" + super + ") " + comp
}

func FuncSer(params, results []string) string {
	return "func(" + strings.Join(params, ",") + ")->(" + strings.Join(results, ",") + ")"
}

// FuncTypeKey is the key of a standalone final function type whose value
// types are serialized as by the compiler ("i32", "ref null func", ...).
func FuncTypeKey(params, results []string) string {
	return TypeKey(GroupKey([]string{SubtypeSer(true, "", FuncSer(params, results))}), 0)
}

// FuncType returns the canonical RTT of a standalone final function type.
func FuncType(params, results []string) *RTT {
	return CanonRTT(KFunc, nil, true, FuncTypeKey(params, results))
}

// Func is a function reference. Fn holds a Go func whose signature is
// func(depth int, params...) results; hosts pass depth 0.
type Func struct {
	Type *RTT
	Fn   any
	Name string
	// Views adapts Fn to supertypes whose Go signature differs (because a
	// subtype refines parameter or result reference types).
	Views []FuncView
}

// FuncView is Fn adapted to the signature of a supertype.
type FuncView struct {
	Type *RTT
	Fn   any
}

// As returns the Go function to call f through static type t.
func (f *Func) As(t *RTT) any {
	if f.Type == t || f.Views == nil {
		return f.Fn
	}
	for _, v := range f.Views {
		if v.Type == t {
			return v.Fn
		}
	}
	return f.Fn
}

// FnOf checks a table entry for call_indirect through type t and returns
// its Go function.
func FnOf(f *Func, t *RTT) any {
	if f == nil {
		panic(ErrUninitElem)
	}
	if f.Type != t {
		if !f.Type.SubOf(t) {
			panic(ErrIndirectType)
		}
		return f.As(t)
	}
	return f.Fn
}

// FnOfRef returns the Go function of a non-null function reference called
// through type t.
func FnOfRef(f *Func, t *RTT) any {
	if f == nil {
		panic(ErrNullFunc)
	}
	if f.Type != t {
		return f.As(t)
	}
	return f.Fn
}

// GCRef is implemented by every generated struct and array type.
type GCRef interface {
	WasmType() *RTT
}

// Object is the header embedded (first) in generated struct and array types
// that take part in a subtype hierarchy. It records the dynamic type.
type Object struct {
	RTT *RTT
}

func (o *Object) WasmType() *RTT      { return o.RTT }
func (o *Object) WasmHeader() *Object { return o }

type headered interface {
	WasmHeader() *Object
}

// I31 is an unboxed 31-bit integer reference (i31ref).
type I31 uint32

func MakeI31(x int32) any { return I31(uint32(x) & 0x7FFFFFFF) }

func I31GetS(x any) int32 {
	v, ok := x.(I31)
	if !ok {
		if x == nil {
			panic(ErrNullI31)
		}
		panic(ErrCast)
	}
	return int32(uint32(v)<<1) >> 1
}

func I31GetU(x any) int32 {
	v, ok := x.(I31)
	if !ok {
		if x == nil {
			panic(ErrNullI31)
		}
		panic(ErrCast)
	}
	return int32(v)
}

// Box converts a pointer to a header-carrying object into an interface
// holding its exact dynamic Go type. nil maps to a nil interface.
func Box[T any](p *T) any {
	if p == nil {
		return nil
	}
	h := (*Object)(unsafe.Pointer(p))
	return h.RTT.Box(unsafe.Pointer(p))
}

// AnyOf converts a pointer to an interface, mapping nil to a nil interface.
func AnyOf[T any](p *T) any {
	if p == nil {
		return nil
	}
	return p
}

// As converts an interface known to hold *T (or nil) back to *T.
func As[T any](x any) *T {
	if x == nil {
		return nil
	}
	return x.(*T)
}

// AsObj converts an interface known to hold a header-carrying object whose
// type is a subtype of T into *T.
func AsObj[T any](x any) *T {
	if x == nil {
		return nil
	}
	if p, ok := x.(*T); ok {
		return p
	}
	return (*T)(unsafe.Pointer(x.(headered).WasmHeader()))
}

// ViewAs reinterprets a pointer to a header-carrying object as a pointer to
// one of its super- or subtypes (validated statically or by a prior test).
func ViewAs[T, S any](p *S) *T { return (*T)(unsafe.Pointer(p)) }

// ObjRTT returns the dynamic type of a header-carrying object.
func ObjRTT[T any](p *T) *RTT { return (*Object)(unsafe.Pointer(p)).RTT }

// Abstract heap type tests on interface-typed references.

func IsEq(x any) bool {
	switch x.(type) {
	case I31:
		return true
	case GCRef:
		return true
	}
	return false
}

func IsI31(x any) bool { _, ok := x.(I31); return ok }

func IsStruct(x any) bool {
	o, ok := x.(GCRef)
	return ok && o.WasmType().Kind == KStruct
}

func IsArray(x any) bool {
	o, ok := x.(GCRef)
	return ok && o.WasmType().Kind == KArray
}

// IsType reports whether x is a struct or array whose type is a subtype of t.
func IsType(x any, t *RTT) bool {
	o, ok := x.(GCRef)
	return ok && o.WasmType().SubOf(t)
}

// IsFuncType reports whether f is non-null with a type that is a subtype of t.
func IsFuncType(f *Func, t *RTT) bool {
	return f != nil && f.Type.SubOf(t)
}

// HostRef is a convenience wrapper for opaque host references.
type HostRef struct{ V any }
