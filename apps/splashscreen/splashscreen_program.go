package splashscreen

import (
	"bytes"
	"image"
	"image/draw"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// programKey names the program's tab and scopes its texture.
const programKey = "program"

// Program is the program that links the splash screen: its identity,
// licence and artwork. The window credits it in a tab of its own, before
// boxer's, and opens on that tab. Nothing in the program's tab comes from
// boxer, and nothing in boxer's comes from the program.
type Program struct {
	// Name names the program's tab; required.
	Name string
	// Icon is an icon glyph set before the name; empty for none.
	Icon string
	// Copyright is the program's copyright line; empty for none. boxer's
	// copyright never stands in for it: without one, the program's tab
	// shows no copyright line.
	Copyright string
	// License is one line naming the program's licence; empty for none.
	License string
	// Notice is the program's NOTICE, shown verbatim; empty for none.
	Notice string
	// Module is the program's Go module path; when set, the About page
	// shows the version the build info records for it.
	Module string
	// Png is the artwork for the light theme, shown in its own colours and
	// scaled to the pane; transparent areas show the window behind. Empty
	// for none, and then the program's tab has no Splash page.
	Png []byte
	// PngDark is the artwork for the dark theme; Png is shown when it is
	// empty.
	PngDark []byte
}

// programState holds the registered program and its artwork decoded once
// per theme (index 0 light, 1 dark), on first use.
type programState struct {
	mu      sync.Mutex
	p       *Program
	decoded [2]decodedImage
	errs    [2]error
	done    [2]bool
	version uint64
}

var program programState

// SetProgram registers the program. Windows opened after the call credit
// it and are sized to its artwork; call it from init so every window does.
func SetProgram(p Program) (err error) {
	if p.Name == "" {
		err = eh.Errorf("program needs a name")
		return
	}
	if len(p.Png) == 0 && len(p.PngDark) > 0 {
		err = eb.Build().Str("name", p.Name).Errorf("program has dark artwork but no light artwork")
		return
	}
	program.mu.Lock()
	program.p = &p
	program.version++
	program.resetDecoded()
	program.mu.Unlock()
	applyWindowHints()
	return
}

// ClearProgram removes the program; windows opened afterwards credit boxer
// only.
func ClearProgram() {
	program.mu.Lock()
	program.p = nil
	program.resetDecoded()
	program.mu.Unlock()
	applyWindowHints()
}

// resetDecoded drops the decoded artwork; the caller holds mu.
func (inst *programState) resetDecoded() {
	inst.decoded = [2]decodedImage{}
	inst.errs = [2]error{}
	inst.done = [2]bool{}
}

// programRegistered reports whether a program is registered.
func programRegistered() (ok bool) {
	program.mu.Lock()
	defer program.mu.Unlock()
	ok = program.p != nil
	return
}

// programParty is the registered program as a credited party.
func programParty() (pt party, ok bool) {
	program.mu.Lock()
	defer program.mu.Unlock()
	if program.p == nil {
		return
	}
	p := program.p
	ok = true
	pt = party{
		key:       programKey,
		name:      p.Name,
		icon:      p.Icon,
		module:    p.Module,
		copyright: p.Copyright,
		license:   p.License,
		notice:    p.Notice,
	}
	if len(p.Png) > 0 {
		pt.image = programImage
	}
	return
}

// programImage returns the program's artwork for the active theme, decoded
// once per registration.
func programImage() (img decodedImage, err error) {
	dark := styletokens.ActiveTheme() != styletokens.ThemeFresh
	program.mu.Lock()
	defer program.mu.Unlock()
	if program.p == nil || len(program.p.Png) == 0 {
		err = eh.Errorf("no program artwork registered")
		return
	}
	i := 0
	src := program.p.Png
	if dark {
		i = 1
		if len(program.p.PngDark) > 0 {
			src = program.p.PngDark
		}
	}
	if !program.done[i] {
		program.decoded[i], program.errs[i] = decodePng(src)
		program.decoded[i].version = program.version
		program.done[i] = true
	}
	img, err = program.decoded[i], program.errs[i]
	return
}

// decodePng unpacks a PNG in its own colours, without the palette mapping
// boxer's artwork goes through.
func decodePng(data []byte) (img decodedImage, err error) {
	src, _, decErr := image.Decode(bytes.NewReader(data))
	if decErr != nil {
		err = eh.Errorf("decode program png: %w", decErr)
		return
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		err = eb.Build().Int("width", w).Int("height", h).Errorf("program png has empty bounds")
		return
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), src, b.Min, draw.Src)
	px := make([]uint32, w*h)
	for i := range px {
		o := i * 4
		px[i] = uint32(nrgba.Pix[o])<<24 | uint32(nrgba.Pix[o+1])<<16 | uint32(nrgba.Pix[o+2])<<8 | uint32(nrgba.Pix[o+3])
	}
	img = decodedImage{pixels: px, w: uint32(w), h: uint32(h)}
	return
}
