package cdecc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"testing"
)

// fillRandom fills b with deterministic pseudo-random bytes for seed.
func fillRandom(seed uint64, b []byte) {
	r := rand.New(rand.NewPCG(seed, 0x6364656363))
	for i := range b {
		b[i] = byte(r.Uint32())
	}
}

// randomSector returns a pseudo-random sector with a valid prefix for typ and
// lba and a regenerated suffix. For Mode 2 types the Form 2 flag of the
// subheader matches typ.
func randomSector(t testing.TB, typ TrackType, lba int64, seed uint64) []byte {
	t.Helper()
	sector := make([]byte, SectorSize)
	fillRandom(seed, sector)
	switch typ {
	case TrackTypeCdMode2Form1:
		sector[0x16] &^= 0x20
	case TrackTypeCdMode2Form2:
		sector[0x16] |= 0x20
	}
	if err := ReconstructPrefix(sector, typ, lba); err != nil {
		t.Fatal(err)
	}
	if err := Reconstruct(sector, typ); err != nil {
		t.Fatal(err)
	}
	return sector
}

// bitwiseEDC is a bit-at-a-time transcription of the EDC definition: a
// reflected CRC-32 with polynomial 0xD8018001 and no inversion.
func bitwiseEDC(edc uint32, data []byte) uint32 {
	for _, b := range data {
		edc ^= uint32(b)
		for range 8 {
			if edc&1 != 0 {
				edc = edc>>1 ^ 0xD8018001
			} else {
				edc >>= 1
			}
		}
	}
	return edc
}

// testLBAs covers the start of the disc, minute/second boundaries, the
// 99:59:74 limit and values outside the representable range.
var testLBAs = []int64{
	-1000000, -45150, -4651, -4650, -4501, -4500, -300, -225, -151, -150, -149, -1,
	0, 1, 74, 75, 149, 150, 4349, 4350, 4499, 4500, 269849, 269850, 359849, 359850,
	449849, 449850, 999999,
}

// LBAToMSF inverts the absolute position lba+150 = (m*60 + s)*75 + f for
// every LBA from -150 to the end of a 100 minute disc.
func TestLBAToMSFConsistent(t *testing.T) {
	for lba := int64(-150); lba < 100*60*75-150; lba++ {
		m, s, f := LBAToMSF(lba)
		if s >= 60 || f >= 75 {
			t.Fatalf("LBAToMSF(%d) = %d:%d:%d out of range", lba, m, s, f)
		}
		if got := (int64(m)*60+int64(s))*75 + int64(f) - 150; got != lba {
			t.Fatalf("LBAToMSF(%d) = %d:%d:%d maps back to %d", lba, m, s, f, got)
		}
	}
}

// ReconstructPrefix writes the sync pattern, the BCD encoding of LBAToMSF
// and the mode byte, copies the Mode 2 subheader, and touches nothing else.
func TestReconstructPrefixLayout(t *testing.T) {
	types := []TrackType{TrackTypeCdMode1, TrackTypeCdMode2Formless, TrackTypeCdMode2Form1, TrackTypeCdMode2Form2}
	for _, typ := range types {
		for _, lba := range testLBAs {
			sector := bytes.Repeat([]byte{0xA5}, SectorSize)
			copy(sector[0x14:], []byte{0x11, 0x22, 0x33, 0x44})
			if err := ReconstructPrefix(sector, typ, lba); err != nil {
				t.Fatalf("ReconstructPrefix(%d, %d): %v", typ, lba, err)
			}
			m, s, f := LBAToMSF(lba)
			want := append(bytes.Clone(syncPattern), bcd(int(m)), bcd(int(s)), bcd(int(f)))
			if typ == TrackTypeCdMode1 {
				want = append(want, 0x01, 0xA5, 0xA5, 0xA5, 0xA5)
			} else {
				want = append(want, 0x02, 0x11, 0x22, 0x33, 0x44)
			}
			want = append(want, 0x11, 0x22, 0x33, 0x44)
			if !bytes.Equal(sector[:24], want) {
				t.Errorf("prefix type=%d lba=%d:\n got %x\nwant %x", typ, lba, sector[:24], want)
			}
			if !bytes.Equal(sector[24:], bytes.Repeat([]byte{0xA5}, SectorSize-24)) {
				t.Errorf("prefix type=%d lba=%d modified bytes beyond 0x18", typ, lba)
			}
		}
	}
}

