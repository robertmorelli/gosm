# gosm — WebAssembly 3.0 (WasmGC) → Go

gosm is an ahead-of-time transpiler from WebAssembly 3.0 modules, including
the GC proposal, to idiomatic, `go vet`-clean Go source. The module becomes an
ordinary Go package. It shares Go's garbage collector, object model and
compiler, with no FFI bridge in between.

```sh
go run gosm/cmd/gosm -pkg mymod -o mymod_gosm.go mymod.wasm
go run gosm/cmd/gosm -pkg app -o app_gosm.go a.wasm b.wasm   # several modules, shared GC types
```

Input is validated first; pass `-no-validate` to skip that.

or with `go generate`:

```go
//go:generate go run gosm/cmd/gosm -o mymod_gosm.go mymod.wasm
```

> Don't name the output `*_wasm.go` or `*_js.go`. Go treats those suffixes as
> GOARCH/GOOS build constraints and silently skips the file. gosm warns if you do.

## What you get

| WebAssembly | Go |
|---|---|
| `i32` `i64` `f32` `f64` `v128` | `int32` `int64` `float32` `float64` `rt.V128` |
| `(struct (field $x (mut i32)) …)` | `type Point struct { X int32; … }` plus `NewPoint(x, …)` |
| `(sub $Shape (struct …))` | `type Circle struct { Shape; R float64 }`: the supertype is embedded first, so `&c.Shape` is the upcast |
| `(array (mut i16))` | `type Chars struct { Elems []int16 }` |
| `(ref null $Point)` | `*Point` (`nil` is null) |
| `anyref` `eqref` `externref` `structref` `arrayref` | `any` (holding the exact dynamic `*T`, `rt.I31` or a host value) |
| `funcref`, `(ref $sig)` | `*rt.Func` (`Fn` is `func(depth int, params…) results`) |
| `exnref` / tags | `*rt.Exception` / `*rt.Tag` |
| `block` / `loop` / `if` / `br_table` | labels with `goto`, labeled `for` with `continue`/`break`, `if`/`else`, `switch` |
| exports | methods on `*Instance` named after the export (`Greet`, `Memory`, …) and `Exports()` |
| imports | a `Host` interface with one method per import, `M_<module>_<field>` |
| traps | `panic(*rt.Trap)`; `rt.Catch(func(){…})` turns traps and exceptions into `error` |
| exceptions | handled inline in the generated code, with no panic (see below). At the API boundary they are `panic(*rt.Exception)`: a Go host throws by panicking, and an export that throws panics |
| threads | goroutines, each running its own `Instance`, sharing an `*rt.Memory`; atomics use `sync/atomic` |

Operand-stack values become typed locals (`s0`, `s1`, named by stack height)
or are inlined as pure expressions. Names come from the name section or the
export names:

```go
// generated from (func $fib (param $n i64) (result i64) (if (result i64) …))
func (m *Instance) fib(d int, n int64) int64 {
	if d > rt.MaxCallDepth {
		rt.Exhausted()
	}
	var s0, s1 int64
	if uint64(n) < uint64(2) {
		s0 = n
	} else {
		s0 = m.fib(d+1, (n - int64(1)))
		s1 = m.fib(d+1, (n - int64(2)))
		s0 = (s0 + s1)
	}
	return s0
}
```

The `d` parameter is the wasm call depth. Only functions that can recurse
check it, which turns runaway recursion into a trap instead of a fatal Go
stack overflow. Leaf functions don't check it, so Go can inline them.

**Exceptions without panics.** In modules that contain handlers:
- `throw` stores the exception in a pending slot and jumps straight to the
  handler's label in the same Go function, or returns.
- Every call site checks the slot.
- `try_table` is therefore plain inline code that costs nothing to enter.
- Throwing and catching runs about 5× faster than V8 in a microbenchmark.

**Tail calls run in constant space.**
- Self tail calls become loops.
- Direct tail calls outside a cycle are plain Go calls.
- Tail calls that could recur without bound go through a trampoline: mutual
  tail recursion, `return_call_indirect` and `return_call_ref`. 40 million
  mutual tail calls run in 13 MB.

**Multi-module packages.** `gosm a.wasm b.wasm` emits one package in which
GC types are canonicalized across the modules. A `*Point` made by module A
is then the same Go type that module B consumes. Each module gets its own
`A`/`NewA`/`AHost`.

### The Host

```go
type Host interface {
	// M_env_log imports "env" "log".
	M_env_log(p0 any)
}
inst, err := greeter.New(myHost{})
```

- Function imports are methods. Memory, table, global and tag imports are
  zero-argument getters returning `*rt.Memory`, `*rt.Table[T]`,
  `*rt.Global[T]` and `*rt.Tag`.
- Method names are `M_` + esc(module) + `_` + esc(field). Bytes outside
  `[A-Za-z0-9_]` become `_hh`.
- `gosm -emit-host x.wasm` prints only the interface, for host generators.
- `gosm -host-type "*MyHost"` makes the instance hold your concrete host type
  instead of the interface. Host calls then become direct calls that Go can
  inline into the wasm code, removing interface dispatch and the boxing of
  `any` arguments. On the Dart benchmarks this alone sped up hot loops by
  1.8–3.2×.

### JS String Builtins and string constants

