package dbench

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"gosm/examples/dart/dartnum"
	"gosm/rt"
)

// DartHost implements the dart2wasm imports this program needs. (dart2wasm
// numbers its imports per compilation; these correspond to fib.mjs.)
type DartHost struct {
	jsTag *rt.Tag
}

func NewDartHost() *DartHost {
	return &DartHost{jsTag: &rt.Tag{Type: rt.FuncType([]string{"ref null extern"}, nil)}}
}

func (h *DartHost) M_dart2wasm__290(x float64) any { return dartnum.Format(x) }
func (h *DartHost) M_dart2wasm__288(x, radix int32) any {
	return strconv.FormatInt(int64(x), int(radix))
}
func (h *DartHost) M_dart2wasm__289(x int64, radix int32) any {
	return strconv.FormatInt(x, int(radix))
}
func (h *DartHost) M_dart2wasm__33(s any) any { return strconv.Quote(fmt.Sprint(s)) }
func (h *DartHost) M_dart2wasm__34(s any)     { fmt.Println(s) }
func (h *DartHost) M_dart2wasm__43(s, sub any, from int32) float64 {
	// String.prototype.indexOf (ASCII-correct; see rt js-string helpers)
	str, pat := s.(string), sub.(string)
	if int(from) > len(str) {
		from = int32(len(str))
	}
	i := strings.Index(str[from:], pat)
	if i < 0 {
		return -1
	}
	return float64(int(from) + i)
}
func (h *DartHost) M_dart2wasm__9() any          { return "" }
func (h *DartHost) M_dart2wasm__18(e any) any    { return fmt.Sprint(e) }
func (h *DartHost) M_dart2wasm__19(e any) any    { return "" }
func (h *DartHost) M_dart2wasm__20(e any) any    { return "" }
func (h *DartHost) M_WebAssembly_JSTag() *rt.Tag { return h.jsTag }

var start = time.Now()

func (h *DartHost) M_dart2wasm__10() int32 { return 0 }
func (h *DartHost) M_dart2wasm__11() float64 {
	return float64(time.Since(start).Nanoseconds()) / 1000 // performance.now()*1000
}
func (h *DartHost) M_dart2wasm__12() float64 { return float64(time.Now().UnixMilli()) }
func (h *DartHost) M_dart2wasm__134(any) any { return rand.Float64() }
func (h *DartHost) M_dart2wasm__137() any    { return "Math" }
func (h *DartHost) M_dart2wasm__161(o any) int32 {
	return rt.B2I(o == nil)
}
func (h *DartHost) M_dart2wasm__168(o any) float64 { return o.(float64) }
