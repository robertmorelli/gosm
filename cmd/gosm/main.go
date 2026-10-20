// Command gosm translates a WebAssembly (3.0, including GC) module into a Go
// package.
//
// Usage:
//
//	gosm [-pkg name] [-o out.go] [-rt importpath] module.wasm
//	gosm -emit-host module.wasm   # print only the Host interface
//
// It is designed for go generate:
//
//	//go:generate gosm -pkg mymod -o mymod.go mymod.wasm
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gosm/internal/compiler"
	"gosm/internal/wasm"
)

func main() {
	pkg := flag.String("pkg", "", "Go package name (default: $GOPACKAGE or derived from the file name)")
	out := flag.String("o", "", "output file (default stdout)")
	rtPath := flag.String("rt", "gosm/rt", "import path of the gosm runtime package")
	hostOnly := flag.Bool("emit-host", false, "emit only the Host interface")
	strMod := flag.String("string-constants", "'", "import module providing imported string constants (\"\" disables)")
	noBuiltins := flag.Bool("no-builtins", false, "do not implement wasm:js-string / text-encoder / text-decoder builtins")
	noFormat := flag.Bool("no-format", false, "do not gofmt the output")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gosm [flags] module.wasm\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	in := flag.Arg(0)
	b, err := os.ReadFile(in)
	if err != nil {
		fatal(err)
	}
	m, err := wasm.Decode(b)
	if err != nil {
		fatal(err)
	}
	name := *pkg
	if name == "" {
		name = os.Getenv("GOPACKAGE")
	}
	if name == "" {
		base := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
		name = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
				return r
			}
			if r >= 'A' && r <= 'Z' {
				return r + 32
			}
			return -1
		}, base)
		if name == "" || name[0] >= '0' && name[0] <= '9' {
			name = "wasm" + name
		}
	}
	src, err := compiler.Compile(m, compiler.Options{
		Package:           name,
		RuntimePath:       *rtPath,
		Source:            filepath.Base(in),
		StringConstModule: *strMod,
		NoBuiltins:        *noBuiltins,
		HostOnly:          *hostOnly,
		NoFormat:          *noFormat,
	})
	if err != nil {
		if src != nil && *out != "" {
			os.WriteFile(*out, src, 0o644)
		}
		fatal(err)
	}
	if *out == "" {
		os.Stdout.Write(src)
		return
	}
	if base := strings.TrimSuffix(filepath.Base(*out), ".go"); strings.HasSuffix(base, "_wasm") || strings.HasSuffix(base, "_js") {
		fmt.Fprintf(os.Stderr, "gosm: warning: %s ends in a GOOS/GOARCH suffix and will only build for that target\n", *out)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gosm:", err)
	os.Exit(1)
}