// ComputeEDC matches the bitwise CRC definition for any start value and
// length, and a block followed by its little-endian EDC has a zero residue.
func TestComputeEDCMatchesDefinition(t *testing.T) {
	buf := make([]byte, 4100)
	fillRandom(1, buf)
	for n := 0; n < 4096; n += 1 + n/16 {
		for _, init := range []uint32{0, 0xFFFFFFFF, 0x12345678} {
			if got, want := ComputeEDC(init, buf[3:3+n]), bitwiseEDC(init, buf[3:3+n]); got != want {
				t.Fatalf("n=%d init=%x: %08x != %08x", n, init, got, want)
			}
		}
	}
	for k := range 8 {
		for b := range 256 {
			in := make([]byte, 1+k)
			in[0] = byte(b)
			if got, want := edcTable[k][b], bitwiseEDC(0, in); got != want {
				t.Fatalf("edcTable[%d][%d] = %08x, want %08x", k, b, got, want)
			}
		}
	}
	block := buf[:100]
	withEDC := binary.LittleEndian.AppendUint32(bytes.Clone(block), ComputeEDC(0, block))
	if r := ComputeEDC(0, withEDC); r != 0 {
		t.Fatalf("residue %08x, want 0", r)
	}
}

// Reconstructed sectors of every type pass the check for their type, store
// the EDC where the layout says, and fail the check for the other mode.
func TestReconstructedSectorsValidate(t *testing.T) {
	for i, lba := range testLBAs {
		seed := uint64(i) + 100

		s1 := randomSector(t, TrackTypeCdMode1, lba, seed)
		if !IsSuffixCorrect(s1) || IsSuffixCorrectMode2(s1) {
			t.Errorf("mode1 lba %d: IsSuffixCorrect=%v IsSuffixCorrectMode2=%v", lba, IsSuffixCorrect(s1), IsSuffixCorrectMode2(s1))
		}
		if ComputeEDC(0, s1[:0x814]) != 0 {
			t.Errorf("mode1 lba %d: EDC residue over 0x000-0x813 is not zero", lba)
		}
		if !bytes.Equal(s1[0x814:0x81C], make([]byte, 8)) {
			t.Errorf("mode1 lba %d: reserved bytes not zero", lba)
		}

		f1 := randomSector(t, TrackTypeCdMode2Form1, lba, seed)
		if !IsSuffixCorrectMode2(f1) || IsSuffixCorrect(f1) {
			t.Errorf("form1 lba %d: IsSuffixCorrect=%v IsSuffixCorrectMode2=%v", lba, IsSuffixCorrect(f1), IsSuffixCorrectMode2(f1))
		}
		if ComputeEDC(0, f1[0x10:0x81C]) != 0 {
			t.Errorf("form1 lba %d: EDC residue over 0x010-0x81B is not zero", lba)
		}

		f2 := randomSector(t, TrackTypeCdMode2Form2, lba, seed)
		if ComputeEDC(0, f2[0x10:0x930]) != 0 {
			t.Errorf("form2 lba %d: EDC residue over 0x010-0x92F is not zero", lba)
		}
		// Form 2 has no ECC, so the ECC-based checks reject it.
		if IsSuffixCorrect(f2) || IsSuffixCorrectMode2(f2) {
			t.Errorf("form2 lba %d: unexpectedly passes an ECC check", lba)
		}
	}
}

// Every single-byte corruption anywhere in a Mode 1 sector is detected, and
// every corruption of the bytes after the header in a Mode 2 Form 1 sector
// is detected while header corruption is not (Mode 2 ECC uses a zero
// address).
func TestSingleByteCorruptionDetected(t *testing.T) {
	s1 := randomSector(t, TrackTypeCdMode1, 12345, 7)
	f1 := randomSector(t, TrackTypeCdMode2Form1, 12345, 8)
	for pos := range SectorSize {
		for _, x := range []byte{0x01, 0x80, 0xFF} {
			s1[pos] ^= x
			if IsSuffixCorrect(s1) {
				t.Fatalf("mode1: corruption 0x%02X at 0x%X not detected", x, pos)
			}
			s1[pos] ^= x

			f1[pos] ^= x
			if got, want := IsSuffixCorrectMode2(f1), pos < 0x10; got != want {
				t.Fatalf("form1: corruption 0x%02X at 0x%X: IsSuffixCorrectMode2=%v, want %v", x, pos, got, want)
			}
			f1[pos] ^= x
		}
	}
}

