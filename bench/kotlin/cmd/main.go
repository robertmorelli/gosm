// Command kbench runs the Kotlin benchmark natively in Go.
package main

import (
	"fmt"
	"os"

	"gosm/bench/kotlin/kbench"
	"gosm/wasi"
)

func main() {
	h := wasi.New(os.Args, nil)
	inst, err := kbench.New(h)
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
