package rt

// JS String Builtins (wasm:js-string, wasm:text-encoder, wasm:text-decoder)
// over native Go strings.
//
// Strings are Go strings holding WTF-8: UTF-8 where unpaired UTF-16
// surrogates are encoded as 3-byte sequences (ED A0..BF xx). All indices and
// lengths are in UTF-16 code units, as in JavaScript, so length("é") == 1
// and length("😀") == 2. ASCII strings take fast paths.

import (
	"strings"
	"sync/atomic"
	"unicode/utf8"
	"unsafe"
)

func jsStr(x any) string {
	s, ok := x.(string)
	if !ok {
		panic(ErrNotString)
	}
	return s
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// decodeWTF8 decodes one code point (surrogates allowed) at s[i:].
func decodeWTF8(s string, i int) (rune, int) {
	c := s[i]
	switch {
	case c < 0x80:
		return rune(c), 1
	case c < 0xE0 && i+1 < len(s):
		return rune(c&0x1F)<<6 | rune(s[i+1]&0x3F), 2
	case c < 0xF0 && i+2 < len(s):
		return rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F), 3
	case i+3 < len(s):
		return rune(c&0x07)<<18 | rune(s[i+1]&0x3F)<<12 | rune(s[i+2]&0x3F)<<6 | rune(s[i+3]&0x3F), 4
	}
	return 0xFFFD, 1
}

