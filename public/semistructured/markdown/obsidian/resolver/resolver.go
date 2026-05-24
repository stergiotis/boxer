//go:build llm_generated_opus46

package resolver

import "strings"

// ResolverI resolves wikilink and embed references to URLs and, for
// image consumers, decodes image bytes into RGBA8 pixels.
//
// The library does not know the vault structure; consumers provide this.
//
// LoadImage is consumed by renderers that splice an image *widget* into
// the output (e.g. imzero2's markdown widget). HTML-style renderers that
// only emit `<img src="...">` ignore LoadImage and stay on
// ResolveEmbed's URL alone, so resolver implementations that have no
// pixel source can return ok=false from LoadImage and remain useful for
// the HTML path.
type ResolverI interface {
	// ResolveWikilink returns the URL for a wikilink target.
	// page is the linked page name, heading is the optional section anchor (may be empty).
	ResolveWikilink(page string, heading string) (url string, exists bool)

	// ResolveEmbed returns the URL and type for an embed target.
	// target is the file/note name, heading is an optional section anchor (may be empty).
	ResolveEmbed(target string, heading string) (url string, isImage bool, exists bool)

	// LoadImage decodes the image referenced by ref into RGBA8 pixels.
	// pixels is row-major in (heightPx × widthPx) order, with each
	// uint32 packed as 0xRRGGBBAA.
	//
	// ref is either:
	//   - the raw URL from a CommonMark image (![alt](url))
	//   - the embed target from an Obsidian embed (![[file.png]])
	//
	// Returning ok=false instructs the consumer to fall back to the
	// pre-image-widget rendering (typically a glyph-prefixed hyperlink).
	// Use it for refs the resolver does not recognise (e.g. http:// URLs
	// in a vault-only resolver) or for formats the resolver cannot
	// decode.
	LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool)
}

// NoopResolver generates fragment-only links without vault knowledge.
// Useful for previewing or testing.
type NoopResolver struct{}

var _ ResolverI = NoopResolver{}

func (inst NoopResolver) ResolveWikilink(page string, heading string) (url string, exists bool) {
	url = "/" + sanitizePath(page)
	if heading != "" {
		url += "#" + sanitizeFragment(heading)
	}
	exists = true
	return
}

func (inst NoopResolver) ResolveEmbed(target string, heading string) (url string, isImage bool, exists bool) {
	url = "/" + sanitizePath(target)
	if heading != "" {
		url += "#" + sanitizeFragment(heading)
	}
	isImage = IsImageFile(target)
	exists = true
	return
}

// LoadImage returns ok=false unconditionally: NoopResolver has no asset
// store, so widget-based renderers fall back to the pre-image-widget
// glyph-hyperlink rendering. Callers that want real inline images
// should supply a custom [ResolverI] whose LoadImage decodes bytes
// (e.g. from disk relative to a vault root).
func (inst NoopResolver) LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool) {
	return
}

func sanitizePath(s string) string {
	return strings.ReplaceAll(s, " ", "%20")
}

func sanitizeFragment(s string) string {
	r := strings.ToLower(s)
	r = strings.ReplaceAll(r, " ", "-")
	return r
}

func IsImageFile(target string) bool {
	lower := strings.ToLower(target)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp", ".avif"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
