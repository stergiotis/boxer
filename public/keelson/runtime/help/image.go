package help

import (
	"bytes"
	"image"
	"io/fs"
	"strings"

	// Format decoders: blank-imported so [image.Decode] auto-detects
	// PNG/JPEG/GIF from their magic prefixes. WebP / AVIF / BMP are
	// deliberately not pulled in — they add ~1 MB to every binary
	// shipping help docs, and the typical help asset is a PNG
	// screenshot or a JPEG photo. Apps that need exotic formats can
	// blank-import the relevant decoder themselves.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
)

// FSImageResolver decodes inline image references in help docs
// against a backing [fs.FS]. URL resolution for wikilinks and embeds
// is delegated to [resolver.NoopResolver]; only image bytes are
// served from the FS.
//
// Image refs in markdown — both CommonMark `![alt](path)` and
// Obsidian `![[file.png]]` — arrive as path strings rooted at the
// FS root. A leading `/` is stripped so `![](/logo.png)` and
// `![](logo.png)` resolve to the same FS entry; otherwise paths are
// interpreted verbatim against fs.ReadFile, which handles nested
// directories (`assets/diag.png`) naturally.
//
// Relative paths (`../assets/`) are intentionally not supported in
// M1 — every doc inside one [BookI] shares the same FS root, so
// vault-rooted refs are the unambiguous form. Future work can layer
// per-doc base-path resolution on top if authors want relative refs.
type FSImageResolver struct {
	resolver.NoopResolver
	fsys fs.FS
}

var _ resolver.ResolverI = FSImageResolver{}

// NewFSImageResolver constructs a resolver that loads images from
// fsys and inherits NoopResolver's URL behaviour for wikilinks /
// embeds. A nil fsys is valid — [LoadImage] returns ok=false for any
// ref, which matches NoopResolver's "no images" baseline.
func NewFSImageResolver(fsys fs.FS) (r FSImageResolver) {
	r = FSImageResolver{fsys: fsys}
	return
}

// LoadImage reads ref from the backing fs.FS and decodes it into
// RGBA8 pixels for the markdown widget's inline image run. ok=false
// (and a nil pixel slice) is returned when the file is missing, the
// FS is nil, or the bytes don't decode as one of the registered
// image formats — the markdown widget then falls back to the
// glyph-prefixed hyperlink rendering.
func (inst FSImageResolver) LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool) {
	if inst.fsys == nil {
		return
	}
	cleaned := strings.TrimPrefix(ref, "/")
	if cleaned == "" {
		return
	}
	data, err := fs.ReadFile(inst.fsys, cleaned)
	if err != nil {
		return
	}
	img, _, decodeErr := image.Decode(bytes.NewReader(data))
	if decodeErr != nil {
		return
	}
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return
	}
	pixels = make([]uint32, w*h)
	i := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			// RGBA() returns 16-bit components (0..65535). The
			// markdown widget consumes 0xRRGGBBAA in row-major
			// order — pack the 8-bit-truncated values into one
			// uint32 per pixel.
			pixels[i] = uint32(r>>8)<<24 | uint32(g>>8)<<16 | uint32(b>>8)<<8 | uint32(a>>8)
			i++
		}
	}
	widthPx = uint32(w)
	heightPx = uint32(h)
	ok = true
	return
}
