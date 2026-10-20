package rt

import (
	"math"
	"testing"
)

func TestJSNumberToString(t *testing.T) {
	cases := map[float64]string{
		1: "1", -1.5: "-1.5", 0.1: "0.1", 1e21: "1e+21", 1e20: "100000000000000000000",
		123456789012345680000: "123456789012345680000", 1e-7: "1e-7", 1.5e-7: "1.5e-7",
		0.000001: "0.000001", 3.14159: "3.14159", math.MaxFloat64: "1.7976931348623157e+308",
		5e-324: "5e-324", 2000: "2000", 0.5: "0.5", 100.25: "100.25",
	}
	for f, want := range cases {
		if got := JSNumberToString(f); got != want {
			t.Errorf("%v: got %q want %q", f, got, want)
		}
	}
}
