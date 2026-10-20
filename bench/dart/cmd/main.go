// Command dbench runs the Dart benchmark.
package main

import (
	"fmt"
	"os"

	"gosm/bench/dart/dbench"
	"gosm/rt"
)

func main() {
	app, err := dbench.New(dbench.NewDartHost())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := rt.Catch(func() {
		app.SetThisModule(nil)
		app.InvokeMain(nil)
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
