// Package greeter is generated from greeter.wat by gosm.
package greeter

//go:generate sh -c "wasm-tools parse greeter.wat -o greeter.wasm"
//go:generate go run gosm/cmd/gosm -o greeter_gosm.go greeter.wasm
