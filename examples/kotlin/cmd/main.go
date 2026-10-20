// Command ktapp runs the Kotlin program examples/kotlin/ktapp natively in Go.
package main

import (
	"fmt"
	"os"

	"gosm/examples/kotlin/ktapp"
	"gosm/wasi"
)

func main() {
	h := wasi.New(os.Args, nil)
	inst, err := ktapp.New(h)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	h.Memory = inst.Memory()
	code, err := h.Run(inst.Start)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(int(code))
}
