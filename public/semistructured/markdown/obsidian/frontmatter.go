package obsidian

import (
	"github.com/yuin/goldmark/parser"
	meta "github.com/yuin/goldmark-meta"
)

// GetFrontmatter extracts YAML frontmatter metadata from a parser.Context
// after rendering. Returns nil if no frontmatter was present.
func GetFrontmatter(pc parser.Context) map[string]interface{} {
	return meta.Get(pc)
}

// TryGetFrontmatter extracts YAML frontmatter metadata from a parser.Context
// after rendering. Returns an error if the YAML was malformed.
func TryGetFrontmatter(pc parser.Context) (metadata map[string]interface{}, err error) {
	metadata, err = meta.TryGet(pc)
	return
}
