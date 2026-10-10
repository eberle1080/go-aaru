package hashtest

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"math/rand/v2"
	"strings"
	"testing"
)

// Standard test vectors: RFC 1321 (MD5) and FIPS 180 (SHA-1, SHA-256).
var vectors = []struct {
	name string
	new  func() hash.Hash
	in   string
	want string
}{
	{"md5", md5.New, "", "d41d8cd98f00b204e9800998ecf8427e"},
	{"md5", md5.New, "a", "0cc175b9c0f1b6a831c399e269772661"},
	{"md5", md5.New, "abc", "900150983cd24fb0d6963f7d28e17f72"},
	{"md5", md5.New, "message digest", "f96b697d7cb7938d525a2f31aaf161d0"},
	{"md5", md5.New, "abcdefghijklmnopqrstuvwxyz", "c3fcd3d76192e4007dfb496cca67e13b"},
	{"sha1", sha1.New, "", "da39a3ee5e6b4b0d3255bfef95601890afd80709"},
	{"sha1", sha1.New, "abc", "a9993e364706816aba3e25717850c26c9cd0d89d"},
	{"sha1", sha1.New, "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq", "84983e441c3bd26ebaae4aa1f95129e5e54670f1"},
	{"sha256", sha256.New, "", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	{"sha256", sha256.New, "abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	{"sha256", sha256.New, "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq", "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1"},
}

// Standard vectors for one million repetitions of 'a' (FIPS 180).
var millionA = []struct {
	name string
	new  func() hash.Hash
	want string
}{
	{"sha1", sha1.New, "34aa973cd4c4daa4f61eeb2bdbad27316534016f"},
	{"sha256", sha256.New, "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"},
}

var hashes = []struct {
	name string
	new  func() hash.Hash
}{
	{"md5", md5.New},
	{"sha1", sha1.New},
	{"sha256", sha256.New},
}

// testData returns n deterministic pseudo-random bytes.
func testData(n int) []byte {
	r := rand.New(rand.NewPCG(0x68617368, 0x74657374))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func sum(h hash.Hash, data []byte) []byte {
	h.Write(data)
	return h.Sum(nil)
}

func TestStandardVectorsOneShot(t *testing.T) {
	for _, v := range vectors {
		if got := hex.EncodeToString(sum(v.new(), []byte(v.in))); got != v.want {
			t.Errorf("%s(%q) = %s, want %s", v.name, v.in, got, v.want)
		}
	}
}

// One byte per update.
func TestStandardVectorsBytewise(t *testing.T) {
	for _, v := range vectors {
		h := v.new()
		for _, c := range []byte(v.in) {
			h.Write([]byte{c})
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != v.want {
			t.Errorf("%s(%q) bytewise = %s, want %s", v.name, v.in, got, v.want)
		}
	}
}

func TestMillionA(t *testing.T) {
	data := []byte(strings.Repeat("a", 1000000))
	for _, v := range millionA {
		if got := hex.EncodeToString(sum(v.new(), data)); got != v.want {
			t.Errorf("%s(1M x 'a') = %s, want %s", v.name, got, v.want)
		}
	}
}

// The one-shot helpers agree with the streaming hash.Hash values.
func TestOneShotHelpersMatchHash(t *testing.T) {
	data := testData(1 << 20)
	m := md5.Sum(data)
	s1 := sha1.Sum(data)
	s256 := sha256.Sum256(data)
	for _, c := range []struct {
		name string
		got  []byte
		h    hash.Hash
	}{{"md5", m[:], md5.New()}, {"sha1", s1[:], sha1.New()}, {"sha256", s256[:], sha256.New()}} {
		if want := sum(c.h, data); !bytes.Equal(c.got, want) {
			t.Errorf("%s: one-shot %x, hash.Hash %x", c.name, c.got, want)
		}
	}
}

// Streaming in any chunk size equals the one-shot digest over a 1 MiB
// pseudo-random buffer.
func TestStreamingMatchesOneShot(t *testing.T) {
	data := testData(1 << 20)
	for _, hs := range hashes {
		want := sum(hs.new(), data)
		for _, chunk := range []int{1, 63, 64, 65, 2352, 8192, 65537} {
			h := hs.new()
			for pos := 0; pos < len(data); pos += chunk {
				h.Write(data[pos:min(pos+chunk, len(data))])
			}
			if got := h.Sum(nil); !bytes.Equal(got, want) {
				t.Errorf("%s chunk %d: got %x, want %x", hs.name, chunk, got, want)
			}
		}
	}
}

// Sum does not change the state, and Reset restores the initial state.
func TestSumAndReset(t *testing.T) {
	data := testData(10000)
	for _, hs := range hashes {
		h := hs.new()
		h.Write(data[:5000])
		h.Sum(nil)
		h.Write(data[5000:])
		if got, want := h.Sum(nil), sum(hs.new(), data); !bytes.Equal(got, want) {
			t.Errorf("%s: Sum altered the state", hs.name)
		}
		h.Reset()
		if got, want := h.Sum(nil), sum(hs.new(), nil); !bytes.Equal(got, want) {
			t.Errorf("%s: Reset did not restore the initial state", hs.name)
		}
	}
}
