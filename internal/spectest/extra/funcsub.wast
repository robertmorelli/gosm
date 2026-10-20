;; Calling a function through a supertype whose Go signature differs.
(module
  (type $A (sub (struct (field i32))))
  (type $B (sub $A (struct (field i32) (field i32))))
  (type $f1 (sub (func (param (ref $B)) (result (ref null $A)))))
  (type $f2 (sub $f1 (func (param (ref $A)) (result (ref null $B)))))
  (table $t 1 funcref)
  (elem declare func $impl)
  (func $impl (type $f2) (param $a (ref $A)) (result (ref null $B))
    (struct.new $B (i32.add (struct.get $A 0 (local.get $a)) (i32.const 1)) (i32.const 0)))
  (func (export "via-call-ref") (result i32)
    (struct.get $A 0
      (call_ref $f1 (struct.new $B (i32.const 41) (i32.const 0)) (ref.func $impl))))
  (func (export "via-call-indirect") (result i32)
    (table.set $t (i32.const 0) (ref.func $impl))
    (struct.get $A 0
      (call_indirect $t (type $f1) (struct.new $B (i32.const 9) (i32.const 0)) (i32.const 0))))
)
(assert_return (invoke "via-call-ref") (i32.const 42))
(assert_return (invoke "via-call-indirect") (i32.const 10))
