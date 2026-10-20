// Command kbench runs the Kotlin benchmark natively in Go.
package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"

	"gosm/bench/kotlin/kbench"
	"gosm/wasi"
)

func main() {
	if p := os.Getenv("CPUPROFILE"); p != "" {
		f, _ := os.Create(p)
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
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
	if os.Getenv("MEMSTATS") != "" {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		fmt.Fprintf(os.Stderr, "mallocs=%d bytes=%d gcs=%d\n", ms.Mallocs, ms.TotalAlloc, ms.NumGC)
	}
	if code != 0 {
		os.Exit(int(code))
	}
}
