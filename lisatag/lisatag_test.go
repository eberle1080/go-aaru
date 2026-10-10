package lisatag

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"testing"
)

// randomTags returns n deterministic pseudo-random 24-byte tags. Some tags
// carry end-of-chain sentinels or small block numbers in their chain fields
// so the sentinel translation and lossless narrowing are exercised.
func randomTags(n int) [][]byte {
	r := rand.New(rand.NewPCG(0x6c697361, 0x746167))
	out := make([][]byte, n)
	for i := range out {
		b := make([]byte, PriamTagSize)
		for j := range b {
			b[j] = byte(r.Uint32())
		}
		switch i % 8 {
		case 0: // wide sentinels
			copy(b[14:20], []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
		case 1: // Sony sentinels with and without the valid flag
			copy(b[8:12], []byte{0x87, 0xFF, 0x07, 0xFF})
		case 2: // small block numbers that survive narrowing
			copy(b[14:20], []byte{0x00, 0x01, 0x23, 0x00, 0x07, 0xFE})
		}
		out[i] = b
	}
	return out
}

// Parsed fields sit at the documented offsets with the documented widths.
func TestParseLayout(t *testing.T) {
	for _, in := range randomTags(200) {
		s, err := ParseSonyTag(in)
		if err != nil {
			t.Fatal(err)
		}
		be := binary.BigEndian
		wantS := SonyTag{
			Version: be.Uint16(in[0:]), Kind: in[2] >> 6, Reserved: in[2] & 0x3F, Volume: in[3],
			FileID: int16(be.Uint16(in[4:])), RelPage: be.Uint16(in[6:]),
			NextBlock: be.Uint16(in[8:]) & 0x87FF, PrevBlock: be.Uint16(in[10:]) & 0x87FF,
		}
		if s != wantS {
			t.Fatalf("ParseSonyTag(%x) = %+v, want %+v", in, s, wantS)
		}

		p, err := ParseProfileTag(in)
		if err != nil {
			t.Fatal(err)
		}
		u24 := func(b []byte) uint32 { return be.Uint32(append([]byte{0}, b[:3]...)) }
		wantP := ProfileTag{
			Version: be.Uint16(in[0:]), Kind: in[2] >> 6, Reserved: in[2] & 0x3F, Volume: in[3],
			FileID: int16(be.Uint16(in[4:])), ValidChk: in[6]&0x80 != 0, UsedBytes: be.Uint16(in[6:]) & 0x7FFF,
			AbsPage: u24(in[8:]), Checksum: in[11], RelPage: be.Uint16(in[12:]),
			NextBlock: u24(in[14:]), PrevBlock: u24(in[17:]),
		}
		if p != wantP {
			t.Fatalf("ParseProfileTag(%x) = %+v, want %+v", in, p, wantP)
		}

		pr, err := ParsePriamTag(in)
		if err != nil {
			t.Fatal(err)
		}
		if pr.ToProfile() != p || pr.DiskSize != be.Uint32(in[20:]) {
			t.Fatalf("ParsePriamTag(%x) = %+v", in, pr)
		}
	}
}

// Bytes inverts Parse. ProFile and Priam layouts use every bit, so they
// round-trip exactly; Sony chain fields keep only the valid flag and the
// 11-bit block number.
func TestBytesInvertsParse(t *testing.T) {
	for _, in := range randomTags(200) {
		pr, _ := ParsePriamTag(in)
		if got := pr.Bytes(); !bytes.Equal(got, in) {
			t.Fatalf("Priam %x -> %x", in, got)
		}
		p, _ := ParseProfileTag(in)
		if got := p.Bytes(); !bytes.Equal(got, in[:20]) {
			t.Fatalf("Profile %x -> %x", in[:20], got)
		}
		s, _ := ParseSonyTag(in)
		want := bytes.Clone(in[:12])
		want[8] &= 0x87
		want[10] &= 0x87
		if got := s.Bytes(); !bytes.Equal(got, want) {
			t.Fatalf("Sony %x -> %x, want %x", in[:12], got, want)
		}
		// Parsing the re-encoded form gives the same struct.
		if s2, _ := ParseSonyTag(s.Bytes()); s2 != s {
			t.Fatalf("Sony struct round trip: %+v != %+v", s2, s)
		}
	}
}

func checkShared(t *testing.T, what string, v1, v2 uint16, k1, k2, r1, r2, vol1, vol2 uint8, f1, f2 int16, rp1, rp2 uint16) {
	t.Helper()
	if v1 != v2 || k1 != k2 || r1 != r2 || vol1 != vol2 || f1 != f2 || rp1 != rp2 {
		t.Fatalf("%s: shared fields differ", what)
	}
}

func widen(v uint16) uint32 {
	if v&0x7FF == 0x7FF {
		return 0xFFFFFF
	}
	return uint32(v & 0x7FF)
}

func narrow(v uint32) uint16 {
	if v == 0xFFFFFF {
		return 0x7FF
	}
	return uint16(v & 0x7FF)
}

// Conversions preserve the fields every format shares, translate the chain
// sentinels, zero the fields the source lacks, and are lossless where the
// target can hold every source field.
func TestConversions(t *testing.T) {
	for _, in := range randomTags(400) {
		s, _ := ParseSonyTag(in)
		p, _ := ParseProfileTag(in)
		pr, _ := ParsePriamTag(in)

		// Sony -> ProFile / Priam.
		sp := s.ToProfile()
		checkShared(t, "sony->profile", s.Version, sp.Version, s.Kind, sp.Kind, s.Reserved, sp.Reserved, s.Volume, sp.Volume, s.FileID, sp.FileID, s.RelPage, sp.RelPage)
		if sp.NextBlock != widen(s.NextBlock) || sp.PrevBlock != widen(s.PrevBlock) {
			t.Fatalf("sony->profile chain %+v -> %+v", s, sp)
		}
		if sp.ValidChk || sp.UsedBytes != 0 || sp.AbsPage != 0 || sp.Checksum != 0 {
			t.Fatalf("sony->profile: ProFile-only fields not zero: %+v", sp)
		}
		if spr := s.ToPriam(); spr != sp.ToPriam() || spr.DiskSize != 0 {
			t.Fatalf("sony->priam %+v != sony->profile->priam %+v", spr, sp.ToPriam())
		}
		// Sony -> ProFile -> Sony loses only the valid flag.
		want := s
		want.NextBlock &= 0x7FF
		want.PrevBlock &= 0x7FF
		if back := sp.ToSony(); back != want {
			t.Fatalf("sony->profile->sony %+v, want %+v", back, want)
		}
		if back := s.ToPriam().ToSony(); back != want {
			t.Fatalf("sony->priam->sony %+v, want %+v", back, want)
		}

		// ProFile -> Sony narrows the chain fields.
		ps := p.ToSony()
		checkShared(t, "profile->sony", p.Version, ps.Version, p.Kind, ps.Kind, p.Reserved, ps.Reserved, p.Volume, ps.Volume, p.FileID, ps.FileID, p.RelPage, ps.RelPage)
		if ps.NextBlock != narrow(p.NextBlock) || ps.PrevBlock != narrow(p.PrevBlock) {
			t.Fatalf("profile->sony chain %+v -> %+v", p, ps)
		}
		if pr.ToSony() != ps {
			t.Fatalf("priam->sony %+v != profile->sony %+v", pr.ToSony(), ps)
		}
		// Narrowing is lossless for block numbers below 0x7FF and for the
		// sentinel, when the ProFile-only fields are zero.
		if (p.NextBlock < 0x7FF || p.NextBlock == 0xFFFFFF) && (p.PrevBlock < 0x7FF || p.PrevBlock == 0xFFFFFF) {
			q := ProfileTag{Version: p.Version, Kind: p.Kind, Reserved: p.Reserved, Volume: p.Volume,
				FileID: p.FileID, RelPage: p.RelPage, NextBlock: p.NextBlock, PrevBlock: p.PrevBlock}
			if back := q.ToSony().ToProfile(); back != q {
				t.Fatalf("profile->sony->profile %+v, want %+v", back, q)
			}
		}

		// ProFile <-> Priam is lossless apart from DiskSize.
		if p.ToPriam().ToProfile() != p {
			t.Fatalf("profile->priam->profile not lossless: %+v", p)
		}
		if p.ToPriam().DiskSize != 0 {
			t.Fatal("profile->priam DiskSize not zero")
		}
		noSize := pr
		noSize.DiskSize = 0
		if pr.ToProfile().ToPriam() != noSize {
			t.Fatalf("priam->profile->priam lost more than DiskSize: %+v", pr)
		}
		if !bytes.Equal(p.ToPriam().Bytes()[:20], in[:20]) {
			t.Fatal("profile->priam bytes do not keep the shared 20-byte prefix")
		}
	}
}

// Convert performs the same conversions as the typed methods.
func TestConvertMatchesMethods(t *testing.T) {
	for _, in := range randomTags(200) {
		s, _ := ParseSonyTag(in)
		p, _ := ParseProfileTag(in)
		pr, _ := ParsePriamTag(in)
		cases := []struct {
			src  []byte
			to   Format
			want []byte
		}{
			{in[:12], FormatProfile, s.ToProfile().Bytes()},
			{in[:12], FormatPriam, s.ToPriam().Bytes()},
			{in[:20], FormatSony, p.ToSony().Bytes()},
			{in[:20], FormatPriam, p.ToPriam().Bytes()},
			{in[:24], FormatSony, pr.ToSony().Bytes()},
			{in[:24], FormatProfile, pr.ToProfile().Bytes()},
		}
		for _, c := range cases {
			out, err := Convert(c.src, c.to)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, c.want) {
				t.Fatalf("Convert(%x -> %v) = %x, want %x", c.src, c.to, out, c.want)
			}
			if len(out) != c.to.Size() {
				t.Fatalf("Convert -> %v returned %d bytes", c.to, len(out))
			}
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := []byte{
		0x12, 0x34, 0b10_101010, 0x07, 0xFF, 0xFE, 0x80, 0x10,
		0x01, 0x02, 0x03, 0xAB, 0x00, 0x05, 0xFF, 0xFF,
		0xFF, 0x00, 0x00, 0x09, 0xDE, 0xAD, 0xBE, 0xEF,
	}
	pr, err := ParsePriamTag(in)
	if err != nil {
		t.Fatal(err)
	}
	want := PriamTag{
		Version: 0x1234, Kind: 2, Reserved: 0x2A, Volume: 7, FileID: -2, ValidChk: true, UsedBytes: 0x0010,
		AbsPage: 0x010203, Checksum: 0xAB, RelPage: 5, NextBlock: 0xFFFFFF, PrevBlock: 9, DiskSize: 0xDEADBEEF,
	}
	if pr != want {
		t.Fatalf("ParsePriamTag = %+v, want %+v", pr, want)
	}
	if !bytes.Equal(pr.Bytes(), in) {
		t.Fatalf("Priam round trip: %x", pr.Bytes())
	}
	pf, _ := ParseProfileTag(in)
	if !bytes.Equal(pf.Bytes(), in[:20]) {
		t.Fatalf("Profile round trip: %x", pf.Bytes())
	}
	if pf.ToPriam().ToProfile() != pf {
		t.Fatal("Profile -> Priam -> Profile not lossless")
	}

	s, _ := ParseSonyTag(in)
	if s.NextBlock != 0x0102 || s.PrevBlock != 0x03AB {
		t.Fatalf("Sony chain fields: %+v", s)
	}
	// Sony -> Profile -> Sony preserves everything but the valid flag.
	s.NextBlock, s.PrevBlock = 0x87FF, 0x0123
	back := s.ToProfile().ToSony()
	if back.NextBlock != 0x07FF || back.PrevBlock != 0x0123 || back.FileID != s.FileID || back.Version != s.Version {
		t.Fatalf("Sony round trip through Profile: %+v", back)
	}
	if p := s.ToPriam(); p.NextBlock != 0xFFFFFF || p.PrevBlock != 0x123 || p.DiskSize != 0 {
		t.Fatalf("Sony -> Priam: %+v", p)
	}
}

func TestBytesMasksFields(t *testing.T) {
	s := SonyTag{Kind: 0xFF, Reserved: 0xFF, NextBlock: 0xFFFF, PrevBlock: 0x7800}
	if got := hex.EncodeToString(s.Bytes()); got != "0000ff000000000087ff0000" {
		t.Fatalf("Sony masked bytes = %s", got)
	}
	p := ProfileTag{UsedBytes: 0xFFFF, AbsPage: 0xFFFFFFFF, NextBlock: 0x01000000, PrevBlock: 0xFF123456}
	if got := hex.EncodeToString(p.Bytes()); got != "000000000000"+"7fff"+"ffffff"+"00"+"0000"+"000000"+"123456" {
		t.Fatalf("Profile masked bytes = %s", got)
	}
}

func TestConvertSameFormatIsVerbatim(t *testing.T) {
	// Bits 0x7800 of the Sony chain fields are not representable in SonyTag,
	// but same-format tags are copied verbatim by Convert.
	raw := []byte{0, 1, 2, 3, 4, 5, 6, 7, 0x7F, 0xFF, 0x78, 0x00}
	out, err := Convert(raw, FormatSony)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, raw) {
		t.Fatalf("Convert same format = %x, want %x", out, raw)
	}
	out[0] = 0xEE
	if raw[0] == 0xEE {
		t.Fatal("Convert must return a copy")
	}
	for _, n := range []int{20, 24} {
		raw := bytes.Repeat([]byte{0xC3}, n)
		f, _ := FormatForSize(n)
		if out, _ := Convert(raw, f); !bytes.Equal(out, raw) {
			t.Fatalf("Convert(%d bytes) same format not verbatim", n)
		}
	}
}

func TestErrors(t *testing.T) {
	if _, err := ParseSonyTag(make([]byte, 11)); !errors.Is(err, ErrShortTag) {
		t.Errorf("ParseSonyTag short: %v", err)
	}
	if _, err := ParseProfileTag(make([]byte, 19)); !errors.Is(err, ErrShortTag) {
		t.Errorf("ParseProfileTag short: %v", err)
	}
	if _, err := ParsePriamTag(make([]byte, 23)); !errors.Is(err, ErrShortTag) {
		t.Errorf("ParsePriamTag short: %v", err)
	}
	if _, err := ParsePriamTag(nil); !errors.Is(err, ErrShortTag) {
		t.Errorf("ParsePriamTag nil: %v", err)
	}
	for _, n := range []int{0, 11, 13, 21, 25, 512} {
		if _, err := Convert(make([]byte, n), FormatSony); !errors.Is(err, ErrInvalidTagSize) {
			t.Errorf("Convert(%d bytes): %v", n, err)
		}
	}
	for _, f := range []Format{0, 4, 255} {
		if _, err := Convert(make([]byte, 12), f); !errors.Is(err, ErrInvalidFormat) {
			t.Errorf("Convert to %v: %v", f, err)
		}
		if f.Size() != 0 {
			t.Errorf("%v.Size() = %d", f, f.Size())
		}
	}
	if FormatSony.Size() != 12 || FormatProfile.Size() != 20 || FormatPriam.Size() != 24 {
		t.Error("format sizes")
	}
	if FormatPriam.String() != "Priam" || Format(9).String() != "Format(9)" {
		t.Error("format names")
	}
}
