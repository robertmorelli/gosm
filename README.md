# gosm

An ahead-of-time compiler from WebAssembly 3.0 (including GC) to Go source.
A module becomes an ordinary Go package that uses Go's garbage collector and
object model directly.

```sh
go run gosm/cmd/gosm -pkg mymod -o mymod_gosm.go mymod.wasm
```

or

```go
//go:generate go run gosm/cmd/gosm -o mymod_gosm.go mymod.wasm
```

Passing several modules (`gosm a.wasm b.wasm`) emits one package with GC types
shared across them. Don't name output files `*_wasm.go` or `*_js.go`: Go
treats those suffixes as build constraints.

## Mapping

| WebAssembly | Go |
|---|---|
| `struct`, `sub` | Go struct, with the supertype embedded first |
| `array` | struct with an `Elems` slice |
| `(ref null $T)` | `*T` |
| `anyref`, `externref`, … | `any` |
| `funcref` | `*rt.Func` |
| exports | methods on `*Instance` |
| imports | a `Host` interface, one method per import |
| traps | `panic(*rt.Trap)`; `rt.Catch` converts to `error` |
| threads | goroutines sharing an `*rt.Memory` |

`-host-type "*MyHost"` uses a concrete host type in place of the interface so
that Go can inline host calls. Imports from `wasm:js-string` are built in, and
`gosm/wasi` provides a WASI preview1 host.

## Coverage

All of WebAssembly 3.0 plus threads: GC, exception handling (including legacy
`try`/`delegate`), tail calls, memory64, multi-memory, SIMD and relaxed SIMD.

`scripts/spectest.sh` runs the official spec and threads test suites:

```
TOTAL pass 64561 fail 0 skip 1248
```

The skipped tests are malformed text-format cases, which don't apply to a
binary-only tool.

Not supported: stack switching, custom descriptors, custom page sizes.

## Examples

- `examples/hello`: a hand-written `.wat` with GC structs and js-string imports
- `examples/kotlin`: a Kotlin/Wasm (WASI) program
- `examples/dart`: a `dart compile wasm` program

## Performance

`bench/run.sh` runs the same Kotlin and Dart programs on Node 25 (V8) and
through gosm on Go 1.27, best of 3 on Apple silicon. Speedup above 1 means gosm
is faster. The Kotlin module was optimized with `wasm-opt -O3 --gufa`.

```
benchmark        kotlin-v8  kotlin-go  speedup      dart-v8    dart-go  speedup
binary-trees         133.4      226.0    0.59x         85.7      209.4    0.41x
fib                    7.6        9.0    0.84x          7.6        9.0    0.85x
nbody                236.1      124.8    1.89x        233.4      147.6    1.58x
strings-maps         215.9      405.3    0.53x        227.0      396.8    0.57x
virtual-calls        219.2      172.9    1.27x        159.9       91.9    1.74x
```

Numeric and dispatch-heavy code runs faster than on V8. Allocation-heavy code
runs slower, because Go's allocator doesn't match V8's generational nursery.
