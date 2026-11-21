package splashscreen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/vcs"
)

func pngOf(t *testing.T, w, h int, col color.NRGBA) (b []byte) {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = col.R, col.G, col.B, col.A
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	b = buf.Bytes()
	return
}

func testProgram(t *testing.T) (p Program) {
	p = Program{
		Name:      "Prog",
		Icon:      "*",
		Copyright: "Copyright © 2026 Prog Authors",
		License:   "Licensed under the Prog Licence.",
		Notice:    "Prog\nincorporates libfoo.\n",
		Png:       pngOf(t, 4, 2, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0x80}),
	}
	return
}

func TestProgramOpensFirst(t *testing.T) {
	t.Cleanup(ClearProgram)
	ClearProgram()
	assert.Equal(t, boxerKey, newApp().sel)
	ps := parties()
	require.Len(t, ps, 1)
	assert.Equal(t, boxerKey, ps[0].key)

	require.NoError(t, SetProgram(testProgram(t)))
	inst := newApp()
	assert.Equal(t, programKey, inst.sel)
	assert.Equal(t, pageSplash, inst.page)
	ps = parties()
	require.Len(t, ps, 2)
	assert.Equal(t, programKey, ps[0].key)
	assert.Equal(t, "* Prog", ps[0].label())
	assert.Equal(t, boxerKey, ps[1].key)

	ClearProgram()
	assert.Equal(t, boxerKey, newApp().sel)
}

// TestPartiesKeepTheirOwnNotices is the point of the split: the program's
// entry carries only what the program supplied, boxer's only boxer's.
func TestPartiesKeepTheirOwnNotices(t *testing.T) {
	t.Cleanup(ClearProgram)
	require.NoError(t, SetProgram(testProgram(t)))
	prog, ok := lookupParty(parties(), programKey)
	require.True(t, ok)
	bx, ok := lookupParty(parties(), boxerKey)
	require.True(t, ok)

	assert.Contains(t, prog.identityLines(), "Copyright © 2026 Prog Authors")
	assert.Contains(t, prog.identityLines(), "Licensed under the Prog Licence.")
	assert.NotContains(t, prog.identityLines(), vcs.CopyrightInfo())
	assert.NotContains(t, prog.identityLines(), boxerLicense)
	assert.NotContains(t, prog.notice, "boxer")

	assert.Contains(t, bx.identityLines(), vcs.CopyrightInfo())
	assert.Contains(t, bx.identityLines(), boxerLicense)
	assert.NotContains(t, bx.notice, "libfoo")

	page := string(helpPage([]byte("# About\n")))
	iProg := strings.Index(page, "## Prog")
	iBoxer := strings.Index(page, "## boxer")
	iBuild := strings.Index(page, "## Build")
	require.True(t, iProg >= 0 && iBoxer > iProg && iBuild > iBoxer, "sections in tab order: program, boxer, Build")
	progSection := page[iProg:iBoxer]
	assert.Contains(t, progSection, "Copyright © 2026 Prog Authors")
	assert.Contains(t, progSection, "incorporates libfoo.")
	assert.NotContains(t, progSection, vcs.CopyrightInfo())
	assert.NotContains(t, page[iBoxer:iBuild], "Prog")
}

func TestProgramArtworkKeepsItsColours(t *testing.T) {
	t.Cleanup(ClearProgram)
	p := testProgram(t)
	p.Png = pngOf(t, 3, 2, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0x80})
	p.PngDark = pngOf(t, 5, 1, color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff})
	require.NoError(t, SetProgram(p))

	img, err := programImage()
	require.NoError(t, err)
	require.Len(t, img.pixels, int(img.w*img.h))
	// whichever theme is active, the pixels are the artwork's own, straight alpha
	switch img.w {
	case 3:
		assert.Equal(t, uint32(0x12345680), img.pixels[0])
	case 5:
		assert.Equal(t, uint32(0xabcdefff), img.pixels[0])
	default:
		t.Fatalf("unexpected width %d", img.w)
	}
}

func TestProgramWithoutArtworkHasNoSplashPage(t *testing.T) {
	t.Cleanup(ClearProgram)
	p := testProgram(t)
	p.Png = nil
	require.NoError(t, SetProgram(p))
	prog, ok := programParty()
	require.True(t, ok)
	assert.Nil(t, prog.image)
	assert.Equal(t, programKey, newApp().sel)
}

func TestProgramVersionChangesWithRegistration(t *testing.T) {
	t.Cleanup(ClearProgram)
	p := testProgram(t)
	require.NoError(t, SetProgram(p))
	first, err := programImage()
	require.NoError(t, err)
	require.NoError(t, SetProgram(p))
	second, err := programImage()
	require.NoError(t, err)
	assert.NotEqual(t, first.version, second.version)
}

func TestSetProgramRefusesIncomplete(t *testing.T) {
	t.Cleanup(ClearProgram)
	ClearProgram()
	noName := testProgram(t)
	noName.Name = ""
	assert.Error(t, SetProgram(noName))
	darkOnly := testProgram(t)
	darkOnly.PngDark, darkOnly.Png = darkOnly.Png, nil
	assert.Error(t, SetProgram(darkOnly))
	assert.False(t, programRegistered())
}

// TestProgramWithoutCopyrightBorrowsNone: a program that supplies no
// copyright line shows none, and boxer's does not fill the gap.
func TestProgramWithoutCopyrightBorrowsNone(t *testing.T) {
	t.Cleanup(ClearProgram)
	p := testProgram(t)
	p.Copyright = ""
	require.NoError(t, SetProgram(p))
	prog, ok := programParty()
	require.True(t, ok)
	for _, line := range prog.identityLines() {
		assert.NotContains(t, line, "Copyright")
	}
}

func TestProgramBadPngDegrades(t *testing.T) {
	t.Cleanup(ClearProgram)
	p := testProgram(t)
	p.Png = []byte("not a png")
	require.NoError(t, SetProgram(p))
	_, err := programImage()
	assert.Error(t, err)
}

func TestBoxerVersionFromBuildInfo(t *testing.T) {
	// In a test binary boxer is the main module; the version is whatever the
	// build info records, and a module absent from it has none.
	assert.Empty(t, moduleVersion(""))
	assert.Empty(t, moduleVersion("example.com/not/linked"))
}
