package main

import "fmt"

// Usage texts of the core verbs.

func usageIdentify() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool identify <filename>\n\n")
	fmt.Printf("Identifies if the file is a supported AaruFormat image.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <filename>       Path to the image file.\n")
}

func usageInfo() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool info <filename>\n\n")
	fmt.Printf("Prints metadata about the image.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <filename>       Path to AaruFormat image.\n")
}

func usageRead() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool read <sector_number> <filename>\n\n")
	fmt.Printf("Reads a sector and prints it out.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <sector_number>  Sector number to read.\n")
	fmt.Printf("  <filename>       Path to image file.\n")
}

func usageReadLong() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool read_long <sector_number> <filename>\n\n")
	fmt.Printf("Reads sector with all metadata and prints it out.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <sector_number>  Sector number to read.\n")
	fmt.Printf("  <filename>       Path to image file.\n")
}

func usageVerify() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool verify [--recover] <filename>\n\n")
	fmt.Printf("Checks block-level integrity.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <filename>       Path to image file.\n")
	fmt.Printf("Options:\n")
	fmt.Printf("  --recover        Attempt erasure coding recovery of corrupt blocks.\n")
	fmt.Printf("                   Reads all sectors, transparently recovering any corrupt\n")
	fmt.Printf("                   blocks via RS parity. Reports recoverable vs unrecoverable.\n")
}

func usageVerifySectors() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool verify_sectors <filename>\n\n")
	fmt.Printf("Checks sector-level integrity.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <filename>       Path to image file.\n")
}

func usageCompare() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool compare <filename1> <filename2>\n\n")
	fmt.Printf("Compares two AaruFormat images.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <filename1>      Path to first image file.\n")
	fmt.Printf("  <filename2>      Path to second image file.\n")
}

func usageCLICompare() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool cli-compare [-l] <filename1> <filename2>\n\n")
	fmt.Printf("Compares two AaruFormat images sector by sector and lists all different sectors.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <filename1>      Path to first image file.\n")
	fmt.Printf("  <filename2>      Path to second image file.\n")
	fmt.Printf("Options:\n")
	fmt.Printf("  -l               Use long sector read (includes tags and metadata).\n")
}

func usageConvert() {
	fmt.Printf("\nUsage:\n")
	fmt.Printf("  aaruformattool convert [-l] [--erasure-coding=K,M] <input> <output>\n\n")
	fmt.Printf("Converts an AaruFormat image by reading all sectors from input and writing them to output.\n")
	fmt.Printf("Arguments:\n")
	fmt.Printf("  <input>          Path to input image file.\n")
	fmt.Printf("  <output>         Path to output image file.\n")
	fmt.Printf("Options:\n")
	fmt.Printf("  -l               Use long sector read/write (includes tags and metadata).\n")
	fmt.Printf("  --erasure-coding=K,M\n")
	fmt.Printf("                   Enable Reed-Solomon erasure coding with K data blocks and\n")
	fmt.Printf("                   M parity blocks per stripe. Examples:\n")
	fmt.Printf("                     --erasure-coding=16,1  Low overhead (~6%%), single fault tolerance\n")
	fmt.Printf("                     --erasure-coding=8,2   Medium overhead (~12%%), double fault tolerance\n")
	fmt.Printf("                     --erasure-coding=4,4   High overhead (~25%%), quad fault tolerance\n")
}
