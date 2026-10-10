package aaru

import "testing"

func TestParseOptions(t *testing.T) {
	o := ParseOptions("")
	if o != DefaultOptions() {
		t.Fatalf("empty options = %+v", o)
	}
	o = ParseOptions("compress=false;deduplicate=false;dictionary=0;table_shift=5;data_shift=0;block_alignment=12;md5=true;sha1=true;sha256=true;blake3=true;spamsum=true;zstd=true;zstd_level=99;threads=0;bogus=1;novalue")
	want := Options{Compress: false, Deduplicate: false, Dictionary: 33554432, TableShift: 5, DataShift: 12,
		BlockAlignment: 12, MD5: true, SHA1: true, SHA256: true, Blake3: true, SpamSum: true, Zstd: true,
		ZstdLevel: 22, Threads: 1}
	if o != want {
		t.Fatalf("got %+v\nwant %+v", o, want)
	}
	if o := ParseOptions("table_shift=-1"); o.TableShift != -1 || o.explicitTableShift() {
		t.Fatalf("table_shift=-1: %+v", o)
	}
	if o := ParseOptions("table_shift=abc"); o.explicitTableShift() {
		t.Fatal("invalid table_shift should be ignored")
	}
	if o := ParseOptions("table_shift=0"); !o.explicitTableShift() || o.TableShift != 0 {
		t.Fatal("table_shift=0 should be explicit")
	}
	if o := ParseOptions("zstd_level=0;threads=8"); o.ZstdLevel != 1 || o.Threads != 8 {
		t.Fatalf("clamping: %+v", o)
	}
	rt := ParseOptions(want.String())
	if rt.Compress != want.Compress || rt.ZstdLevel != want.ZstdLevel || rt.BlockAlignment != want.BlockAlignment {
		t.Fatalf("String round trip: %+v", rt)
	}
}
