package harness

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/vizeval"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// The contact sheet (ADR-0266 §SD8) is every candidate's artifact of one run
// in one image, labelled with its options and status, for a reader who looks
// at a round as a whole before opening one artifact. It is sized for being
// looked at as one image: a viewer that shows an image whole shrinks it to
// roughly its screen, so the sheet is at most contactWidth wide and splits
// into further sheets past contactPerSheet candidates rather than shrinking
// every thumbnail to nothing.
const (
	contactWidth    = 1600
	contactPerSheet = 6
	contactMaxCols  = 2
	contactGutter   = 12
	contactPad      = 8
	contactFontPx   = 15
	contactLineGap  = 4
	// contactEmptyH is the thumbnail height of a candidate with no artifact.
	contactEmptyH = 60
)

var (
	contactBg      = color.RGBA{0x2a, 0x2a, 0x2e, 0xff}
	contactCellBg  = color.RGBA{0x12, 0x12, 0x14, 0xff}
	contactText    = color.RGBA{0xe8, 0xe8, 0xe8, 0xff}
	contactDim     = color.RGBA{0xa0, 0xa0, 0xa8, 0xff}
	contactStatusC = map[StatusE]color.RGBA{
		StatusScored:       {0x6c, 0xd1, 0x7a, 0xff},
		StatusGated:        {0xf2, 0xb1, 0x3c, 0xff},
		StatusInadmissible: {0xa0, 0xa0, 0xa8, 0xff},
		StatusFailed:       {0xf0, 0x60, 0x5a, 0xff},
	}
)

// ContactSheetName is the first sheet's file name; further sheets are
// contact-2.png, contact-3.png, ….
const ContactSheetName = "contact.png"

// contactSheetPath is the path of the n-th sheet, counting from 1.
func contactSheetPath(dir string, n int) string {
	if n == 1 {
		return filepath.Join(dir, ContactSheetName)
	}
	return filepath.Join(dir, "contact-"+strconv.Itoa(n)+".png")
}

