// Command gosm translates a WebAssembly (3.0, including GC) module into a Go
// package.
//
// Usage:
//
//	gosm [-pkg name] [-o out.go] [-rt importpath] module.wasm [more.wasm...]
//	gosm -emit-host module.wasm   # print only the Host interface
//
// Several modules compiled together share their GC types, so struct and
// array values can flow between them; each gets its own instance type named
// after its file (a.wasm -> type A, NewA, AHost).
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
	"gosm/internal/validate"
	"gosm/internal/wasm"
)

func main() {
	pkg := flag.String("pkg", "", "Go package name (default: $GOPACKAGE or derived from the file name)")
	out := flag.String("o", "", "output file (default stdout)")
	rtPath := flag.String("rt", "gosm/rt", "import path of the gosm runtime package")
	hostOnly := flag.Bool("emit-host", false, "emit only the Host interface")
	var strMods stringList
	flag.Var(&strMods, "string-constants", "import module whose global imports are string constants (repeatable; default \"'\"; Dart uses \"\")")
	noStrConst := flag.Bool("no-string-constants", false, "disable imported string constants")
	noBuiltins := flag.Bool("no-builtins", false, "do not implement wasm:js-string / text-encoder / text-decoder builtins")
	hostType := flag.String("host-type", "", "concrete Go host type defined in the package (e.g. \"*Imports\"); host calls become direct and inlinable")
	noValidate := flag.Bool("no-validate", false, "skip validation of the input module(s)")
	noFormat := flag.Bool("no-format", false, "do not gofmt the output")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gosm [flags] module.wasm\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}
	in := flag.Arg(0)
	var srcs []compiler.Source
	for _, path := range flag.Args() {
		b, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		m, err := wasm.Decode(b)
		if err != nil {
			fatal(fmt.Errorf("%s: %v", path, err))
		}
		if !*noValidate {
			if err := validate.Validate(m); err != nil {
				fatal(fmt.Errorf("%s: invalid module: %v", path, err))
			}
		}
		srcs = append(srcs, compiler.Source{Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), Module: m})
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
	srcName := filepath.Base(in)
	if len(srcs) > 1 {
		srcName = fmt.Sprintf("%d modules", len(srcs))
	}
	src, err := compiler.CompileModules(srcs, compiler.Options{
		Package:            name,
		RuntimePath:        *rtPath,
		Source:             srcName,
		StringConstModules: strModules(strMods, *noStrConst),
		NoBuiltins:         *noBuiltins,
		HostOnly:           *hostOnly,
		NoFormat:           *noFormat,
		HostType:           *hostType,
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

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func strModules(l stringList, disable bool) []string {
	if disable {
		return []string{}
	}
	if len(l) == 0 {
		return nil
	}
	return l
}
