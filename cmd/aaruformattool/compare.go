package main

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/eberle1080/go-aaru"
	"github.com/eberle1080/go-aaru/media"
)

// The colors the compare UI uses, in the default 8-color output mode
// (red is ANSI color 1, ..., white is ANSI color 7).
const (
	tbBlack  = tcell.ColorBlack
	tbRed    = tcell.ColorMaroon
	tbGreen  = tcell.ColorGreen
	tbYellow = tcell.ColorOlive
	tbBlue   = tcell.ColorNavy
	tbWhite  = tcell.ColorSilver
)

// newCompareScreen creates the terminal screen of compare; tests replace it
// with a simulation screen.
var newCompareScreen = func() (tcell.Screen, error) { return tcell.NewScreen() }

// cmdCompare implements "compare <filename1> <filename2>": an interactive
// side-by-side comparison of two images.
func cmdCompare(args []string) int {
	pos, ok := newVerbArgs("compare", usageCompare).parse(args, 2, 2)
	if !ok {
		return -1
	}
	s, err := newCompareScreen()
	if err != nil {
		return 1
	}
	return compareInteractive(s, pos[0], pos[1])
}

// compareValueKind selects how a compare line's values are drawn.
type compareValueKind int

const (
	compareValueNormal compareValueKind = iota // white value after a bold label
	compareValueRed                            // red value (the images differ)
	compareTextOnly                            // the whole line is bold white text (left/right)
)

// compareLine is one row of the comparison panels: a label and the value of
// each image. Rows are drawn from screen row 3 downwards.
type compareLine struct {
	label string
	// valueCol is the column of the value relative to the panel.
	valueCol    int
	left, right string
	// hasLeft/hasRight are false for absent metadata strings.
	hasLeft, hasRight bool
	kind              compareValueKind
}

// compareSummary is the result of comparing everything but the sectors.
type compareSummary struct {
	lines []compareLine
	// hasDifferences is set when a compared header, image information or
	// metadata field differs.
	hasDifferences bool
}

