package mdextract

import "time"

// Document is the extracted reading of one markdown source.
//
// Ordinals are 0-based positions in document order within one item kind;
// Line is 1-based and 0 when the position is unknown. Section fields index
// [Document.Headings] and are -1 for items before the first heading.
type Document struct {
	// Title is the first heading with text, "" when there is none.
	Title string
	// Words counts whitespace-separated runs across every text node of the
	// parsed body: prose, headings, link labels, inline code. Fenced and
	// indented code bodies and the frontmatter are not text nodes and are
	// not counted.
	Words uint64

	// Frontmatter is nil when the document opens with no `---` block.
	Frontmatter *Frontmatter

	Headings   []Heading
	CodeBlocks []CodeBlock
	Links      []Link
	Emphases   []Emphasis
	Tags       []Tag
}

// Frontmatter is the YAML block at the top of the document, exploded.
type Frontmatter struct {
	// Leaves is the mlvhp explosion, in sorted-key document order. Empty
	// when Err is set.
	Leaves []Leaf
	// Aliases is Obsidian's `aliases` (or `alias`) property, whether it was
	// written as a list or as a comma-separated string.
	Aliases []string
	// Dropped counts leaves the params codec could not address — an array
	// longer than the codec's index range. The rest of the document is
	// unaffected.
	Dropped uint64
	// Err is the YAML error text when the block did not parse; the document
	// body is still extracted.
	Err string
}

// LeafKindE is the YAML type of one frontmatter leaf, which decides the
// section it is stored in.
type LeafKindE uint8

const (
	LeafKindString LeafKindE = iota
	LeafKindInt
	LeafKindFloat
	LeafKindBool
	// LeafKindTime is a YAML timestamp or an ISO 8601 date or date-time. The
	// decoder hands such values back as text, so they are recognised on the
	// string by [ParseTimestamp]; the original spelling stays in S.
	LeafKindTime
	// LeafKindNull, LeafKindEmptyObject and LeafKindEmptyArray carry no value:
	// the kind is the value. They keep the explosion lossless, the way the
	// canonical mapping's `null` / `emptyObject` / `emptyArray` sections do.
	LeafKindNull
	LeafKindEmptyObject
	LeafKindEmptyArray
)

// String is the marker a value-less leaf is stored as.
func (inst LeafKindE) String() string {
	switch inst {
	case LeafKindString:
		return "string"
	case LeafKindInt:
		return "int"
	case LeafKindFloat:
		return "float"
	case LeafKindBool:
		return "bool"
	case LeafKindTime:
		return "time"
	case LeafKindNull:
		return "null"
	case LeafKindEmptyObject:
		return "{}"
	case LeafKindEmptyArray:
		return "[]"
	}
	return "unknown"
}

// Leaf is one (path, params, value) triple.
type Leaf struct {
	// Path is the low-cardinality half of the address: a JSON-pointer-style
	// path (RFC 6901 escaping of "~" and "/" in keys) with every array
	// position replaced by "_".
	Path string
	// Params holds the elided array indices, outermost first. Nil when the
	// path crosses no array.
	Params []uint64
	Kind   LeafKindE

	// S is the string value, and for a time leaf the text it was read from.
	S string
	I int64
	F float64
	B bool
	T time.Time
}

// Heading is one ATX or setext heading.
type Heading struct {
	Ordinal uint64
	Line    uint64
	Level   uint8
	// Text is the heading's inline content flattened to plain text.
	Text string
	// Slug is the fragment a wikilink `[[page#heading]]` resolves against:
	// lower-cased, spaces to hyphens — the same rule the imzero2 markdown
	// widget applies, so both sides meet on one key.
	Slug string
	// Anchor is an explicit `{#anchor}`, "" when none. When present it is
	// what Slug was derived from.
	Anchor string
	// Parent indexes the enclosing heading (the nearest earlier heading of
	// a lower level), -1 at the top.
	Parent int
	// Path is the texts of the ancestors, outermost first, self excluded.
	Path []string
}

// CodeBlock is one fenced code block. Indented code is not collected.
type CodeBlock struct {
	Ordinal uint64
	// Line is the opening fence's line.
	Line    uint64
	Section int
	// Language is the first word of the info string; Info is the whole of it.
	Language string
	Info     string
	Content  string
	Lines    uint64
}

// LinkKindE is the spelling a link was written in.
type LinkKindE uint8

const (
	LinkKindWikilink LinkKindE = iota // [[page#heading|alias]]
	LinkKindEmbed                     // ![[target#heading]]
	LinkKindInline                    // [text](target)
	LinkKindImage                     // ![alt](target)
	LinkKindAutolink                  // <https://…> or a bare URL under GFM
)

func (inst LinkKindE) String() string {
	switch inst {
	case LinkKindWikilink:
		return "wikilink"
	case LinkKindEmbed:
		return "embed"
	case LinkKindInline:
		return "inline"
	case LinkKindImage:
		return "image"
	case LinkKindAutolink:
		return "autolink"
	}
	return "unknown"
}

// Link is one outgoing reference.
type Link struct {
	Ordinal uint64
	Line    uint64
	Section int
	Kind    LinkKindE
	// Target is the destination without its fragment: a page name for a
	// wikilink or embed, a path or URL otherwise. Non-URL targets are
	// percent-decoded so `my%20note.md` and `my note.md` are one target.
	Target string
	// Fragment is the `#heading` part, "" when none. For a wikilink it is
	// as written; for an inline link it is the URL fragment.
	Fragment string
	// Text is the alias or label as written, "" when the link shows its
	// target.
	Text string
	// External is true when Target carries a URL scheme.
	External bool
}

// EmphasisStyleE is the inline styling of a span.
type EmphasisStyleE uint8

const (
	EmphasisStyleBold          EmphasisStyleE = iota // **text** / __text__
	EmphasisStyleItalic                              // *text* / _text_
	EmphasisStyleHighlight                           // ==text==
	EmphasisStyleStrikethrough                       // ~~text~~
)

func (inst EmphasisStyleE) String() string {
	switch inst {
	case EmphasisStyleBold:
		return "bold"
	case EmphasisStyleItalic:
		return "italic"
	case EmphasisStyleHighlight:
		return "highlight"
	case EmphasisStyleStrikethrough:
		return "strikethrough"
	}
	return "unknown"
}

// Emphasis is one styled span. Nested styles (`***text***`) yield one entry
// per style with the same text.
type Emphasis struct {
	Ordinal uint64
	Line    uint64
	Section int
	Style   EmphasisStyleE
	Text    string
}

// TagSourceE says where a tag was written.
type TagSourceE uint8

const (
	TagSourceBody        TagSourceE = iota // #tag in the text
	TagSourceFrontmatter                   // the `tags` property
)

func (inst TagSourceE) String() string {
	switch inst {
	case TagSourceBody:
		return "body"
	case TagSourceFrontmatter:
		return "frontmatter"
	}
	return "unknown"
}

// Tag is one tag occurrence. Tag is the name without its `#`, nesting kept
// (`project/alpha`); resolution of a parent tag to its children is a prefix
// question for the reader.
type Tag struct {
	Ordinal uint64
	// Line is 0 for a frontmatter tag.
	Line    uint64
	Section int
	Source  TagSourceE
	Tag     string
}
