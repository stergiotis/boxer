package help

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"testing/fstest"
)

// tinyPNG encodes a 2x2 solid-red NRGBA image as PNG bytes for
// resolver round-trip tests. Keeping the fixture in-memory avoids
// shipping a binary asset in testdata/.
func tinyPNG(t *testing.T) (data []byte, widthPx, heightPx uint32) {
	t.Helper()
	const w, h = 2, 2
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	red := color.NRGBA{R: 255, G: 0, B: 0, A: 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	data = buf.Bytes()
	widthPx = w
	heightPx = h
	return
}

func TestFSImageResolver_LoadImage(t *testing.T) {
	data, wantW, wantH := tinyPNG(t)
	fsys := fstest.MapFS{
		"logo.png": {Data: data},
	}
	r := NewFSImageResolver(fsys)
	pixels, w, h, ok := r.LoadImage("logo.png")
	if !ok {
		t.Fatalf("LoadImage: ok=false; want ok=true")
	}
	if w != wantW || h != wantH {
		t.Errorf("dims: got (%d,%d), want (%d,%d)", w, h, wantW, wantH)
	}
	if len(pixels) != int(wantW*wantH) {
		t.Fatalf("pixels: got len=%d, want %d", len(pixels), wantW*wantH)
	}
	// All four pixels are solid red (0xFF0000FF in RRGGBBAA).
	for i, p := range pixels {
		if p != 0xFF0000FF {
			t.Errorf("pixels[%d] = %08x, want %08x", i, p, uint32(0xFF0000FF))
		}
	}
}

func TestFSImageResolver_LoadImage_LeadingSlash(t *testing.T) {
	data, _, _ := tinyPNG(t)
	fsys := fstest.MapFS{"logo.png": {Data: data}}
	r := NewFSImageResolver(fsys)
	_, _, _, ok := r.LoadImage("/logo.png")
	if !ok {
		t.Errorf("LoadImage(/logo.png) ok=false; leading-slash should be stripped")
	}
}

func TestFSImageResolver_LoadImage_Missing(t *testing.T) {
	r := NewFSImageResolver(fstest.MapFS{})
	pixels, _, _, ok := r.LoadImage("missing.png")
	if ok {
		t.Errorf("LoadImage(missing): ok=true, want false")
	}
	if pixels != nil {
		t.Errorf("LoadImage(missing): pixels non-nil")
	}
}

func TestFSImageResolver_LoadImage_NilFS(t *testing.T) {
	r := NewFSImageResolver(nil)
	_, _, _, ok := r.LoadImage("anything.png")
	if ok {
		t.Errorf("LoadImage on nil FS: ok=true, want false")
	}
}

func TestFSImageResolver_LoadImage_NotAnImage(t *testing.T) {
	r := NewFSImageResolver(fstest.MapFS{
		"fake.png": {Data: []byte("not actually a png")},
	})
	_, _, _, ok := r.LoadImage("fake.png")
	if ok {
		t.Errorf("LoadImage on non-image bytes: ok=true, want false")
	}
}

func TestFSImageResolver_InheritsNoopWikilink(t *testing.T) {
	r := NewFSImageResolver(fstest.MapFS{})
	url, exists := r.ResolveWikilink("some page", "anchor")
	if !exists {
		t.Errorf("ResolveWikilink: exists=false; NoopResolver always reports exists=true")
	}
	if url == "" {
		t.Errorf("ResolveWikilink: empty url")
	}
}
