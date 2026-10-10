package main

import (
	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// Raw-value adapters over the name tables (names_gen.go) for the
// maintenance verbs, which work on undecoded on-disk fields.

func maintMediaTypeString(t uint32) string   { return mediaTypeString(media.Type(t)) }
func maintDataTypeString(t uint16) string    { return dataTypeString(aaru.DataType(t)) }
func maintMediaTagTypeString(t int32) string { return mediaTagTypeString(aaru.MediaTagType(t)) }
