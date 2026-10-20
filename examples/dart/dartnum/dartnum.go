// Package dartnum formats numbers the way the dart2wasm glue expects
// (Number.prototype.toString semantics), for the hand-written Dart hosts.
package dartnum

import (
	"math"
	"strconv"
	"strings"
)

// Format formats f like Number.prototype.toString().
func Format(f float64) string {
	switch {
	case f != f:
		return "NaN"
	case f == 0:
		return "0"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	sign := ""
	if f < 0 {
		sign = "-"
		f = -f
	}
	// shortest round-trip digits and decimal exponent
	s := strconv.FormatFloat(f, 'e', -1, 64) // d.ddddde±XX
	mant, exp, _ := strings.Cut(s, "e")
	digits := strings.Replace(mant, ".", "", 1)
	e, _ := strconv.Atoi(exp)
	k := len(digits)
	n := e + 1 // position of the decimal point
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	es := "+"
	if n-1 < 0 {
		es = "-"
	}
	ev := n - 1
	if ev < 0 {
		ev = -ev
	}
	if k == 1 {
		return sign + digits + "e" + es + strconv.Itoa(ev)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + es + strconv.Itoa(ev)
}

// FormatRadix formats an integer-valued number in the given radix
// like Number.prototype.toString(radix) (integers only).
func FormatRadix(f float64, radix int) string {
	if radix == 10 || f != math.Trunc(f) || math.IsInf(f, 0) {
		return Format(f)
	}
	return strconv.FormatInt(int64(f), radix)
}