// Two corrupted bytes are also detected.
func TestDoubleByteCorruptionDetected(t *testing.T) {
	s1 := randomSector(t, TrackTypeCdMode1, 999, 9)
	r := rand.New(rand.NewPCG(9, 9))
	for range 2000 {
		a, b := r.IntN(SectorSize), r.IntN(SectorSize)
		if a == b {
			continue
		}
		c := bytes.Clone(s1)
		c[a] ^= byte(r.IntN(255) + 1)
		c[b] ^= byte(r.IntN(255) + 1)
		if IsSuffixCorrect(c) {
			t.Fatalf("corruption at 0x%X and 0x%X not detected", a, b)
		}
	}
}

// Reconstruct regenerates a damaged suffix exactly and is idempotent.
func TestReconstructRestoresSuffix(t *testing.T) {
	cases := []struct {
		typ          TrackType
		suffixStart  int
		suffixLength int
	}{
		{TrackTypeCdMode1, 0x810, SectorSize - 0x810},
		{TrackTypeCdMode2Form1, 0x818, SectorSize - 0x818},
		{TrackTypeCdMode2Form2, 0x92C, 4},
	}
	for _, c := range cases {
		want := randomSector(t, c.typ, 4242, 11)
		again := bytes.Clone(want)
		if err := Reconstruct(again, c.typ); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(again, want) {
			t.Errorf("type %d: Reconstruct is not idempotent", c.typ)
		}
		damaged := bytes.Clone(want)
		fillRandom(12, damaged[c.suffixStart:c.suffixStart+c.suffixLength])
		if err := Reconstruct(damaged, c.typ); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(damaged, want) {
			t.Errorf("type %d: Reconstruct did not restore the suffix", c.typ)
		}
	}
}

// The ECC is linear over GF(2): the parity of the XOR of two inputs is the
// XOR of their parities.
func TestECCLinear(t *testing.T) {
	addrA, addrB := make([]byte, 4), make([]byte, 4)
	dataA, dataB := make([]byte, SectorSize-0x10), make([]byte, SectorSize-0x10)
	fillRandom(20, addrA)
	fillRandom(21, addrB)
	fillRandom(22, dataA[:0x80C])
	fillRandom(23, dataB[:0x80C])
	if err := WriteSectorECC(addrA, dataA, dataA[0x80C:]); err != nil {
		t.Fatal(err)
	}
	if err := WriteSectorECC(addrB, dataB, dataB[0x80C:]); err != nil {
		t.Fatal(err)
	}
	addrX, dataX := make([]byte, 4), make([]byte, SectorSize-0x10)
	for i := range addrX {
		addrX[i] = addrA[i] ^ addrB[i]
	}
	for i := range 0x80C {
		dataX[i] = dataA[i] ^ dataB[i]
	}
	if err := WriteSectorECC(addrX, dataX, dataX[0x80C:]); err != nil {
		t.Fatal(err)
	}
	for i := 0x80C; i < len(dataX); i++ {
		if dataX[i] != dataA[i]^dataB[i] {
			t.Fatalf("ECC byte %d: %02x != %02x ^ %02x", i-0x80C, dataX[i], dataA[i], dataB[i])
		}
	}
	// The all-zero block has all-zero parity.
	zero := make([]byte, SectorSize-0x10)
	zero[0x900] = 0xEE // stale bytes in the ECC area are overwritten
	if err := WriteSectorECC(make([]byte, 4), zero, zero[0x80C:]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zero, make([]byte, SectorSize-0x10)) {
		t.Fatal("zero input produced non-zero ECC")
	}
}

// Pseudo-random sectors essentially never carry a valid suffix.
func TestRandomSectorsRejected(t *testing.T) {
	sector := make([]byte, SectorSize)
	for seed := range uint64(50) {
		fillRandom(1000+seed, sector)
		if IsSuffixCorrect(sector) || IsSuffixCorrectMode2(sector) {
			t.Errorf("random sector %d accepted", seed)
		}
	}
}

var syncPattern = []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

func bcd(v int) byte { return byte((v/10)<<4 | v%10) }

