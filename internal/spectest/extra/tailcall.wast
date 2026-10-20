;; Tail calls must run in constant stack space.
(module
  (type $t (func (param i64) (result i64)))
  (table $tab 2 funcref)
  (elem (table $tab) (i32.const 0) func $ieven $iodd)
  (tag $e)

  ;; direct mutual recursion
  (func $even (export "even") (param i64) (result i64)
    (if (result i64) (i64.eqz (local.get 0)) (then (i64.const 1))
      (else (return_call $odd (i64.sub (local.get 0) (i64.const 1))))))
  (func $odd (param i64) (result i64)
    (if (result i64) (i64.eqz (local.get 0)) (then (i64.const 0))
      (else (return_call $even (i64.sub (local.get 0) (i64.const 1))))))

  ;; indirect mutual recursion through a table
  (func $ieven (export "ieven") (param i64) (result i64)
    (if (result i64) (i64.eqz (local.get 0)) (then (i64.const 1))
      (else (return_call_indirect $tab (type $t) (i64.sub (local.get 0) (i64.const 1)) (i32.const 1)))))
  (func $iodd (param i64) (result i64)
    (if (result i64) (i64.eqz (local.get 0)) (then (i64.const 0))
      (else (return_call_indirect $tab (type $t) (i64.sub (local.get 0) (i64.const 1)) (i32.const 0)))))

  ;; through call_ref
  (func $reven (export "reven") (param i64) (result i64)
    (if (result i64) (i64.eqz (local.get 0)) (then (i64.const 1))
      (else (return_call_ref $t (i64.sub (local.get 0) (i64.const 1)) (ref.func $rodd)))))
  (func $rodd (param i64) (result i64)
    (if (result i64) (i64.eqz (local.get 0)) (then (i64.const 0))
      (else (return_call_ref $t (i64.sub (local.get 0) (i64.const 1)) (ref.func $reven)))))
  (elem declare func $rodd $reven)

  ;; self tail call from inside a try_table (handler must not apply to callee)
  (func $count (export "count-in-try") (param i64) (result i64)
    (block $h
      (try_table (catch $e $h)
        (if (i64.eqz (local.get 0)) (then (return (i64.const 7))))
        (return_call $count (i64.sub (local.get 0) (i64.const 1)))))
    (i64.const -1))

  ;; a callee throwing after a tail call from a try must not be caught there
  (func $thrower (param i64) (result i64) (throw $e))
  (func (export "tail-escapes-handler") (result i64)
    (block $h
      (try_table (catch $e $h)
        (return_call $thrower (i64.const 0))))
    (i64.const -1))
)
(assert_return (invoke "even" (i64.const 10000000)) (i64.const 1))
(assert_return (invoke "even" (i64.const 10000001)) (i64.const 0))
(assert_return (invoke "ieven" (i64.const 10000000)) (i64.const 1))
(assert_return (invoke "reven" (i64.const 10000001)) (i64.const 0))
(assert_return (invoke "count-in-try" (i64.const 10000000)) (i64.const 7))
(assert_exception (invoke "tail-escapes-handler"))
