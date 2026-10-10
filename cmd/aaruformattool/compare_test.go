package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// coreKeyScreen is a simulation screen of a given size that records its
// contents and presses a key whenever the UI waits for an event.
type coreKeyScreen struct {
	tcell.SimulationScreen
	cells         []tcell.SimCell
	width, height int
	polls         int
	w, h          int
}

func (s *coreKeyScreen) Init() error {
	if err := s.SimulationScreen.Init(); err != nil {
		return err
	}
	s.SetSize(s.w, s.h)
	return nil
}

func (s *coreKeyScreen) PollEvent() tcell.Event {
	s.polls++
	s.cells, s.width, s.height = s.SimulationScreen.GetContents()
	s.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
	return s.SimulationScreen.PollEvent()
}

// row returns the text of a screen row (trailing blanks removed).
func (s *coreKeyScreen) row(y int) string {
	var b strings.Builder
	for x := 0; x < s.width; x++ {
		c := s.cells[y*s.width+x]
		if len(c.Runes) > 0 {
			b.WriteString(string(c.Runes))
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// cell returns the cell at (x, y).
func (s *coreKeyScreen) cell(x, y int) tcell.SimCell { return s.cells[y*s.width+x] }

// find returns the position of text on the screen, or -1, -1.
func (s *coreKeyScreen) find(text string) (x, y int) {
	for y := 0; y < s.height; y++ {
		if i := strings.Index(s.row(y), text); i >= 0 {
			return len([]rune(s.row(y)[:i])), y
		}
	}
	return -1, -1
}

// coreRunCompareUI runs the compare UI of two images on a w x h simulation
// screen and returns the screen as it was when the UI waited for a key.
func coreRunCompareUI(t *testing.T, path1, path2 string, w, h int) (*coreKeyScreen, int) {
	t.Helper()
	s := &coreKeyScreen{SimulationScreen: tcell.NewSimulationScreen("UTF-8"), w: w, h: h}
	old := newCompareScreen
	newCompareScreen = func() (tcell.Screen, error) { return s, nil }
	defer func() { newCompareScreen = old }()
	code, out, _ := coreRun(t, "compare", path1, path2)
	if out != "" {
		t.Errorf("compare wrote to stdout: %q", out)
	}
	if s.cells == nil {
		t.Fatal("the UI never waited for a key")
	}
	return s, code
}

// coreMakeBlockImage creates a 512-byte sector hard disk image at path with
// the given sectors (nil sectors are left not dumped) and returns path.
func coreMakeBlockImage(t *testing.T, path string, sectors [][]byte) string {
	t.Helper()
	img, err := aaru.Create(path, aaru.CreateParams{MediaType: media.GENERIC_HDD, SectorSize: 512,
		UserSectors: uint64(len(sectors)), Application: "coretest"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range sectors {
		if d != nil {
			if err := img.WriteSector(uint64(i), false, d, aaru.SectorStatusDumped); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// coreSectors returns n 512-byte sectors filled with their index plus seed;
// the sectors listed in skip are nil.
func coreSectors(n int, seed byte, skip ...int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = bytes.Repeat([]byte{byte(i) + seed}, 512)
	}
	for _, i := range skip {
		out[i] = nil
	}
	return out
}

func openCore(t *testing.T, path string) *aaru.Image {
	t.Helper()
	img, err := aaru.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { img.Close() })
	return img
}

func TestCoreCompareMetadata(t *testing.T) {
	img := openCore(t, fixture(t, "cdmode1"))
	sum := compareImagesMetadata(img, img)
	if sum.hasDifferences {
		t.Error("an image differs from itself")
	}
	labels := map[string]compareLine{}
	for _, l := range sum.lines {
		if l.kind == compareValueRed {
			t.Errorf("line %q drawn as different", l.label)
		}
		labels[l.label] = l
	}
	// The first six lines are the header rows 3 to 8.
	wantHeader := []string{"Application: ", "Application version: ", "Image format version: ", "Media type: ",
		"Creation time: ", "Last written time: "}
	for i, w := range wantHeader {
		if sum.lines[i].label != w {
			t.Errorf("header line %d = %q, want %q", i, sum.lines[i].label, w)
		}
	}
	for _, c := range []struct{ label, value string }{
		{"Application: ", "go-aaru-test"},
		{"Image contains: ", "600 sectors"},
		{"Biggest sector is: ", "2048 bytes"},
		{"Has partitions?: ", "yes"},
		{"Image version: ", "2.0"},
		{"XML media type: ", "0"},
		{"Media type: ", "15"},
		{"Image size without headers: ", fmt.Sprintf("%d bytes", img.Info().ImageSize)},
	} {
		if l, ok := labels[c.label]; !ok || l.left != c.value || l.right != c.value {
			t.Errorf("%q = %+v, want %q", c.label, l, c.value)
		}
	}
	if _, ok := labels["Creator: "]; ok {
		t.Error("absent metadata string shown")
	}

	// Version 1 vs version 2 image of the same disc.
	v1 := openCore(t, fixture(t, "cdmode1_v1"))
	sum = compareImagesMetadata(img, v1)
	if !sum.hasDifferences {
		t.Error("v1 and v2 images compare equal")
	}
	red := map[string]bool{}
	for _, l := range sum.lines {
		if l.kind == compareValueRed {
			red[l.label] = true
		}
	}
	if !red["Image format version: "] || !red["Image version: "] || red["Image contains: "] {
		t.Errorf("red lines: %v", red)
	}

	// Metadata strings, media sequence and geometry.
	meta := openCore(t, fixture(t, "meta_all"))
	sum = compareImagesMetadata(meta, img)
	var texts []string
	for _, l := range sum.lines {
		if l.kind == compareTextOnly {
			texts = append(texts, l.left+" | "+l.right)
		}
		if l.label == "Creator: " && (!l.hasLeft || l.hasRight || l.left == "") {
			t.Errorf("creator line %+v", l)
		}
	}
	if len(texts) != 2 || !strings.HasPrefix(texts[0], "Media is number ") ||
		!strings.HasPrefix(texts[1], "Media has ") {
		t.Errorf("text lines %q", texts)
	}
}

func TestCoreCompareSectorContents(t *testing.T) {
	base := coreSectors(40, 1)
	changed := coreSectors(40, 1, 7) // sector 7 not dumped
	changed[12] = bytes.Repeat([]byte{0xEE}, 512)
	changed[39] = bytes.Repeat([]byte{0xEF}, 512)
	dir := t.TempDir()
	img1 := openCore(t, coreMakeBlockImage(t, filepath.Join(dir, "a.aif"), base))
	img2 := openCore(t, coreMakeBlockImage(t, filepath.Join(dir, "b.aif"), changed))
	var differ []uint64
	var progress int
	got := compareSectorContents(img1, img2, func(i, n uint64) { progress++ }, func(s uint64) { differ = append(differ, s) })
	if !got || progress != 40 || fmt.Sprint(differ) != "[7 12 39]" {
		t.Errorf("compare: %v, %d progress calls, differ %v", got, progress, differ)
	}
	if compareSectorContents(img1, img1, nil, nil) {
		t.Error("an image's contents differ from themselves")
	}
	// Not dumped sectors equal each other; only the common sectors count.
	img3 := openCore(t, coreMakeBlockImage(t, filepath.Join(dir, "c.aif"), coreSectors(30, 1, 7)))
	differ = nil
	compareSectorContents(img2, img3, nil, func(s uint64) { differ = append(differ, s) })
	if fmt.Sprint(differ) != "[12]" {
		t.Errorf("b vs c differ %v", differ)
	}
}

func TestCoreCompareUI(t *testing.T) {
	changed := coreSectors(400, 1, 300)
	changed[309] = bytes.Repeat([]byte{0xEE}, 512)
	// Long paths overwrite the divider.
	t.Chdir(t.TempDir())
	path1 := coreMakeBlockImage(t, "a.aif", coreSectors(400, 1))
	path2 := coreMakeBlockImage(t, "b.aif", changed)
	s, code := coreRunCompareUI(t, path1, path2, 120, 50)
	if code != 0 {
		t.Errorf("exit status %d", code)
	}
	mid := 60
	if got := s.row(1); !strings.HasPrefix(got, "  "+path1) || !strings.Contains(got, "| "+path2) {
		t.Errorf("row 1 = %q", got)
	}
	if got := s.row(2); got != "  Image opened successfully..."+strings.Repeat(" ", mid-30)+"| Image opened successfully..." {
		t.Errorf("row 2 = %q", got)
	}
	if got := s.row(3); !strings.HasPrefix(got, "  Application: coretest") {
		t.Errorf("row 3 = %q", got)
	}
	for _, want := range []string{"Comparing sector 400 of 400", "Images are different!", "Images contents are different!",
		"Press any key to exit...", "Sector 300 differs", "Sector 309 differs", "Image contains: 400 sectors"} {
		if x, _ := s.find(want); x < 0 {
			t.Errorf("screen lacks %q", want)
		}
	}
	// Layout: the divider, blue background, red differences, full progress bar.
	for y := 0; y < 50; y++ {
		c := s.cell(mid, y)
		if string(c.Runes) != "|" {
			t.Fatalf("divider missing at row %d: %q", y, string(c.Runes))
		}
		if fg, bg, attr := c.Style.Decompose(); fg != tbBlack || bg != tbBlue || attr&tcell.AttrBold == 0 {
			t.Fatalf("divider style %v %v %v", fg, bg, attr)
		}
	}
	x, y := s.find("Sector 300 differs")
	if fg, bg, attr := s.cell(x, y).Style.Decompose(); fg != tbRed || bg != tbBlue || attr&tcell.AttrBold == 0 {
		t.Errorf("difference style %v %v %v", fg, bg, attr)
	}
	// Rows 3-8 header, 9-14 image information, 15-20 metadata, a blank row.
	if x != 2 || y != 22 {
		t.Errorf("first differing sector at %d,%d, want 2,22", x, y)
	}
	if bar := s.row(46); bar != "  ["+strings.Repeat("=", 56)+"]|" {
		t.Errorf("progress bar row = %q", bar)
	}
	x, y = s.find("Images are different!")
	if fg, _, _ := s.cell(x, y).Style.Decompose(); fg != tbRed || y != 47 {
		t.Errorf("result style %v row %d", fg, y)
	}
	x, y = s.find("Image contains: ")
	if fg, _, attr := s.cell(x+len("Image contains: "), y).Style.Decompose(); fg != tbWhite || attr&tcell.AttrBold != 0 {
		t.Errorf("value style %v %v", fg, attr)
	}

	// Identical images: green results.
	s, _ = coreRunCompareUI(t, path1, path1, 100, 45)
	for _, want := range []string{"Images are identical!", "Images contents are identical!"} {
		x, y := s.find(want)
		if x < 0 {
			t.Fatalf("screen lacks %q", want)
		}
		if fg, _, _ := s.cell(x, y).Style.Decompose(); fg != tbGreen {
			t.Errorf("%q color %v", want, fg)
		}
	}
	// Differing sectors are listed only while they fit (above row height-6).
	path3 := coreMakeBlockImage(t, "c.aif", coreSectors(400, 2))
	s, _ = coreRunCompareUI(t, path1, path3, 100, 40)
	last := -1
	for y := 0; y < 40; y++ {
		if strings.Contains(s.row(y), " differs") {
			last = y
		}
	}
	if x, y := s.find("Sector 0 differs"); x != 2 || y != 22 || last != 40-7 {
		t.Errorf("first differing sector at %d,%d, last listed on row %d", x, y, last)
	}

	// Version 1 and 2 images of the same disc: same contents, but the header
	// (versions, times) differs. Sector statuses are not compared.
	fixture(t, "cdmode1")
	fixture(t, "cdmode1_v1")
	t.Chdir(fxDir)
	s, _ = coreRunCompareUI(t, "cdmode1.aif", "cdmode1_v1.aif", 140, 40)
	if x, _ := s.find("Images are different!"); x < 0 {
		t.Error("cdmode1 vs cdmode1_v1: headers compare equal")
	}
	if x, _ := s.find("Images contents are identical!"); x < 0 {
		t.Error("cdmode1 vs cdmode1_v1: contents differ")
	}
	x, y = s.find("Image format version: 2.0")
	if fg, _, _ := s.cell(x+len("Image format version: "), y).Style.Decompose(); x < 0 || fg != tbRed {
		t.Errorf("image format version at %d,%d color %v", x, y, fg)
	}
}

func TestCoreCompareOpenError(t *testing.T) {
	notImage := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(notImage, fxData(99, 0, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	s, code := coreRunCompareUI(t, fixture(t, "cdmode1"), notImage, 80, 30)
	if code != -1 {
		t.Errorf("exit status %d, want -1", code)
	}
	if got := s.row(3); !strings.HasSuffix(got, "| Error opening image") {
		t.Errorf("row 3 = %q", got)
	}
	if got := s.row(29); got != "  Press any key to exit..."+strings.Repeat(" ", 40-26)+"|" {
		t.Errorf("row 29 = %q", got)
	}
	x, y := s.find("Error opening image")
	if fg, _, _ := s.cell(x, y).Style.Decompose(); fg != tbRed {
		t.Errorf("error color %v", fg)
	}

	old := newCompareScreen
	newCompareScreen = func() (tcell.Screen, error) { return nil, fmt.Errorf("no terminal") }
	defer func() { newCompareScreen = old }()
	if code, _, _ := coreRun(t, "compare", "a", "b"); code != 1 {
		t.Errorf("compare without terminal = %d, want 1", code)
	}
}