func appendUnits(u []uint16, s string) []uint16 {
	for i := 0; i < len(s); {
		r, n := decodeWTF8(s, i)
		i += n
		if r >= 0x10000 {
			r -= 0x10000
			u = append(u, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			u = append(u, uint16(r))
		}
	}
	return u
}

type unitCache struct {
	p     *byte
	n     int
	units []uint16 // nil for ASCII strings
}

var lastUnits atomic.Pointer[unitCache]

// units returns the UTF-16 code units of s, or nil if s is ASCII (in which
// case the bytes are the units).
func units(s string) []uint16 {
	if len(s) == 0 {
		return nil
	}
	p := unsafe.StringData(s)
	if c := lastUnits.Load(); c != nil && c.p == p && c.n == len(s) {
		return c.units
	}
	var u []uint16
	if !isASCII(s) {
		u = appendUnits(make([]uint16, 0, len(s)), s)
	}
	if len(s) >= 16 {
		lastUnits.Store(&unitCache{p: p, n: len(s), units: u})
	}
	return u
}

func appendWTF8(b []byte, r rune) []byte {
	if r >= 0xD800 && r <= 0xDFFF {
		return append(b, 0xED, byte(0xA0|(r>>6)&0x1F), byte(0x80|r&0x3F))
	}
	return utf8.AppendRune(b, r)
}

// fromUnits encodes UTF-16 code units as WTF-8.
func fromUnits(u []uint16) string {
	ascii := true
	for _, c := range u {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	b := make([]byte, 0, len(u))
	if ascii {
		for _, c := range u {
			b = append(b, byte(c))
		}
		return string(b)
	}
	for i := 0; i < len(u); i++ {
		c := rune(u[i])
		if c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF {
			r := 0x10000 + (c-0xD800)<<10 + rune(u[i+1]-0xDC00)
			b = utf8.AppendRune(b, r)
			i++
			continue
		}
		b = appendWTF8(b, c)
	}
	return string(b)
}

func JSCast(x any) any {
	return jsStr(x)
}

func JSTest(x any) int32 {
	_, ok := x.(string)
	return B2I(ok)
}

func JSLength(x any) int32 {
	s := jsStr(x)
	if u := units(s); u != nil {
		return int32(len(u))
	}
	return int32(len(s))
}

func JSCharCodeAt(x any, i int32) int32 {
	s := jsStr(x)
	u := units(s)
	if u == nil {
		if uint32(i) >= uint32(len(s)) {
			panic(&Trap{"string index out of bounds"})
		}
		return int32(s[uint32(i)])
	}
	if uint32(i) >= uint32(len(u)) {
		panic(&Trap{"string index out of bounds"})
	}
	return int32(u[uint32(i)])
}

func JSCodePointAt(x any, i int32) int32 {
	s := jsStr(x)
	u := units(s)
	if u == nil {
		if uint32(i) >= uint32(len(s)) {
			panic(&Trap{"string index out of bounds"})
		}
		return int32(s[uint32(i)])
	}
	if uint32(i) >= uint32(len(u)) {
		panic(&Trap{"string index out of bounds"})
	}
	c := u[uint32(i)]
	if c >= 0xD800 && c <= 0xDBFF && int(uint32(i))+1 < len(u) {
		d := u[uint32(i)+1]
		if d >= 0xDC00 && d <= 0xDFFF {
			return 0x10000 + (int32(c)-0xD800)<<10 + int32(d) - 0xDC00
		}
	}
	return int32(c)
}

func JSFromCharCode(c int32) any {
	return fromUnits([]uint16{uint16(c)})
}

func JSFromCodePoint(c int32) any {
	if uint32(c) > 0x10FFFF {
		panic(&Trap{"invalid code point"})
	}
	return string(appendWTF8(nil, rune(c)))
}

func endsWithHighSurrogate(s string) bool {
	n := len(s)
	return n >= 3 && s[n-3] == 0xED && s[n-2] >= 0xA0 && s[n-2] <= 0xAF
}

func startsWithLowSurrogate(s string) bool {
	return len(s) >= 3 && s[0] == 0xED && s[1] >= 0xB0 && s[1] <= 0xBF
}

func JSConcat(a, b any) any {
	x, y := jsStr(a), jsStr(b)
	if endsWithHighSurrogate(x) && startsWithLowSurrogate(y) {
		return fromUnits(appendUnits(appendUnits(nil, x), y))
	}
	return x + y
}

func JSSubstring(x any, start, end int32) any {
	s := jsStr(x)
	us, ue := uint32(start), uint32(end)
	u := units(s)
	n := uint32(len(s))
	if u != nil {
		n = uint32(len(u))
	}
	if us > n || ue < us {
		return ""
	}
	if ue > n {
		ue = n
	}
	if u == nil {
		return s[us:ue]
	}
	return fromUnits(u[us:ue])
}

func JSEquals(a, b any) int32 {
	if a == nil || b == nil {
		if a != nil {
			jsStr(a)
		}
		if b != nil {
			jsStr(b)
		}
		return B2I(a == nil && b == nil)
	}
	return B2I(jsStr(a) == jsStr(b))
}

func JSCompare(a, b any) int32 {
	x, y := jsStr(a), jsStr(b)
	ux, uy := units(x), units(y)
	if ux == nil && uy == nil {
		return int32(strings.Compare(x, y))
	}
	if ux == nil {
		ux = appendUnits(nil, x)
	}
	if uy == nil {
		uy = appendUnits(nil, y)
	}
	for i := 0; i < len(ux) && i < len(uy); i++ {
		if ux[i] != uy[i] {
			if ux[i] < uy[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ux) < len(uy):
		return -1
	case len(ux) > len(uy):
		return 1
	}
	return 0
}

func JSFromCharCodeArray(arr []int16, start, end int32) any {
	s, e := uint32(start), uint32(end)
	if s > e || e > uint32(len(arr)) {
		panic(ErrArrayOOB)
	}
	u := unsafe.Slice((*uint16)(unsafe.Pointer(unsafe.SliceData(arr))), len(arr))
	return fromUnits(u[s:e])
}

func JSIntoCharCodeArray(x any, arr []int16, start int32) int32 {
	s := jsStr(x)
	u := units(s)
	if u == nil {
		if uint64(uint32(start))+uint64(len(s)) > uint64(len(arr)) {
			panic(ErrArrayOOB)
		}
		for i := 0; i < len(s); i++ {
			arr[uint32(start)+uint32(i)] = int16(s[i])
		}
		return int32(len(s))
	}
	if uint64(uint32(start))+uint64(len(u)) > uint64(len(arr)) {
		panic(ErrArrayOOB)
	}
	for i, c := range u {
		arr[uint32(start)+uint32(i)] = int16(c)
	}
	return int32(len(u))
}

// toUTF8 replaces lone surrogates with U+FFFD.
func toUTF8(s string) string {
	if !strings.Contains(s, "\xED") {
		return s
	}
	var b []byte
	for i := 0; i < len(s); {
		r, n := decodeWTF8(s, i)
		i += n
		if r >= 0xD800 && r <= 0xDFFF {
			r = 0xFFFD
		}
		b = utf8.AppendRune(b, r)
	}
	return string(b)
}

func JSMeasureUTF8(x any) int32 {
	return int32(len(jsStr(x)))
}

func JSEncodeUTF8(x any) []int8 {
	s := toUTF8(jsStr(x))
	out := make([]int8, len(s))
	for i := 0; i < len(s); i++ {
		out[i] = int8(s[i])
	}
	return out
}

func JSEncodeUTF8Into(x any, arr []int8, start int32) int32 {
	s := toUTF8(jsStr(x))
	if uint64(uint32(start))+uint64(len(s)) > uint64(len(arr)) {
		panic(ErrArrayOOB)
	}
	for i := 0; i < len(s); i++ {
		arr[uint32(start)+uint32(i)] = int8(s[i])
	}
	return int32(len(s))
}

// JSDecodeUTF8 decodes UTF-8 like TextDecoder (replacing maximal invalid
// subparts with U+FFFD).
func JSDecodeUTF8(arr []int8, start, end int32) any {
	s, e := uint32(start), uint32(end)
	if s > e || e > uint32(len(arr)) {
		panic(ErrArrayOOB)
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(arr))), len(arr))[s:e]
	if utf8.Valid(b) {
		return string(b)
	}
	var out []byte
	for i := 0; i < len(b); {
		c := b[i]
		if c < 0x80 {
			out = append(out, c)
			i++
			continue
		}
		var need int
		lo, hi := byte(0x80), byte(0xBF)
		switch {
		case c >= 0xC2 && c <= 0xDF:
			need = 1
		case c >= 0xE0 && c <= 0xEF:
			need = 2
			if c == 0xE0 {
				lo = 0xA0
			} else if c == 0xED {
				hi = 0x9F
			}
		case c >= 0xF0 && c <= 0xF4:
			need = 3
			if c == 0xF0 {
				lo = 0x90
			} else if c == 0xF4 {
				hi = 0x8F
			}
		default:
			out = utf8.AppendRune(out, 0xFFFD)
			i++
			continue
		}
		j := i + 1
		ok := true
		for k := 0; k < need; k++ {
			if j >= len(b) || b[j] < lo || b[j] > hi {
				ok = false
				break
			}
			lo, hi = 0x80, 0xBF
			j++
		}
		if !ok {
			out = utf8.AppendRune(out, 0xFFFD)
			i = j
			continue
		}
		out = append(out, b[i:j]...)
		i = j
	}
	return string(out)
}
