package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// verbArgs parses a verb's arguments: options may appear before, between or
// after positional arguments. Options accept both "-x" and "--long" spellings
// (stdlib flag semantics).
type verbArgs struct {
	fs    *flag.FlagSet
	usage func()
}

// newVerbArgs creates a parser for verb; usage prints the verb's help text.
func newVerbArgs(verb string, usage func()) *verbArgs {
	fs := flag.NewFlagSet(verb, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return &verbArgs{fs: fs, usage: usage}
}

// parse parses args and returns the positional arguments. It returns ok=false
// (after printing the error and the usage text) when parsing fails, when
// --help/-h is given, or when the number of positional arguments is outside
// [minPos, maxPos] (maxPos < 0 means unbounded).
func (v *verbArgs) parse(args []string, minPos, maxPos int) (pos []string, ok bool) {
	for {
		if err := v.fs.Parse(args); err != nil {
			if err != flag.ErrHelp {
				fmt.Fprintf(os.Stderr, "%s: %v\n", v.fs.Name(), err)
			}
			v.usage()
			return nil, false
		}
		rest := v.fs.Args()
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	if len(pos) < minPos || (maxPos >= 0 && len(pos) > maxPos) {
		fmt.Fprintf(os.Stderr, "%s: wrong number of arguments\n", v.fs.Name())
		v.usage()
		return nil, false
	}
	return pos, true
}
