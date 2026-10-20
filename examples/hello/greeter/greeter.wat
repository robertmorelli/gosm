(module
  (type $chars (array (mut i16)))
  (type $Point (struct (field $x (mut i32)) (field $y (mut i32))))
  (import "env" "log" (func $log (param externref)))
  (import "wasm:js-string" "concat" (func $concat (param externref externref) (result (ref extern))))
  (import "wasm:js-string" "length" (func $length (param externref) (result i32)))
  (import "'" "Hello, " (global $hello externref))

  (func (export "greet") (param $name externref) (result i32)
    (local $s externref)
    (local.set $s (call $concat (global.get $hello) (local.get $name)))
    (call $log (local.get $s))
    (call $length (local.get $s)))

  (func (export "newPoint") (param $x i32) (param $y i32) (result (ref $Point))
    (struct.new $Point (local.get $x) (local.get $y)))

  (func (export "manhattan") (param $p (ref null $Point)) (result i32)
    (i32.add (struct.get $Point $x (local.get $p)) (struct.get $Point $y (local.get $p))))
)
