package help

import (
	"io/fs"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
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
// FS is nil, or the bytes don't decode as one of the formats
// [imagedecode.DecodeRGBA8] registers — the markdown widget then falls
// back to the glyph-prefixed hyperlink rendering.
//
// The decode is bounded by [imagedecode.DefaultMaxPixels] even though
// help assets ship inside the binary and are not attacker-supplied: a
// bound costs one header read, and FSImageResolver is constructed from
// whatever fs.FS a caller hands it, which need not be an embed.FS.
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
	pixels, widthPx, heightPx, err = imagedecode.DecodeRGBA8(data, imagedecode.DefaultMaxPixels)
	if err != nil {
		pixels = nil
		widthPx = 0
		heightPx = 0
		return
	}
	ok = true
	return
}
