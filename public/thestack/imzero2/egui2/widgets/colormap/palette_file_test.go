package colormap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"pgregory.net/rapid"
)

func TestLoadPaletteDirE(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("b_two.json", `{"name":"two","author":"a","map":["#ff0000","00ff00ff"]}`)
	write("a_one.json", `{"map":["#000000","#ffffff"]}`)
	write("ignored.txt", `not a palette`)

	ps, err := LoadPaletteDirE(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("got %d palettes, want 2", len(ps))
	}
	if ps[0].Name != "a_one" {
		t.Errorf("ps[0].Name = %q, want the file stem (sorted by file name)", ps[0].Name)
	}
	if ps[1].Name != "two" || ps[1].Author != "a" {
		t.Errorf("ps[1] = %+v, want name two / author a", ps[1])
	}
	if want := []uint32{0xff0000ff, 0x00ff00ff}; len(ps[1].Palette) != 2 || ps[1].Palette[0] != want[0] || ps[1].Palette[1] != want[1] {
		t.Errorf("ps[1].Palette = %#v, want %#v", ps[1].Palette, want)
	}
	// The result feeds Config directly.
	cfg := NewConfig(ps[1].Palette, 0, 1)
	if len(cfg.Palette) != 2 {
		t.Errorf("NewConfig did not accept the loaded palette")
	}
}

func TestLoadPaletteDirEMissingDirIsEmpty(t *testing.T) {
	ps, err := LoadPaletteDirE(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil || ps != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", ps, err)
	}
}

func TestLoadPaletteDirERejectsBad(t *testing.T) {
	for name, body := range map[string]string{
		"short.json":  `{"map":["#ffffff"]}`,
		"badhex.json": `{"map":["#ffffff","#zzzzzz"]}`,
		"syntax.json": `{"map":[`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPaletteDirE(dir); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseHexRGBAE(t *testing.T) {
	cases := map[string]uint32{
		"#ff0000":   0xff0000ff,
		"ff0000":    0xff0000ff,
		" #00FF00 ": 0x00ff00ff,
		"#12345678": 0x12345678,
	}
	for in, want := range cases {
		got, err := ParseHexRGBAE(in)
		if err != nil || got != want {
			t.Errorf("ParseHexRGBAE(%q) = (%#x, %v), want %#x", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "#fff", "#gggggg", "#123456789"} {
		if _, err := ParseHexRGBAE(bad); err == nil {
			t.Errorf("ParseHexRGBAE(%q): want an error", bad)
		}
	}
}

// Formatting a 0xRRGGBBAA value as "#RRGGBBAA" and parsing it back is the
// identity, and the six-digit form is the eight-digit form with alpha 0xff.
func TestParseHexRGBAERoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Uint32().Draw(t, "rgba")
		got, err := ParseHexRGBAE(fmt.Sprintf("#%08x", v))
		if err != nil || got != v {
			t.Fatalf("8-digit round trip of %#08x: (%#08x, %v)", v, got, err)
		}
		rgb := v >> 8
		got, err = ParseHexRGBAE(fmt.Sprintf("%06X", rgb))
		if err != nil || got != rgb<<8|0xff {
			t.Fatalf("6-digit round trip of %#06x: (%#08x, %v)", rgb, got, err)
		}
	})
}
