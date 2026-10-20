package wasm

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

var errEOF = errors.New("unexpected end")

type reader struct {
	b   []byte
	pos int
}

func (r *reader) eof() bool { return r.pos >= len(r.b) }

func (r *reader) fail(format string, args ...any) {
	panic(decodeError{fmt.Sprintf("at offset %d: ", r.pos) + fmt.Sprintf(format, args...)})
}

type decodeError struct{ msg string }

func (r *reader) byte() byte {
	if r.pos >= len(r.b) {
		r.fail("unexpected end")
	}
	c := r.b[r.pos]
	r.pos++
	return c
}

func (r *reader) peek() byte {
	if r.pos >= len(r.b) {
		r.fail("unexpected end")
	}
	return r.b[r.pos]
}

func (r *reader) bytes(n int) []byte {
	if n < 0 || r.pos+n > len(r.b) {
		r.fail("unexpected end")
	}
	s := r.b[r.pos : r.pos+n]
	r.pos += n
	return s
}

func (r *reader) u32() uint32 {
	var result uint64
	var shift uint
	for i := 0; ; i++ {
		c := r.byte()
		if i == 4 && c&0x70 != 0 {
			r.fail("integer too large")
		}
		result |= uint64(c&0x7F) << shift
		if c&0x80 == 0 {
			break
		}
		shift += 7
		if i >= 4 {
			r.fail("integer representation too long")
		}
	}
	return uint32(result)
}

func (r *reader) u64() uint64 {
	var result uint64
	var shift uint
	for i := 0; ; i++ {
		c := r.byte()
		if i == 9 && c&0x7E != 0 {
			r.fail("integer too large")
		}
		result |= uint64(c&0x7F) << shift
		if c&0x80 == 0 {
			break
		}
		shift += 7
		if i >= 9 {
			r.fail("integer representation too long")
		}
	}
	return result
}

// sN reads a signed LEB128 of at most n bits.
func (r *reader) sN(n uint) int64 {
	var result int64
	var shift uint
	maxBytes := int((n + 6) / 7)
	for i := 0; ; i++ {
		c := r.byte()
		if i == maxBytes-1 {
			// remaining bits must be a sign extension
			used := n - shift // bits available in this byte
			if used < 7 {
				mask := byte(0x7F) &^ byte((1<<(used))-1) // unused high bits
				sign := c & byte(1<<(used-1))
				if sign != 0 {
					if c&mask != mask {
						r.fail("integer too large")
					}
				} else if c&mask != 0 {
					r.fail("integer too large")
				}
			}
			if c&0x80 != 0 {
				r.fail("integer representation too long")
			}
		}
		result |= int64(c&0x7F) << shift
		shift += 7
		if c&0x80 == 0 {
			if shift < 64 && c&0x40 != 0 {
				result |= -1 << shift
			}
			break
		}
	}
	return result
}

func (r *reader) s32() int32 { return int32(r.sN(32)) }
func (r *reader) s33() int64 { return r.sN(33) }
func (r *reader) s64() int64 { return r.sN(64) }

func (r *reader) f32bits() uint32 {
	b := r.bytes(4)
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func (r *reader) f64bits() uint64 {
	b := r.bytes(8)
	var v uint64
	for i := 7; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

func (r *reader) name() string {
	n := r.u32()
	b := r.bytes(int(n))
	if !utf8.Valid(b) {
		r.fail("malformed UTF-8 encoding")
	}
	return string(b)
}

var _ = math.MaxInt32