// buildMode1 builds a correct Mode 1 sector: hand-written sync + BCD header,
// constant user data, library reconstruction.
func buildMode1(t *testing.T, lba int64, fill byte) []byte {
	t.Helper()
	sector := make([]byte, SectorSize)
	copy(sector, syncPattern)
	abs := lba + 150
	sector[12] = bcd(int(abs / (60 * 75)))
	sector[13] = bcd(int((abs / 75) % 60))
	sector[14] = bcd(int(abs % 75))
	sector[15] = 0x01
	for i := 16; i < 16+2048; i++ {
		sector[i] = fill
	}
	if err := Reconstruct(sector, TrackTypeCdMode1); err != nil {
		t.Fatal(err)
	}
	return sector
}

func TestMode1ReconstructRoundTrip(t *testing.T) {
	for _, lba := range []int64{0, 1, 16, 4349, 333000} {
		sector := buildMode1(t, lba, byte(lba))
		if !IsSuffixCorrect(sector) {
			t.Fatalf("lba %d: reconstructed Mode 1 sector fails check", lba)
		}

		// The hand-built prefix equals ReconstructPrefix's.
		prefix := make([]byte, 16)
		if err := ReconstructPrefix(prefix, TrackTypeCdMode1, lba); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(prefix, sector[:16]) {
			t.Fatalf("lba %d: prefix %x != %x", lba, prefix, sector[:16])
		}

		// In Mode 1 every byte of the sector is covered by EDC, ECC or the
		// reserved-zero check: corrupting any of them must fail the check.
		for _, pos := range []int{0, 5, 0xC, 0xF, 0x10, 0x400, 0x80F, 0x810, 0x813, 0x814, 0x81B, 0x81C, 0x8C7, 0x8C8, 0x92F} {
			c := bytes.Clone(sector)
			c[pos] ^= 0x01
			if IsSuffixCorrect(c) {
				t.Errorf("lba %d: corruption at 0x%X not detected", lba, pos)
			}
		}
	}
}

func TestMode1ReservedMustBeZero(t *testing.T) {
	sector := buildMode1(t, 100, 0x42)
	sector[0x818] = 0x01 // reserved
	if IsSuffixCorrect(sector) {
		t.Fatal("non-zero reserved byte accepted")
	}
	if err := Reconstruct(sector, TrackTypeCdMode1); err != nil {
		t.Fatal(err)
	}
	if sector[0x818] != 0 || !IsSuffixCorrect(sector) {
		t.Fatal("Reconstruct did not clear reserved bytes")
	}
}

func TestIndividualECCPasses(t *testing.T) {
	sector := buildMode1(t, 77, 0x99)
	header, data := sector[0xC:0x10], sector[0x10:]
	if !CheckECC(header, data, sector[0x81C:], ECCP) || !CheckECC(header, data, sector[0x8C8:], ECCQ) {
		t.Fatal("valid sector fails P or Q")
	}
	// A byte of ECC Q is outside the P pass coverage.
	c := bytes.Clone(sector)
	c[0x8D0] ^= 0xFF
	if !CheckECC(c[0xC:0x10], c[0x10:], c[0x81C:], ECCP) || CheckECC(c[0xC:0x10], c[0x10:], c[0x8C8:], ECCQ) {
		t.Fatal("Q-only corruption should fail Q and pass P")
	}
	// A byte of ECC P is covered by Q (as data) and by P (as parity).
	c = bytes.Clone(sector)
	c[0x830] ^= 0xFF
	if CheckECC(c[0xC:0x10], c[0x10:], c[0x81C:], ECCP) || CheckECC(c[0xC:0x10], c[0x10:], c[0x8C8:], ECCQ) {
		t.Fatal("P parity corruption should fail both passes")
	}

	// WriteECC/WriteSectorECC regenerate exactly the same bytes.
	ecc := make([]byte, 276)
	if err := WriteSectorECC(header, data, ecc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ecc, sector[0x81C:0x930]) {
		t.Fatal("WriteSectorECC differs from Reconstruct")
	}
}

