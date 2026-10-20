// Command hello demonstrates using a gosm-generated package.
package main

import (
	"fmt"

	"gosm/examples/hello/greeter"
)

// host implements greeter.Host.
type host struct{}

func (host) M_env_log(s any) { fmt.Println("wasm says:", s) }

func main() {
	g, err := greeter.New(host{})
	if err != nil {
		panic(err)
	}
	n := g.Greet("gopher 😀")
	fmt.Println("length in UTF-16 code units:", n)

	p := g.NewPoint(3, 4)       // a plain *greeter.Point
	p.X = 10                    // ordinary Go field access
	fmt.Println(g.Manhattan(p)) // 14
}