// compareImagesMetadata compares the headers, image information and metadata
// of two images. Numeric fields are printed by value, the creator, comments,
// media and drive strings are decoded from UTF-16 and the application name is
// decoded as UTF-8 in version 2 images.
func compareImagesMetadata(img1, img2 *aaru.Image) compareSummary {
	var sum compareSummary
	h1, h2 := img1.Header(), img2.Header()
	i1, i2 := img1.Info(), img2.Info()

	add := func(l compareLine) { sum.lines = append(sum.lines, l) }
	// compared adds a field that is drawn red and counted when the values
	// differ.
	compared := func(label string, col int, v1, v2 string, differ bool) {
		kind := compareValueNormal
		if differ {
			kind = compareValueRed
			sum.hasDifferences = true
		}
		add(compareLine{label: label, valueCol: col, left: v1, right: v2, hasLeft: true, hasRight: true, kind: kind})
	}
	// shown adds a field that is displayed but not compared.
	shown := func(label string, col int, v1, v2 string) {
		add(compareLine{label: label, valueCol: col, left: v1, right: v2, hasLeft: true, hasRight: true})
	}
	// optional adds a metadata string if at least one image has it
	// (displayed, not compared).
	optional := func(label string, col int, f aaru.MetadataField) {
		s1, err1 := img1.MetadataString(f)
		s2, err2 := img2.MetadataString(f)
		if err1 != nil && err2 != nil {
			return
		}
		add(compareLine{label: label, valueCol: col, left: s1, right: s2, hasLeft: err1 == nil, hasRight: err2 == nil})
	}
	version := func(major, minor uint8) string { return fmt.Sprintf("%d.%d", major, minor) }
	yes := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}

	// Header (rows 3 to 8). The application name counts as a difference but
	// is always drawn white.
	if i1.Application != i2.Application {
		sum.hasDifferences = true
	}
	shown("Application: ", 15, i1.Application, i2.Application)
	compared("Application version: ", 23, version(h1.ApplicationMajorVersion, h1.ApplicationMinorVersion),
		version(h2.ApplicationMajorVersion, h2.ApplicationMinorVersion),
		h1.ApplicationMajorVersion != h2.ApplicationMajorVersion ||
			h1.ApplicationMinorVersion != h2.ApplicationMinorVersion)
	compared("Image format version: ", 24, version(h1.ImageMajorVersion, h1.ImageMinorVersion),
		version(h2.ImageMajorVersion, h2.ImageMinorVersion),
		h1.ImageMajorVersion != h2.ImageMajorVersion || h1.ImageMinorVersion != h2.ImageMinorVersion)
	compared("Media type: ", 14, fmt.Sprint(h1.MediaType), fmt.Sprint(h2.MediaType), h1.MediaType != h2.MediaType)
	compared("Creation time: ", 17, fmt.Sprint(h1.CreationTime), fmt.Sprint(h2.CreationTime),
		h1.CreationTime != h2.CreationTime)
	compared("Last written time: ", 21, fmt.Sprint(h1.LastWrittenTime), fmt.Sprint(h2.LastWrittenTime),
		h1.LastWrittenTime != h2.LastWrittenTime)

	// Image information.
	compared("Has partitions?: ", 19, yes(i1.HasPartitions), yes(i2.HasPartitions),
		i1.HasPartitions != i2.HasPartitions)
	compared("Has sessions?: ", 17, yes(i1.HasSessions), yes(i2.HasSessions), i1.HasSessions != i2.HasSessions)
	shown("Image size without headers: ", 30, fmt.Sprintf("%d bytes", i1.ImageSize),
		fmt.Sprintf("%d bytes", i2.ImageSize))
	compared("Image contains: ", 18, fmt.Sprintf("%d sectors", i1.Sectors), fmt.Sprintf("%d sectors", i2.Sectors),
		i1.Sectors != i2.Sectors)
	compared("Biggest sector is: ", 21, fmt.Sprintf("%d bytes", i1.SectorSize),
		fmt.Sprintf("%d bytes", i2.SectorSize), i1.SectorSize != i2.SectorSize)
	compared("Image version: ", 17, i1.Version, i2.Version, i1.Version != i2.Version)

	// Metadata.
	shown("Application version: ", 23, i1.ApplicationVersion, i2.ApplicationVersion)
	optional("Creator: ", 11, aaru.MetadataCreator)
	compared("Creation time: ", 17, fmt.Sprint(h1.CreationTime), fmt.Sprint(h2.CreationTime),
		h1.CreationTime != h2.CreationTime)
	compared("Last written time: ", 21, fmt.Sprint(h1.LastWrittenTime), fmt.Sprint(h2.LastWrittenTime),
		h1.LastWrittenTime != h2.LastWrittenTime)
	optional("Comments: ", 12, aaru.MetadataComments)
	optional("Media title: ", 15, aaru.MetadataMediaTitle)
	optional("Media manufacturer: ", 22, aaru.MetadataMediaManufacturer)
	optional("Media serial number: ", 23, aaru.MetadataMediaSerialNumber)
	optional("Media barcode: ", 17, aaru.MetadataMediaBarcode)
	optional("Media part number: ", 21, aaru.MetadataMediaPartNumber)
	compared("Media type: ", 14, fmt.Sprint(uint32(i1.MediaType)), fmt.Sprint(uint32(i2.MediaType)),
		i1.MediaType != i2.MediaType)

	seq1, last1, _ := img1.MediaSequence()
	seq2, last2, _ := img2.MediaSequence()
	if seq1 > 0 || last1 > 0 || seq2 > 0 || last2 > 0 {
		add(compareLine{kind: compareTextOnly, hasLeft: true, hasRight: true,
			left:  fmt.Sprintf("Media is number %d in a set of %d media", seq1, last1),
			right: fmt.Sprintf("Media is number %d in a set of %d media", seq2, last2)})
	}

	optional("Drive manufacturer: ", 22, aaru.MetadataDriveManufacturer)
	optional("Drive model: ", 15, aaru.MetadataDriveModel)
	optional("Drive serial number: ", 23, aaru.MetadataDriveSerialNumber)
	optional("Drive firmware revision: ", 27, aaru.MetadataDriveFirmwareRevision)
	compared("XML media type: ", 18, fmt.Sprint(uint8(i1.MetadataMediaType)), fmt.Sprint(uint8(i2.MetadataMediaType)),
		i1.MetadataMediaType != i2.MetadataMediaType)

	c1, hd1, s1 := compareGeometry(img1)
	c2, hd2, s2 := compareGeometry(img2)
	if c1 > 0 || hd1 > 0 || s1 > 0 || c2 > 0 || hd2 > 0 || s2 > 0 {
		add(compareLine{kind: compareTextOnly, hasLeft: true, hasRight: true,
			left:  fmt.Sprintf("Media has %d cylinders, %d heads and %d sectors per track", c1, hd1, s1),
			right: fmt.Sprintf("Media has %d cylinders, %d heads and %d sectors per track", c2, hd2, s2)})
	}
	return sum
}

