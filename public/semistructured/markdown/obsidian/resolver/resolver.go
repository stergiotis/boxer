package resolver

import "strings"

// ResolverI resolves wikilink and embed references to URLs.
// The library does not know the vault structure; consumers provide this.
type ResolverI interface {
	// ResolveWikilink returns the URL for a wikilink target.
	// page is the linked page name, heading is the optional section anchor (may be empty).
	ResolveWikilink(page string, heading string) (url string, exists bool)

	// ResolveEmbed returns the URL and type for an embed target.
	// target is the file/note name, heading is an optional section anchor (may be empty).
	ResolveEmbed(target string, heading string) (url string, isImage bool, exists bool)
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
