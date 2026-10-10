package aaru

import (
	"encoding/binary"
	"testing"
)

// TestStructSizes checks that the Go on-disk structures have exactly the
// packed sizes the on-disk format requires.
func TestStructSizes(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want int
	}{
		{"Header", Header{}, headerSize},
		{"BlockHeader", BlockHeader{}, blockHeaderSize},
		{"GeometryBlockHeader", GeometryBlockHeader{}, geometryBlockHeaderSize},
		{"DDTHeader", DDTHeader{}, ddtHeaderSize},
		{"DDTHeader2", DDTHeader2{}, ddtHeader2Size},
		{"IndexHeader", IndexHeader{}, indexHeaderSize},
		{"IndexHeader2", IndexHeader2{}, indexHeader2Size},
		{"IndexHeader3", IndexHeader3{}, indexHeader3Size},
		{"IndexEntry", IndexEntry{}, indexEntrySize},
		{"MetadataBlockHeader", MetadataBlockHeader{}, metadataBlockHeaderSize},
		{"CICMBlockHeader", CICMBlockHeader{}, cicmBlockHeaderSize},
		{"JSONBlockHeader", JSONBlockHeader{}, jsonBlockHeaderSize},
		{"TracksHeader", TracksHeader{}, tracksHeaderSize},
		{"TrackEntry", TrackEntry{}, trackEntrySize},
		{"ChecksumHeader", ChecksumHeader{}, checksumHeaderSize},
		{"ChecksumEntry", ChecksumEntry{}, checksumEntrySize},
		{"DumpHardwareHeader", DumpHardwareHeader{}, dumpHardwareHeaderSize},
		{"DumpHardwareEntry", DumpHardwareEntry{}, dumpHardwareEntrySize},
		{"DumpExtent", DumpExtent{}, dumpExtentSize},
		{"TapeFileHeader", TapeFileHeader{}, tapeFileHeaderSize},
		{"TapeFileEntry", TapeFileEntry{}, tapeFileEntrySize},
		{"TapePartitionHeader", TapePartitionHeader{}, tapePartitionHeaderSize},
		{"TapePartitionEntry", TapePartitionEntry{}, tapePartitionEntrySize},
		{"FluxHeader", FluxHeader{}, fluxHeaderSize},
		{"FluxEntry", FluxEntry{}, fluxEntrySize},
		{"DataStreamPayloadHeader", DataStreamPayloadHeader{}, dataStreamPayloadHdrSize},
		{"DPMHeader", DPMHeader{}, dpmHeaderSize},
		{"DPMEntry", DPMEntry{}, dpmEntrySize},
		{"DPMCalibration", DPMCalibration{}, dpmCalibrationSize},
		{"ErasureCodingMapHeader", ErasureCodingMapHeader{}, ecMapHeaderSize},
		{"StripeGroupDescriptor", StripeGroupDescriptor{}, stripeGroupDescriptorSize},
		{"StripeDataBlockEntry", StripeDataBlockEntry{}, stripeDataBlockEntrySize},
		{"StripeParityBlockEntry", StripeParityBlockEntry{}, stripeParityBlockEntrySize},
		{"RecoveryFooter", RecoveryFooter{}, recoveryFooterSize},
	}
	for _, c := range cases {
		if got := binary.Size(c.v); got != c.want {
			t.Errorf("%s: size %d, want %d", c.name, got, c.want)
		}
	}
	if s := 8 + HeaderAppNameLen + 4 + 4 + 8 + 8 + 8; s != headerV1Size {
		t.Errorf("v1 header size %d, want %d", s, headerV1Size)
	}
}

func TestBlockHeaderFastPath(t *testing.T) {
	h := BlockHeader{BlockTypeDataBlock, DataTypeCdSubchannel, CompressionLzmaCst, 96, 1234, 5678, 0x0102030405060708, 0x1112131415161718}
	b := h.appendTo(nil)
	if string(b) != string(marshal(h)) {
		t.Fatalf("fast encoding differs from reflective encoding")
	}
	got, err := decodeBlockHeader(b)
	if err != nil || got != h {
		t.Fatalf("decode = %+v, %v", got, err)
	}
	if _, err := decodeBlockHeader(b[:10]); err == nil {
		t.Fatal("expected error on short buffer")
	}
}
