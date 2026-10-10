// Command aaruformattool inspects, verifies, compares, converts and repairs
// AaruFormat images.
//
// Usage:
//
//	aaruformattool <verb> [arguments]
//
// Run "aaruformattool" without arguments for the list of verbs, and
// "aaruformattool <verb> --help" for help with a verb.
package main

import (
	"fmt"
	"os"

	"github.com/eberle1080/go-aaru"
)

// Tool version.
const (
	toolMajorVersion = 1
	toolMinorVersion = 0
)

// command is a verb handler; it receives the arguments after the verb and
// returns the process exit status.
type command struct {
	verb        string
	description string
	run         func(args []string) int
}

// commands lists the verbs in the order they appear in the usage text.
var commands = []command{
	{"cli-compare", "Compares two AaruFormat images sector by sector (CLI mode).", cmdCLICompare},
	{"compare", "Compares two AaruFormat images.", cmdCompare},
	{"convert", "Converts an AaruFormat image to another AaruFormat image.", cmdConvert},
	{"convert-ps3", "Converts a PS3 disc image to AaruFormat with decrypted storage.", cmdConvertPS3},
	{"convert-wiiu", "Converts a Wii U disc image to AaruFormat with decrypted storage.", cmdConvertWiiU},
	{"convert-ngcw", "Converts a GameCube/Wii disc image to AaruFormat.", cmdConvertNGCW},
	{"identify", "Identifies if the indicated file is a supported AaruFormat image.", cmdIdentify},
	{"info", "Prints information about a given AaruFormat image.", cmdInfo},
	{"inject-media-tag", "Injects a media tag into an AaruFormat image.", cmdInjectMediaTag},
	{"read", "Reads a sector and prints it out on screen.", cmdRead},
	{"read_long", "Reads a sector with all its prefixes and suffixes.", cmdReadLong},
	{"repair-cd-arena", "Repairs CD sector prefix/suffix arenas damaged by the resume bug.", cmdRepairCDArena},
	{"upgrade-ddt-to-alpha21", "Upgrades a DDT image to alpha21 format.", cmdUpgradeDDTToAlpha21},
	{"verify", "Verifies the integrity of blocks in an image.", cmdVerify},
	{"verify_sectors", "Verifies the integrity of all sectors in an image.", cmdVerifySectors},
}

func main() {
	os.Exit(run(os.Args[1:]))
}

// run dispatches args[0] to its verb; it is separate from main for tests.
func run(args []string) int {
	printBanner()
	if len(args) < 1 {
		usage()
		return -1
	}
	for _, c := range commands {
		if c.verb == args[0] {
			return c.run(args[1:])
		}
	}
	fmt.Fprintf(os.Stderr, "Unknown verb: %s\n", args[0])
	usage()
	return -1
}

func printBanner() {
	fmt.Printf("AaruFormat Tool version %d.%d\n", toolMajorVersion, toolMinorVersion)
	fmt.Printf("go-aaru version %d.%d\n\n", aaru.LibraryMajorVersion, aaru.LibraryMinorVersion)
}

func usage() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool <verb> [arguments]\n\n")
	fmt.Printf("Available verbs:\n")
	for _, c := range commands {
		fmt.Printf("  %-22s %s\n", c.verb, c.description)
	}
	fmt.Printf("\nFor help with any verb, run:\n")
	fmt.Printf("  aaruformattool <verb> --help\n")
}
