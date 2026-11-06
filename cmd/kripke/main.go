// Command kripke checks Go models written with package fv.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:]))
}
