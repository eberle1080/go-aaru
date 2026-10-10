package main

// Snapshot tests of the console converters (convert-ps3, convert-wiiu and
// convert-ngcw): the media tags written for synthetic discs are compared with
// testdata/expected/console/<scenario>_tags.txt (rewritten with -update).

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/console/ps3"
	"github.com/eberle1080/go-aaru/media"
)

// conRunRaw runs a converter, capturing its output verbatim.
func conRunRaw(fn func(o *conOut) int) conResult {
	var out, errOut bytes.Buffer
	o := newConOut(&out, &errOut)
	code := fn(o)
	o.flush()
	return conResult{code: code, stdout: out.String(), stderr: errOut.String()}
}

// conScenario is a synthetic disc converted by one of the console converters.
type conScenario struct {
	name  string
	disc  []byte // expected disc contents
	input string // input file
	conv  func(o *conOut, in, out string) int
	mt    media.Type
}

// conBuildScenarios writes the synthetic inputs of the media tag tests to dir.
func conBuildScenarios(t *testing.T, dir string) []conScenario {
	t.Helper()
	put := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		conWriteFile(t, p, data)
		return p
	}

	// PS3: disc key option, IRD, data1 sidecar.
	key := conRandomKey(conTestRand(21))
	ps3Disc := buildPS3Disc(t, key, false)
	irdPath, irdFile := conParseIRD(t)
	ps3IRDDisc := buildPS3Disc(t, ps3.DeriveDiscKey(irdFile.Data1), false)
	data1 := conRandomKey(conTestRand(22))
	ps3D1Disc := buildPS3Disc(t, ps3.DeriveDiscKey(data1), true)
	put("sidecar.data1", data1[:])

	wud := buildWUD(t)
	gc := buildGCDisc(t)
	put("gc.bca", bytes.Repeat([]byte{0xC3}, 64))
	wiiDisc, _ := buildWiiDisc(t)

	keyHex := hex.EncodeToString(key[:])
	wiiuKeyHex := hex.EncodeToString(wud.discKey[:])

	ps3Conv := func(k, d1, irdp *string) func(o *conOut, in, out string) int {
		return func(o *conOut, in, out string) int { return convertPS3(o, in, out, k, d1, irdp) }
	}
	wiiuConv := func(k *string) func(o *conOut, in, out string) int {
		return func(o *conOut, in, out string) int { return convertWiiU(o, in, out, k) }
	}

	return []conScenario{
		{name: "ps3", disc: ps3Disc.iso, input: put("ps3.iso", ps3Disc.iso), conv: ps3Conv(&keyHex, nil, nil),
			mt: media.PS3BD},
		{name: "ps3ird", disc: ps3IRDDisc.iso, input: put("ps3ird.iso", ps3IRDDisc.iso),
			conv: ps3Conv(nil, nil, &irdPath), mt: media.PS3BD},
		{name: "ps3d1", disc: ps3D1Disc.iso, input: put("sidecar.iso", ps3D1Disc.iso), conv: ps3Conv(nil, nil, nil),
			mt: media.PS3BD},
		{name: "wud", disc: wud.wud, input: put("game.wud", wud.wud), conv: wiiuConv(&wiiuKeyHex), mt: media.WUOD},
		{name: "wux", disc: wud.wud, input: put("game.wux", buildWUX(wud.wud)), conv: wiiuConv(&wiiuKeyHex),
			mt: media.WUOD},
		{name: "gc", disc: gc, input: put("gc.iso", gc), conv: convertNGCW, mt: media.GOD},
		{name: "wii", disc: wiiDisc, input: put("wii.iso", wiiDisc), conv: convertNGCW, mt: media.WOD},
	}
}

// conTagsText lists media tags as "<type> <name> <length> <hex>" lines in
// type order; tags longer than conTagsHexMax bytes are given as a SHA-256
// digest instead of their contents.
func conTagsText(img *aaru.Image, t *testing.T) string {
	tags := img.ReadableMediaTags()
	sort.Slice(tags, func(i, j int) bool { return tags[i] < tags[j] })
	var b strings.Builder
	for _, tag := range tags {
		data := conMediaTag(t, img, tag)
		fmt.Fprintf(&b, "%d %s %d ", int(tag), tag, len(data))
		if len(data) > conTagsHexMax {
			sum := sha256.Sum256(data)
			fmt.Fprintf(&b, "sha256:%s\n", hex.EncodeToString(sum[:]))
		} else {
			fmt.Fprintf(&b, "%s\n", hex.EncodeToString(data))
		}
	}
	return b.String()
}

const conTagsHexMax = 4096

// TestConsoleConvertMediaTags checks that the media tags (keys, encryption
// and partition maps, junk maps, BCA...) written for the synthetic discs match
// the expected ones.
func TestConsoleConvertMediaTags(t *testing.T) {
	dir := t.TempDir()
	for _, sc := range conBuildScenarios(t, dir) {
		t.Run(sc.name, func(t *testing.T) {
			out := filepath.Join(dir, sc.name+".aif")
			if r := conRunRaw(func(o *conOut) int { return sc.conv(o, sc.input, out) }); r.code != 0 {
				t.Fatalf("conversion failed:\n%s", r.all())
			}
			img := conCheckImage(t, out, sc.disc, sc.mt)
			checkExpected(t, "console/"+sc.name+"_tags.txt", conTagsText(img, t))
		})
	}
}