// compareGeometry returns the geometry of an image as an opened image
// reports it: the geometry block, or for block media without one the
// default of 16 heads and 63 sectors per track.
func compareGeometry(img *aaru.Image) (cylinders, heads, sectorsPerTrack uint32) {
	if c, h, s, err := img.Geometry(); err == nil {
		return c, h, s
	}
	if img.Info().MetadataMediaType == media.BlockMedia {
		return uint32(img.Info().Sectors / 16 / 63), 16, 63
	}
	return 0, 0, 0
}

// compareSectorContents compares the user data of the sectors present in
// both images. progress is called before each sector
// with its index and the number of sectors compared; differs is called for
// every sector whose contents differ. Sectors that cannot be read (other than
// not dumped ones) are skipped; a not dumped sector equals another not dumped
// sector and differs from a dumped one.
func compareSectorContents(img1, img2 *aaru.Image, progress func(i, n uint64), differs func(sector uint64)) bool {
	sectors := min(img1.Info().Sectors, img2.Info().Sectors)
	contentsDiffer := false
	for i := uint64(0); i < sectors; i++ {
		if progress != nil {
			progress(i, sectors)
		}
		data1, _, err1 := img1.ReadSector(i, false)
		data2, _, err2 := img2.ReadSector(i, false)
		if err1 != nil && !errors.Is(err1, aaru.ErrSectorNotDumped) {
			continue
		}
		if err2 != nil && !errors.Is(err2, aaru.ErrSectorNotDumped) {
			continue
		}
		if (err1 == nil) != (err2 == nil) || !bytes.Equal(data1, data2) {
			contentsDiffer = true
			if differs != nil {
				differs(i)
			}
		}
	}
	return contentsDiffer
}

// compareUI draws the compare layout on a tcell screen.
type compareUI struct {
	s             tcell.Screen
	width, height int
	midX          int
	leftRow       int
	lastShow      time.Time
}

// compareStyle returns a style with fg on blue, optionally bold.
func compareStyle(fg tcell.Color, bold bool) tcell.Style {
	return tcell.StyleDefault.Foreground(fg).Background(tbBlue).Bold(bold)
}

// print draws text at (x, y).
func (u *compareUI) print(x, y int, fg tcell.Color, bold bool, text string) {
	u.s.PutStrStyled(x, y, text, compareStyle(fg, bold))
}

// show presents the screen, at most every 50 ms unless forced.
func (u *compareUI) show(force bool) {
	if force || time.Since(u.lastShow) >= 50*time.Millisecond {
		u.s.Show()
		u.lastShow = time.Now()
	}
}

// setup paints both blue panels and the divider.
func (u *compareUI) setup() {
	u.width, u.height = u.s.Size()
	u.midX = u.width / 2
	u.leftRow = 9
	u.s.Clear()
	for y := 0; y < u.height; y++ {
		for x := 0; x < u.width; x++ {
			if x == u.midX {
				u.s.SetContent(x, y, '|', nil, compareStyle(tbBlack, true))
			} else {
				u.s.SetContent(x, y, ' ', nil, compareStyle(tbWhite, false))
			}
		}
	}
}

