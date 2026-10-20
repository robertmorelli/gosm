// Package fibapp is fib.dart compiled with `dart compile wasm` and
// translated to Go by gosm.
package fibapp

//go:generate sh -c "dart compile wasm fib.dart -o fib.wasm && rm -f fib.mjs fib.support.js fib.wasm.map"
//go:generate go run gosm/cmd/gosm -string-constants "" -o fib_gosm.go fib.wasm
