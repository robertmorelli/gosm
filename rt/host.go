package rt

import "reflect"

var goTypes = map[string]reflect.Type{
	"i32":  reflect.TypeOf(int32(0)),
	"i64":  reflect.TypeOf(int64(0)),
	"f32":  reflect.TypeOf(float32(0)),
	"f64":  reflect.TypeOf(float64(0)),
	"v128": reflect.TypeOf(V128{}),
}

// NewHostFunc builds a function reference for a host function of the given
// wasm signature ("i32", "i64", "f32", "f64", "v128") whose implementation
// is impl (nil = returns zero values). impl receives and returns the values
// as Go values.
func NewHostFunc(params, results []string, impl ...func(args []any) []any) *Func {
	in := []reflect.Type{reflect.TypeOf(0)}
	for _, p := range params {
		in = append(in, goTypes[p])
	}
	var out []reflect.Type
	for _, r := range results {
		out = append(out, goTypes[r])
	}
	ft := reflect.FuncOf(in, out, false)
	fn := reflect.MakeFunc(ft, func(args []reflect.Value) []reflect.Value {
		var res []any
		if len(impl) > 0 {
			var a []any
			for _, x := range args[1:] {
				a = append(a, x.Interface())
			}
			res = impl[0](a)
		}
		rv := make([]reflect.Value, len(out))
		for i, t := range out {
			if i < len(res) {
				rv[i] = reflect.ValueOf(res[i]).Convert(t)
			} else {
				rv[i] = reflect.Zero(t)
			}
		}
		return rv
	})
	return &Func{Type: FuncType(params, results), Fn: fn.Interface()}
}
