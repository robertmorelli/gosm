// Command dartfib runs the Dart program examples/dart/fibapp natively in Go.
package main

import (
	"fmt"
	"os"

	"gosm/examples/dart/fibapp"
	"gosm/rt"
)

func main() {
	app, err := fibapp.New(fibapp.NewDartHost())
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