func TestMode2Form1(t *testing.T) {
	sector := make([]byte, SectorSize)
	fillRandom(99, sector)
	copy(sector[0x14:0x18], []byte{0x01, 0x02, 0x08, 0x00}) // form 1 subheader
	if err := ReconstructPrefix(sector, TrackTypeCdMode2Form1, 1234); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sector[0x10:0x14], sector[0x14:0x18]) || sector[0xF] != 0x02 {
		t.Fatal("Mode 2 prefix did not copy subheader / set mode")
	}
	if err := Reconstruct(sector, TrackTypeCdMode2Form1); err != nil {
		t.Fatal(err)
	}
	if !IsSuffixCorrectMode2(sector) {
		t.Fatal("reconstructed Form 1 sector fails check")
	}
	// The header is not covered by Mode 2 EDC/ECC.
	for _, pos := range []int{0, 0xB, 0xC, 0xE, 0xF} {
		c := bytes.Clone(sector)
		c[pos] ^= 0xFF
		if !IsSuffixCorrectMode2(c) {
			t.Errorf("header corruption at 0x%X should not affect Mode 2 check", pos)
		}
	}
	for _, pos := range []int{0x10, 0x12, 0x18, 0x817, 0x818, 0x81B, 0x81C, 0x92F} {
		c := bytes.Clone(sector)
		c[pos] ^= 0x04
		if IsSuffixCorrectMode2(c) {
			t.Errorf("corruption at 0x%X not detected", pos)
		}
	}
}

// TestMode2Form2EDC checks Form 2 sector handling: the EDC over subheader +
// user data (0x10, 0x91C bytes) is stored at 0x92C.
// IsSuffixCorrectMode2 also checks ECC P/Q, which Form 2
// sectors do not have, so it rejects them; EDC is checked directly instead.
func TestMode2Form2EDC(t *testing.T) {
	sector := make([]byte, SectorSize)
	if err := ReconstructPrefix(sector, TrackTypeCdMode2Form2, 7); err != nil {
		t.Fatal(err)
	}
	copy(sector[0x10:0x18], []byte{0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x20, 0x00})
	for i := 0x18; i < 0x18+2324; i++ {
		sector[i] = 0x55
	}
	edc := ComputeEDC(0, sector[0x10:0x10+0x91C])
	binary.LittleEndian.PutUint32(sector[0x92C:], edc)

	c := bytes.Clone(sector)
	if err := Reconstruct(c, TrackTypeCdMode2Form2); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c, sector) {
		t.Fatal("Reconstruct(Form2) should only (re)write the EDC")
	}
	if edc == 0 {
		t.Fatal("EDC unexpectedly zero")
	}
	if IsSuffixCorrectMode2(sector) {
		t.Fatal("Form 2 sector unexpectedly passes the ECC-based Mode 2 check")
	}
}

func TestLBAToMSF(t *testing.T) {
	cases := []struct {
		lba     int64
		m, s, f uint8
	}{
		{-150, 0, 0, 0},
		{-1, 0, 1, 74},
		{0, 0, 2, 0},
		{74, 0, 2, 74},
		{75, 0, 3, 0},
		{4350, 1, 0, 0},
		{449849, 99, 59, 74},
		{449850, 100, 0, 0},
		{-151, 0, 0, 255}, // (-1 % 75) truncated to uint8
	}
	for _, c := range cases {
		m, s, f := LBAToMSF(c.lba)
		if m != c.m || s != c.s || f != c.f {
			t.Errorf("LBAToMSF(%d) = %d:%d:%d, want %d:%d:%d", c.lba, m, s, f, c.m, c.s, c.f)
		}
	}
}

func simpleEDC(edc uint32, data []byte) uint32 {
	for _, b := range data {
		edc = edc>>8 ^ edcTable[0][byte(edc)^b]
	}
	return edc
}

func TestComputeEDCMatchesBytewise(t *testing.T) {
	buf := make([]byte, 3000)
	fillRandom(5, buf)
	for n := range 70 {
		for _, init := range []uint32{0, 0xFFFFFFFF, 0x12345678} {
			if got, want := ComputeEDC(init, buf[n:n*37]), simpleEDC(init, buf[n:n*37]); got != want {
				t.Fatalf("n=%d init=%x: %08x != %08x", n, init, got, want)
			}
		}
	}
	// Incremental computation equals one-shot.
	whole := ComputeEDC(0, buf)
	inc := uint32(0)
	for i := 0; i < len(buf); i += 333 {
		inc = ComputeEDC(inc, buf[i:min(i+333, len(buf))])
	}
	if inc != whole {
		t.Fatalf("incremental %08x != whole %08x", inc, whole)
	}
	if ComputeEDC(0, nil) != 0 {
		t.Fatal("EDC of nothing should be 0")
	}
}

