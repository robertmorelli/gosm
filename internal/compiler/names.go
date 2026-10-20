package compiler

import (
	"fmt"
	"go/token"
	"strings"
	"unicode"
)

// splitWords splits a wasm name into alphanumeric words.
func splitWords(s string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return words
}

// exportIdent converts a wasm name into an exported Go identifier.
func exportIdent(s string) string {
	var b strings.Builder
	for _, w := range splitWords(s) {
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	r := b.String()
	if r == "" || !unicode.IsLetter(rune(r[0])) {
		r = "X" + r
	}
	return r
}

// localIdent converts a wasm name into an unexported Go identifier.
func localIdent(s string) string {
	words := splitWords(s)
	var b strings.Builder
	for i, w := range words {
		if i == 0 {
			b.WriteString(strings.ToLower(w[:1]) + w[1:])
		} else {
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	r := b.String()
	if r == "" || !unicode.IsLetter(rune(r[0])) {
		r = "x" + r
	}
	if token.IsKeyword(r) {
		r += "_"
	}
	return r
}

// hostMethodName implements the Host naming contract:
// "M_" + esc(module) + "_" + esc(field), where esc keeps [A-Za-z0-9_] and
// encodes every other byte as "_" followed by two lowercase hex digits.
func hostMethodName(module, field string) string {
	return "M_" + hostEsc(module) + "_" + hostEsc(field)
}

func hostEsc(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "_%02x", c)
		}
	}
	return b.String()
}

// pkgIdent reserves a unique package-level identifier.
func (c *compiler) pkgIdent(base string) string { return c.u.pkgIdent(base) }

// memberIdent reserves a unique Instance member (field or method) name.
func (c *compiler) memberIdent(base string) string {
	name := base
	for k := 2; c.members[name]; k++ {
		name = fmt.Sprintf("%s%d", base, k)
	}
	c.members[name] = true
	return name
}

var reservedLocals = map[string]bool{
	"m": true, "d": true, "rt": true, "math": true, "bits": true, "unsafe": true,
	"c": true, "r": true, "e": true, "ok": true, "nil": true, "true": true, "false": true,
	"any": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint8": true, "uint16": true, "uint32": true, "uint64": true, "float32": true,
	"float64": true, "len": true, "make": true, "panic": true, "recover": true,
	"append": true, "copy": true, "string": true, "bool": true, "byte": true,
	"error": true, "new": true, "cap": true, "min": true, "max": true, "uint": true,
	"iota": true, "print": true, "println": true, "clear": true, "close": true,
	"delete": true, "complex": true, "real": true, "imag": true, "rune": true,
	"uintptr": true, "complex64": true, "complex128": true,
}
