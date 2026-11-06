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
	//
	// Ownership: the returned pixels slice is retained by the consumer
	// for as long as the surrounding document lives; resolvers must not
	// mutate or pool it across calls.
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
	r := strings.ToLower(FoldTypography(s))
	r = strings.ReplaceAll(r, " ", "-")
	return r
}

// typographyFolds maps the typographic punctuation the obsidian package's
// FeatureTypographer sets back to what an author types, so a fragment
// written either way names the same heading. It mirrors the package's
// substitution table, which a test there holds to it.
var typographyFolds = strings.NewReplacer(
	"\u2018", "'", "\u2019", "'",
	"\u201c", "\"", "\u201d", "\"",
	"\u2014", "---", "\u2013", "--",
	"\u2026", "...",
)

// FoldTypography returns s with typographic quotes, dashes and the
// ellipsis replaced by the characters an author types for them; a heading
// set by the typographer and a link naming it by its source text meet in
// one fragment. Exported so the markdown widget's [SlugHeading] applies
// the same fold the resolver does.
func FoldTypography(s string) string {
	return typographyFolds.Replace(s)
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