func TestErrorsAndShortBuffers(t *testing.T) {
	short := make([]byte, SectorSize-1)
	if IsSuffixCorrect(short) || IsSuffixCorrectMode2(short) || IsSuffixCorrect(nil) || IsSuffixCorrectMode2(nil) {
		t.Fatal("short sector accepted")
	}
	if err := Reconstruct(short, TrackTypeCdMode1); !errors.Is(err, ErrShortSector) {
		t.Fatalf("Reconstruct short: %v", err)
	}
	for _, tt := range []TrackType{TrackTypeAudio, TrackTypeData, TrackTypeCdMode2Formless, 6, 255} {
		sector := bytes.Repeat([]byte{0x5A}, SectorSize)
		if err := Reconstruct(sector, tt); !errors.Is(err, ErrUnsupportedTrackType) {
			t.Errorf("Reconstruct(%d): %v", tt, err)
		}
		if tt != TrackTypeCdMode2Formless {
			if err := ReconstructPrefix(sector, tt, 0); !errors.Is(err, ErrUnsupportedTrackType) {
				t.Errorf("ReconstructPrefix(%d): %v", tt, err)
			}
		}
		if !bytes.Equal(sector, bytes.Repeat([]byte{0x5A}, SectorSize)) {
			t.Errorf("type %d: sector modified on error", tt)
		}
	}
	if err := ReconstructPrefix(make([]byte, 15), TrackTypeCdMode1, 0); !errors.Is(err, ErrShortSector) {
		t.Errorf("ReconstructPrefix short mode1: %v", err)
	}
	if err := ReconstructPrefix(make([]byte, 16), TrackTypeCdMode1, 0); err != nil {
		t.Errorf("ReconstructPrefix 16-byte mode1: %v", err)
	}
	if err := ReconstructPrefix(make([]byte, 23), TrackTypeCdMode2Form1, 0); !errors.Is(err, ErrShortSector) {
		t.Errorf("ReconstructPrefix short mode2: %v", err)
	}

	sector := buildMode1(t, 3, 3)
	if CheckECC(sector[0xC:0x10], sector[0x10:0x100], sector[0x81C:], ECCP) {
		t.Error("CheckECC with short data accepted")
	}
	if CheckECC(sector[0xC:0x10], sector[0x10:], sector[0x81C:0x81C+171], ECCP) {
		t.Error("CheckECC with short ecc accepted")
	}
	if err := WriteECC(sector[0xC:0x10], sector[0x10:], make([]byte, 10), ECCQ); !errors.Is(err, ErrInvalidECCParams) {
		t.Errorf("WriteECC short ecc: %v", err)
	}
	if err := WriteSectorECC(sector[0xC:0x10], sector[0x10:], make([]byte, 200)); !errors.Is(err, ErrInvalidECCParams) {
		t.Errorf("WriteSectorECC short ecc: %v", err)
	}
	for _, p := range []ECCParams{{}, {MajorCount: 4, MinorCount: 4, MajorMult: 100, MinorInc: 1}, {MajorCount: 2, MinorCount: 2, MajorMult: 1, MinorInc: 5}, {MajorCount: -1, MinorCount: 3}} {
		if CheckECC(make([]byte, 4), make([]byte, 4096), make([]byte, 4096), p) {
			t.Errorf("CheckECC accepted invalid params %+v", p)
		}
		if err := WriteECC(make([]byte, 4), make([]byte, 4096), make([]byte, 4096), p); !errors.Is(err, ErrInvalidECCParams) {
			t.Errorf("WriteECC(%+v): %v", p, err)
		}
	}
}

func BenchmarkReconstructMode1(b *testing.B) {
	sector := make([]byte, SectorSize)
	fillRandom(1, sector)
	b.SetBytes(SectorSize)
	for b.Loop() {
		_ = Reconstruct(sector, TrackTypeCdMode1)
	}
}

func BenchmarkIsSuffixCorrect(b *testing.B) {
	sector := make([]byte, SectorSize)
	fillRandom(1, sector)
	_ = ReconstructPrefix(sector, TrackTypeCdMode1, 0)
	_ = Reconstruct(sector, TrackTypeCdMode1)
	b.SetBytes(SectorSize)
	for b.Loop() {
		if !IsSuffixCorrect(sector) {
			b.Fatal("bad sector")
		}
	}
}

func BenchmarkComputeEDC(b *testing.B) {
	buf := make([]byte, 0x91C)
	b.SetBytes(int64(len(buf)))
	for b.Loop() {
		ComputeEDC(0, buf)
	}
}
