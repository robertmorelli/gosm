package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gosm/internal/wasm"
)

func main() {
	files, _ := filepath.Glob(os.Args[1] + "/*/*.wasm")
	bad := 0
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if _, err := wasm.Decode(b); err != nil {
			bad++
			if len(os.Args) > 2 {
				fmt.Println(f, err)
			}
		}
	}
	fmt.Println(len(files), "files,", bad, "failed")
}
