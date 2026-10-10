package main

import (
	"fmt"
	"os"
)

func notImplemented(verb string) int {
	fmt.Fprintf(os.Stderr, "%s: not implemented\n", verb)
	return -1
}