// writeContactSheets writes the scenario's sheets and returns their paths.
// Cells are numbered in card order, the order index.md lists them in, so a
// number on the sheet finds its section there.
func writeContactSheets(outDir string, dir string, cards []Scorecard) (paths []string, err error) {
	if len(cards) == 0 {
		return nil, nil
	}
	face, err := contactFace()
	if err != nil {
		return nil, err
	}
	defer func() { _ = face.Close() }()
	arts := make([]image.Image, len(cards))
	widest := 0
	for i, c := range cards {
		if c.Dir == "" {
			continue
		}
		if img, e := readPNG(filepath.Join(outDir, c.Dir, "artifact.png")); e == nil {
			arts[i] = img
			widest = max(widest, img.Bounds().Dx())
		}
	}
	// One scale for every sheet of the run — the one that fits the widest
	// artifact into a column — so candidates' sizes stay comparable across
	// sheets as they were on screen.
	cols := min(len(cards), contactMaxCols)
	innerW := contactCellW(cols) - 2*contactPad
	scale := 1.0
	if widest > innerW {
		scale = float64(innerW) / float64(widest)
	}
	for start, n := 0, 1; start < len(cards); start, n = start+contactPerSheet, n+1 {
		end := min(start+contactPerSheet, len(cards))
		img := contactSheet(cards[start:end], arts[start:end], start, cols, scale, face)
		p := contactSheetPath(dir, n)
		if err = writePNG(p, img); err != nil {
			return paths, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func contactFace() (face font.Face, err error) {
	f, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, eh.Errorf("unable to parse the contact sheet's font: %w", err)
	}
	face, err = opentype.NewFace(f, &opentype.FaceOptions{Size: contactFontPx, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, eh.Errorf("unable to size the contact sheet's font: %w", err)
	}
	return face, nil
}

type contactCell struct {
	lines  []contactLine
	thumb  image.Image
	labelH int
	thumbH int
}

type contactLine struct {
	text string
	col  color.RGBA
}

func contactCellW(cols int) int {
	return (contactWidth - contactGutter*(cols+1)) / cols
}

// contactSheet lays cards out on one sheet, arts[i] being cards[i]'s artifact
// or nil, every thumbnail scaled by scale.
func contactSheet(cards []Scorecard, arts []image.Image, first int, cols int, scale float64, face font.Face) *image.RGBA {
	cellW := contactCellW(cols)
	innerW := cellW - 2*contactPad
	lineH := face.Metrics().Height.Ceil() + contactLineGap
	cells := make([]contactCell, len(cards))
	for i, c := range cards {
		cells[i] = contactCell{lines: contactLabel(c, first+i+1, face, innerW)}
		cells[i].labelH = len(cells[i].lines)*lineH + contactPad
		if arts[i] == nil {
			cells[i].thumbH = contactEmptyH
			continue
		}
		b := arts[i].Bounds()
		w, h := max(1, int(float64(b.Dx())*scale+0.5)), max(1, int(float64(b.Dy())*scale+0.5))
		t := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(t, t.Bounds(), arts[i], b, draw.Src, nil)
		cells[i].thumb = t
		cells[i].thumbH = h
	}
	// Row heights: each row as tall as its tallest cell, so a row's thumbnails
	// start on one line and a short label does not pull its image up.
	rows := (len(cells) + cols - 1) / cols
	labelHs, thumbHs := make([]int, rows), make([]int, rows)
	for i, cl := range cells {
		r := i / cols
		labelHs[r] = max(labelHs[r], cl.labelH)
		thumbHs[r] = max(thumbHs[r], cl.thumbH)
	}
	height := contactGutter
	for r := range rows {
		height += labelHs[r] + thumbHs[r] + 2*contactPad + contactGutter
	}
	sheet := image.NewRGBA(image.Rect(0, 0, contactWidth, height))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(contactBg), image.Point{}, draw.Src)
	y := contactGutter
	for r := range rows {
		rowH := labelHs[r] + thumbHs[r] + 2*contactPad
		for k := range cols {
			i := r*cols + k
			if i >= len(cells) {
				break
			}
			x := contactGutter + k*(cellW+contactGutter)
			draw.Draw(sheet, image.Rect(x, y, x+cellW, y+rowH), image.NewUniform(contactCellBg), image.Point{}, draw.Src)
			d := font.Drawer{Dst: sheet, Face: face}
			ty := y + contactPad + face.Metrics().Ascent.Ceil()
			for _, l := range cells[i].lines {
				d.Src = image.NewUniform(l.col)
				d.Dot = fixed.P(x+contactPad, ty)
				d.DrawString(l.text)
				ty += lineH
			}
			if t := cells[i].thumb; t != nil {
				ox, oy := x+contactPad, y+contactPad+labelHs[r]
				draw.Draw(sheet, t.Bounds().Add(image.Pt(ox, oy)), t, image.Point{}, draw.Src)
			}
		}
		y += rowH + contactGutter
	}
	return sheet
}

// contactLabel is a cell's caption: its number, status and sink, then the
// options one per `name=value` token wrapped to the cell, then what explains
// a status other than scored — the failed gates or the reason.
func contactLabel(c Scorecard, n int, face font.Face, width int) (lines []contactLine) {
	head := "#" + strconv.Itoa(n) + " " + string(c.Status) + "  " + c.Candidate.Sink + "  " + c.CandidateID[:min(8, len(c.CandidateID))]
	col, ok := contactStatusC[c.Status]
	if !ok {
		col = contactText
	}
	lines = append(lines, contactLine{head, col})
	for _, l := range wrapTokens(OptionTokens(c.Candidate.Options), face, width) {
		lines = append(lines, contactLine{l, contactText})
	}
	var note []string
	for _, g := range sortedKeys(c.Gates) {
		if !c.Gates[g] {
			note = append(note, "failed gate "+g)
		}
	}
	if c.Reason != "" {
		note = append(note, c.Reason)
	}
	if c.ReusedFrom != "" && (c.Dir == "" || c.Metrics == nil) {
		note = append(note, "reused from "+c.ReusedFrom)
	}
	if len(note) > 0 {
		for _, l := range wrapTokens(strings.Fields(strings.Join(note, "; ")), face, width) {
			lines = append(lines, contactLine{l, contactDim})
		}
	}
	return lines
}

// wrapTokens joins tokens with spaces into lines no wider than width; a token
// wider than a line is cut with an ellipsis rather than overflowing the cell.
func wrapTokens(tokens []string, face font.Face, width int) (lines []string) {
	limit := fixed.I(width)
	var cur string
	for _, t := range tokens {
		if font.MeasureString(face, t) > limit {
			rs := []rune(t)
			for len(rs) > 1 && font.MeasureString(face, string(rs)+"…") > limit {
				rs = rs[:len(rs)-1]
			}
			t = string(rs) + "…"
		}
		next := t
		if cur != "" {
			next = cur + " " + t
		}
		if cur != "" && font.MeasureString(face, next) > limit {
			lines = append(lines, cur)
			next = t
		}
		cur = next
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// OptionTokens renders options as `name=value` tokens in name order — the
// canonical form's content without its JSON punctuation, for a reader
// scanning a column of candidates.
func OptionTokens(opts vizeval.Values) (tokens []string) {
	tokens = make([]string, 0, len(opts))
	for _, k := range sortedKeys(opts) {
		var v string
		switch x := opts[k].(type) {
		case string:
			v = x
		case float64:
			v = strconv.FormatFloat(x, 'g', -1, 64)
		case int64:
			v = strconv.FormatInt(x, 10)
		case bool:
			v = strconv.FormatBool(x)
		default:
			v = fmt.Sprint(x)
		}
		tokens = append(tokens, k+"="+v)
	}
	return tokens
}

func sortedKeys[V any](m map[string]V) (keys []string) {
	keys = make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func writePNG(path string, img image.Image) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return eh.Errorf("unable to create %s: %w", filepath.Base(path), err)
	}
	if err = png.Encode(f, img); err != nil {
		_ = f.Close()
		return eh.Errorf("unable to encode %s: %w", filepath.Base(path), err)
	}
	return f.Close()
}
