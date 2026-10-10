package main

import "github.com/eberle1080/go-aaru"

// cmdIdentify implements "identify <filename>".
func cmdIdentify(args []string) int {
	pos, ok := newVerbArgs("identify", usageIdentify).parse(args, 1, 1)
	if !ok {
		return -1
	}
	return withCoreOutput(func(o *coreOutput) int { return identify(o, pos[0]) })
}

// identify reports whether path is an AaruFormat image: exit status 0 if it
// is, 1 if it is not, the (positive) system error number if it cannot be
// read. A missing file is reported as an error, not as "not enough
// confidence".
func identify(o *coreOutput, path string) int {
	ret, err := aaru.Identify(path)
	if err != nil {
		code := coreOpenErrno(err)
		o.printf("Error %d while trying to identify file.\n", code)
		return code
	}
	switch ret {
	case 0:
		o.printf("The specified file is not a support AaruFormat image.\n")
		return 1
	case 100:
		o.printf("The specified file is a supported AaruFormat image.\n")
		return 0
	default:
		o.printf("The library has not enough confidence this is an AaruFormat image, and this shall NEVER happen.\n")
		return 2
	}
}
