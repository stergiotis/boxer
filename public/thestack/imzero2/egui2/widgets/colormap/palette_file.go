package colormap

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// NamedPalette is a palette read from disk: a display name, its author when
// the file names one, and the 0xRRGGBBAA stops (length ≥ 2) that go into
// Config.Palette.
type NamedPalette struct {
	Name    string
	Author  string
	Palette []uint32
}

// paletteFile is the on-disk JSON shape — the SDR++ colormap convention
// (res/colormaps/*.json in github.com/AlexandreRouma/SDRPlusPlus): a name,
// an optional author, and "#RRGGBB" / "#RRGGBBAA" hex stops in order.
type paletteFile struct {
	Name   string   `json:"name"`
	Author string   `json:"author"`
	Map    []string `json:"map"`
}

// LoadPaletteDirE reads every *.json palette in dir, sorted by file name. A
// nonexistent dir yields no palettes and no error, so a caller can point at
// an optional user directory; a malformed file is an error naming the
// offending path. A file without a name takes its file-name stem.
func LoadPaletteDirE(dir string) (out []NamedPalette, err error) {
	entries, e := os.ReadDir(dir)
	if e != nil {
		if os.IsNotExist(e) {
			return nil, nil
		}
		return nil, eb.Build().Str("dir", dir).Errorf("read palette dir: %w", e)
	}
	names := make([]string, 0, len(entries))
	for _, ent := range entries {
		if !ent.IsDir() && filepath.Ext(ent.Name()) == ".json" {
			names = append(names, ent.Name())
		}
	}
	sort.Strings(names)
	out = make([]NamedPalette, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		raw, re := os.ReadFile(path)
		if re != nil {
			return nil, eb.Build().Str("path", path).Errorf("read palette: %w", re)
		}
		p, pe := ParsePaletteJSONE(raw, strings.TrimSuffix(name, ".json"))
		if pe != nil {
			return nil, eb.Build().Str("path", path).Errorf("palette file: %w", pe)
		}
		out = append(out, p)
	}
	return out, nil
}

// ParsePaletteJSONE parses one SDR++-style palette document. fallbackName is
// used when the document carries no name. Fewer than two valid stops is an
// error, since Config needs at least two.
func ParsePaletteJSONE(raw []byte, fallbackName string) (p NamedPalette, err error) {
	var pf paletteFile
	if e := json.Unmarshal(raw, &pf); e != nil {
		err = eh.Errorf("parse palette json: %w", e)
		return
	}
	stops := make([]uint32, 0, len(pf.Map))
	for _, h := range pf.Map {
		rgba, he := ParseHexRGBAE(h)
		if he != nil {
			err = eb.Build().Str("hex", h).Errorf("palette stop: %w", he)
			return
		}
		stops = append(stops, rgba)
	}
	if len(stops) < 2 {
		err = eb.Build().Int("stops", len(stops)).Errorf("palette needs at least 2 stops")
		return
	}
	name := pf.Name
	if name == "" {
		name = fallbackName
	}
	p = NamedPalette{Name: name, Author: pf.Author, Palette: stops}
	return
}

// ParseHexRGBAE parses "#RRGGBB" or "#RRGGBBAA" (leading # optional,
// surrounding whitespace ignored) into the 0xRRGGBBAA layout Config uses; a
// six-digit value is taken as fully opaque.
func ParseHexRGBAE(s string) (rgba uint32, err error) {
	t := strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(t) {
	case 6:
		v, e := strconv.ParseUint(t, 16, 32)
		if e != nil {
			err = eb.Build().Str("hex", s).Errorf("invalid hex: %w", e)
			return
		}
		rgba = uint32(v)<<8 | 0xff
	case 8:
		v, e := strconv.ParseUint(t, 16, 32)
		if e != nil {
			err = eb.Build().Str("hex", s).Errorf("invalid hex: %w", e)
			return
		}
		rgba = uint32(v)
	default:
		err = eb.Build().Str("hex", s).Errorf("hex stop must be 6 or 8 digits")
	}
	return
}