// drawLines draws the comparison rows starting at row 3 and leaves leftRow
// after the last one.
func (u *compareUI) drawLines(lines []compareLine) {
	row := 3
	for _, l := range lines {
		if l.kind == compareTextOnly {
			u.print(2, row, tbWhite, true, l.left)
			u.print(u.midX+2, row, tbWhite, true, l.right)
		} else {
			fg := tbWhite
			if l.kind == compareValueRed {
				fg = tbRed
			}
			u.print(2, row, tbWhite, true, l.label)
			if l.hasLeft {
				u.print(l.valueCol, row, fg, false, l.left)
			}
			u.print(u.midX+2, row, tbWhite, true, l.label)
			if l.hasRight {
				u.print(u.midX+l.valueCol, row, fg, false, l.right)
			}
		}
		row++
	}
	u.leftRow = max(row, 9)
	u.show(true)
}

// progressBar draws the progress bar at row.
func (u *compareUI) progressBar(row, percent int) {
	barWidth := u.width/2 - 4
	filled := barWidth * percent / 100
	u.print(2, row, tbYellow, true, "[")
	u.print(barWidth+3, row, tbYellow, true, "]")
	for i := 0; i < filled; i++ {
		u.s.SetContent(3+i, row, '=', nil, compareStyle(tbYellow, true))
	}
}

// compareInteractive runs the compare UI on s until a key is pressed. It
// returns 0, or -1 if an image cannot be opened.
func compareInteractive(s tcell.Screen, path1, path2 string) int {
	if err := s.Init(); err != nil {
		return 1
	}
	defer s.Fini()

	u := &compareUI{s: s}
	u.setup()
	ret := compareRun(u, path1, path2)

	u.print(2, u.height-1, tbWhite, true, "Press any key to exit...")
	u.show(true)
	for {
		switch s.PollEvent().(type) {
		case nil, *tcell.EventKey:
			return ret
		case *tcell.EventResize:
			s.Sync()
		}
	}
}

// compareRun opens both images and draws the comparison.
func compareRun(u *compareUI, path1, path2 string) int {
	u.print(2, 1, tbWhite, true, path1)
	u.print(u.midX+2, 1, tbWhite, true, path2)
	u.show(true)

	u.print(2, 2, tbWhite, true, "Opening image...")
	u.show(true)
	img1, err := aaru.Open(path1)
	if err != nil {
		u.print(2, 3, tbRed, true, "Error opening image")
		u.show(true)
		return -1
	}
	defer img1.Close()
	u.print(2, 2, tbWhite, true, "Image opened successfully...")

	u.print(u.midX+2, 2, tbWhite, true, "Opening image...")
	u.show(true)
	img2, err := aaru.Open(path2)
	if err != nil {
		u.print(u.midX+2, 3, tbRed, true, "Error opening image")
		u.show(true)
		return -1
	}
	defer img2.Close()
	u.print(u.midX+2, 2, tbWhite, true, "Image opened successfully...")

	sum := compareImagesMetadata(img1, img2)
	u.drawLines(sum.lines)
	u.leftRow++

	// The progress text is throttled rather than redrawn for every sector.
	progressText := func(i, n uint64) {
		u.print(2, u.height-5, tbWhite, true, fmt.Sprintf("Comparing sector %d of %d", i+1, n))
	}
	progress := func(i, n uint64) {
		if i > 0 && time.Since(u.lastShow) < 50*time.Millisecond {
			return
		}
		progressText(i, n)
		u.progressBar(u.height-4, int(i*100/n))
		u.show(true)
	}
	differs := func(sector uint64) {
		if u.leftRow < u.height-6 {
			u.print(2, u.leftRow, tbRed, true, fmt.Sprintf("Sector %d differs", sector))
			u.leftRow++
			u.show(false)
		}
	}
	contentsDiffer := compareSectorContents(img1, img2, progress, differs)
	if n := min(img1.Info().Sectors, img2.Info().Sectors); n > 0 {
		progressText(n-1, n)
	}
	u.progressBar(u.height-4, 100)

	if sum.hasDifferences {
		u.print(2, u.height-3, tbRed, true, "Images are different!")
	} else {
		u.print(2, u.height-3, tbGreen, true, "Images are identical!")
	}
	if contentsDiffer {
		u.print(2, u.height-2, tbRed, true, "Images contents are different!")
	} else {
		u.print(2, u.height-2, tbGreen, true, "Images contents are identical!")
	}
	u.show(true)
	return 0
}