- Imports from `wasm:js-string`, `wasm:text-encoder` and `wasm:text-decoder`
  are implemented natively, so they never appear in `Host`.
- Strings are plain Go `string`s inside an `externref` (`any`). All
  `length`/`charCodeAt`/`substring`/`compare` operations use JS UTF-16
  code-unit semantics: `length("é") == 1`, `length("😀") == 2`.
- Lone surrogates are represented in WTF-8, and concatenation re-pairs them.
  The behavior is checked against V8.
- Immutable global imports from module `'` (`importedStringConstants`) become
  string constants. `-string-constants ""` handles Dart's convention.

### WASI

Package `gosm/wasi` implements WASI preview1 and the wasi-threads
`thread-spawn` hook (`h.Spawn`). Its methods follow the Host
naming convention, so one `*wasi.Host` satisfies the WASI part of any
generated `Host`:

```go
h := wasi.New(os.Args, nil)
inst, _ := ktapp.New(h)
h.Memory = inst.Memory()
code, err := h.Run(inst.Start)
```

## Coverage

gosm implements essentially all of WebAssembly 3.0:
- MVP, multi-value, sign-ext and saturating conversions
- bulk memory, reference types and typed function references
- GC: structs, arrays, i31, casts, `br_on_cast*`, iso-recursive type
  canonicalization, `extern.convert_any`
- exception handling: `try_table`, `throw_ref`, and the legacy
  `try`/`catch`/`delegate`/`rethrow`
- tail calls, memory64 and table64, multi-memory, extended-const
- 128-bit SIMD and relaxed SIMD

It also implements the threads proposal: shared memories, atomics and
`wait`/`notify`.

`scripts/spectest.sh` downloads the official spec and threads test suites,
then:
1. checks that every invalid or malformed binary module is rejected,
2. compiles all of a script's modules into one package,
3. runs every assertion, with spec `thread` blocks on goroutines.

```
TOTAL pass 64561 fail 0 skip 1248
```

The skips are malformed *text-format* tests, which don't apply to a
binary-only tool.

## Real producers

| producer | example | status |
|---|---|---|
| Kotlin/Wasm (WASI target) | `examples/kotlin` | output matches Node |
| Dart (`dart compile wasm`) | `examples/dart` | output matches Node (hand-written `dart2wasm` host) |
| Your own `.wat` | `examples/hello` | imports, js-string builtins, GC structs |
| J2Wasm | — | not tested yet (needs a Bazel/J2CL toolchain) |

dart2wasm numbers its JS imports per compilation (`_288`, …). The Dart host
in `examples/dart` therefore covers only that program's imports, and a
general Dart host would have to derive its methods from the generated `.mjs`.

## Performance vs V8

`bench/run.sh` runs the same programs with Node 25 (V8) and as gosm-translated
Go 1.27, taking the best of 3 on an Apple-silicon Mac. Times are in ms;
speedup > 1 means gosm is faster. Kotlin wasm was optimized with
`wasm-opt -O3 --gufa` for both engines, and Dart's own `-O2` pipeline already
runs Binaryen.

```
benchmark        kotlin-v8  kotlin-go  speedup      dart-v8    dart-go  speedup
binary-trees         133.4      226.0    0.59x         85.7      209.4    0.41x
fib                    7.6        9.0    0.84x          7.6        9.0    0.85x
nbody                236.1      124.8    1.89x        233.4      147.6    1.58x
strings-maps         215.9      405.3    0.53x        227.0      396.8    0.57x
virtual-calls        219.2      172.9    1.27x        159.9       91.9    1.74x
```

- **Where AOT wins:** field-heavy numeric code and virtual dispatch. Go
  compiles structs to fixed offsets with real registers and needs no tier-up.
  With `-host-type`, host calls are inlined across what used to be the module
  boundary.
- **Where V8 wins:** allocation-heavy code. V8's generational nursery
  bump-allocates and escape-analyzes short-lived objects, while Go's
  allocator costs about 10–15 ns per object, and each GC object in a subtype
  hierarchy also carries an 8-byte type header. `GOGC=400` recovers about 25%.

## Limitations

- **Go's allocator and GC:** allocation-heavy code still trails V8's
  generational nursery. That's a property of Go's runtime; `GOGC` tuning
  helps.
- **Separately generated packages don't share GC types.** Compile modules
  that exchange struct or array values together. Funcrefs, externrefs,
  memories, tables, globals and tags cross package boundaries fine.
- **Resource limits:** `rt.MaxMemoryPages` (default 4 GiB), `rt.MaxTableSize`,
  `rt.MaxArrayLen` and `rt.MaxCallDepth` are tunable.
- **Not supported:** stack switching, custom descriptors and custom page
  sizes (post-3.0 proposals).

## Layout

| path | contents |
|---|---|
| `cmd/gosm` | the CLI |
| `internal/wasm` | binary decoder for the full 3.0 format |
| `internal/validate` | WebAssembly 3.0 + threads validator |
| `internal/compiler` | type universe, function codegen, exceptions, tail calls, module/Host/exports emission |
| `rt` | runtime: memories, tables, RTTs and casts, exceptions, numerics, SIMD, JS string builtins |
| `wasi` | WASI preview1 host |
| `internal/spectest` | spec-test harness (`gen` compiles and runs, `runner` executes scripts), plus `extra/` tests |
| `examples/`, `bench/` | the examples and benchmarks above |
