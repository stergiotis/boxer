// Package mddocvocab is the markdown-document leeway natural-key vocabulary:
// the memberships that tag a sent document in `boxer.facts`.
//
// It mirrors [github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab],
// which does the same job for metric samples in the same table; what keeps the
// vocabularies apart is the tag value below. The one kind written against
// these names lives in
// [github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts],
// with the ids resolved into its generated record store at generation time.
//
// The vocabulary began with one flow (ADR-0217): an editor hands a markdown
// document to the SQL playground by persisting it as a fact and opening play
// on a query that selects it back. The markdown ingestor (its own ADR) grew it
// into a document model: beside the whole-document kind, one kind per
// extracted item — heading, code block, link, emphasis, tag — each row
// pointing back at its document, and a frontmatter row carrying the YAML
// block exploded into (path, params, value) leaves. Append-shaped throughout —
// every ingest is a new set of rows, the entity is the content itself, and
// nothing here is ever updated or deleted.
package mddocvocab

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Contract is this vocabulary's leeway contract — the vcs-managed convention,
// matching every other vocabulary sharing the table.
var Contract = contract.NewVcsManagedContract()

// NamingStyle matches the peer vocabularies, so a query joining document rows
// to runtime facts reads the same way on both sides.
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, claimed from the width-32
// class every version-controlled vocabulary claims from (ADR-0183 D0). The
// mint refuses a value another package already claimed, and the committed
// assignment table beside this package is what makes a re-pointed id visible
// in review.
var TagValueClaim = tagmint.MustClaim("mddoc", 2178316, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
// One kind with a handful of attributes — the smallest hint the peers use.
const MaxExpectedMemberships = 1 << 16

// NkRegistry is the natural-key registry for mddoc memberships. The size is a
// capacity hint only.
var NkRegistry = registry.MustNewNaturalKeyRegistry(
	TagValueClaim, 32, NamingStyle, Contract,
)

// Membership constants for `boxer.facts` rows carrying markdown documents.
// Each states its ordinal: the number beside the name is the id's body, and
// rows already written carry it (ADR-0183 D0).
var (
	// MembKindMdDoc carries the kind label for readability; the membership id
	// is what identifies the kind. It exists so hand-written SQL can select
	// document rows without enumerating attributes.
	MembKindMdDoc = NkRegistry.MustBegin("mddocKind", 0).End()

	// MembTitle is the document's first heading text, "" when it has none.
	MembTitle = NkRegistry.MustBegin("mddocTitle", 1).End()

	// MembFileName is the display name the fs Powerbox named for the file the
	// document was opened from or saves to — a basename, never a path — or ""
	// for a scratch document.
	MembFileName = NkRegistry.MustBegin("mddocFileName", 2).End()

	// MembContent is the markdown source, verbatim.
	MembContent = NkRegistry.MustBegin("mddocContent", 3).End()

	// MembContentHash is the blake3-256 of the content, hex — the same
	// material the row's natural key carries, as a queryable column.
	MembContentHash = NkRegistry.MustBegin("mddocContentHash", 4).End()

	// MembWords is the sender's prose word count (markup excluded), a cheap
	// size signal for lists of sent documents.
	MembWords = NkRegistry.MustBegin("mddocWords", 5).End()
)

// Item kinds. Every item row carries the same spine under kind-specific
// names — the store generator requires each kind to own its memberships:
//
//   - <kind>Kind — the kind label, a symbol (the sysmfacts convention).
//   - <kind>Doc — the document row's id, on the foreignKey section; exact
//     for the ingest that wrote both.
//   - <kind>DocHash — the document's blake3-256 content hash, the same bytes
//     as the document row's natural key; the join that survives re-ingests
//     of identical content.
//   - <kind>Ordinal — 0-based position among the document's items of this
//     kind, in document order.
//   - <kind>Line — 1-based source line, 0 when unknown.
//   - <kind>Section — the ordinal of the heading the item sits under; absent
//     before the first heading.
var (
	MembKindHeading    = NkRegistry.MustBegin("mdHeadingKind", 6).End()
	MembHeadingDoc     = NkRegistry.MustBegin("mdHeadingDoc", 7).End()
	MembHeadingDocHash = NkRegistry.MustBegin("mdHeadingDocHash", 8).End()
	MembHeadingOrdinal = NkRegistry.MustBegin("mdHeadingOrdinal", 9).End()
	MembHeadingLine    = NkRegistry.MustBegin("mdHeadingLine", 10).End()
	// MembHeadingLevel is 1..6.
	MembHeadingLevel = NkRegistry.MustBegin("mdHeadingLevel", 11).End()
	// MembHeadingText is the heading's inline content as plain text.
	MembHeadingText = NkRegistry.MustBegin("mdHeadingText", 12).End()
	// MembHeadingSlug is the fragment key a `[[page#heading]]` resolves
	// against: lower-cased, spaces to hyphens; the explicit anchor when the
	// heading carries one.
	MembHeadingSlug = NkRegistry.MustBegin("mdHeadingSlug", 13).End()
	// MembHeadingAnchor is an explicit `{#anchor}`, absent otherwise.
	MembHeadingAnchor = NkRegistry.MustBegin("mdHeadingAnchor", 14).End()
	// MembHeadingParent is the ordinal of the enclosing heading, absent at
	// the top level.
	MembHeadingParent = NkRegistry.MustBegin("mdHeadingParent", 15).End()
	// MembHeadingPath lists the ancestors' texts, outermost first.
	MembHeadingPath = NkRegistry.MustBegin("mdHeadingPath", 16).End()

	MembKindCode    = NkRegistry.MustBegin("mdCodeKind", 17).End()
	MembCodeDoc     = NkRegistry.MustBegin("mdCodeDoc", 18).End()
	MembCodeDocHash = NkRegistry.MustBegin("mdCodeDocHash", 19).End()
	MembCodeOrdinal = NkRegistry.MustBegin("mdCodeOrdinal", 20).End()
	// MembCodeLine is the opening fence's line.
	MembCodeLine    = NkRegistry.MustBegin("mdCodeLine", 21).End()
	MembCodeSection = NkRegistry.MustBegin("mdCodeSection", 22).End()
	// MembCodeLanguage is the info string's first word, "" for a bare fence.
	MembCodeLanguage = NkRegistry.MustBegin("mdCodeLanguage", 23).End()
	// MembCodeInfo is the whole info string.
	MembCodeInfo = NkRegistry.MustBegin("mdCodeInfo", 24).End()
	// MembCodeContent is the fenced body, verbatim.
	MembCodeContent = NkRegistry.MustBegin("mdCodeContent", 25).End()
	// MembCodeLines is the body's line count.
	MembCodeLines = NkRegistry.MustBegin("mdCodeLines", 26).End()

	MembKindLink    = NkRegistry.MustBegin("mdLinkKind", 27).End()
	MembLinkDoc     = NkRegistry.MustBegin("mdLinkDoc", 28).End()
	MembLinkDocHash = NkRegistry.MustBegin("mdLinkDocHash", 29).End()
	MembLinkOrdinal = NkRegistry.MustBegin("mdLinkOrdinal", 30).End()
	MembLinkLine    = NkRegistry.MustBegin("mdLinkLine", 31).End()
	MembLinkSection = NkRegistry.MustBegin("mdLinkSection", 32).End()
	// MembLinkSpelling is how the link was written: wikilink, embed,
	// inline, image or autolink.
	MembLinkSpelling = NkRegistry.MustBegin("mdLinkSpelling", 33).End()
	// MembLinkTarget is the destination without its fragment: a page name
	// for a wikilink or embed, a path or URL otherwise.
	MembLinkTarget = NkRegistry.MustBegin("mdLinkTarget", 34).End()
	// MembLinkFragment is the `#heading` part, "" when none.
	MembLinkFragment = NkRegistry.MustBegin("mdLinkFragment", 35).End()
	// MembLinkText is the alias or label as written, "" when the link shows
	// its target.
	MembLinkText = NkRegistry.MustBegin("mdLinkText", 36).End()
	// MembLinkExternal is true when the target carries a URL scheme.
	MembLinkExternal = NkRegistry.MustBegin("mdLinkExternal", 37).End()

	MembKindEmphasis    = NkRegistry.MustBegin("mdEmphasisKind", 38).End()
	MembEmphasisDoc     = NkRegistry.MustBegin("mdEmphasisDoc", 39).End()
	MembEmphasisDocHash = NkRegistry.MustBegin("mdEmphasisDocHash", 40).End()
	MembEmphasisOrdinal = NkRegistry.MustBegin("mdEmphasisOrdinal", 41).End()
	MembEmphasisLine    = NkRegistry.MustBegin("mdEmphasisLine", 42).End()
	MembEmphasisSection = NkRegistry.MustBegin("mdEmphasisSection", 43).End()
	// MembEmphasisStyle is bold, italic, highlight or strikethrough.
	MembEmphasisStyle = NkRegistry.MustBegin("mdEmphasisStyle", 44).End()
	MembEmphasisText  = NkRegistry.MustBegin("mdEmphasisText", 45).End()

	MembKindTag    = NkRegistry.MustBegin("mdTagKind", 46).End()
	MembTagDoc     = NkRegistry.MustBegin("mdTagDoc", 47).End()
	MembTagDocHash = NkRegistry.MustBegin("mdTagDocHash", 48).End()
	MembTagOrdinal = NkRegistry.MustBegin("mdTagOrdinal", 49).End()
	// MembTagLine is 0 for a frontmatter tag.
	MembTagLine    = NkRegistry.MustBegin("mdTagLine", 50).End()
	MembTagSection = NkRegistry.MustBegin("mdTagSection", 51).End()
	// MembTagSource is body or frontmatter.
	MembTagSource = NkRegistry.MustBegin("mdTagSource", 52).End()
	// MembTagName is the tag without its "#", nesting kept.
	MembTagName = NkRegistry.MustBegin("mdTagName", 53).End()
)

// The frontmatter row. One per document that has a YAML block, written
// through the raw DML rather than a generated component: its leaves ride the
// mixed membership channel, which the generated lane does not read.
//
// A leaf is one attribute in the section of its YAML type — stringArray,
// i64Array, f64Array, bool, timeArray, and symbolArray for the value-less markers
// `null`, `[]` and `{}` — carrying two memberships on the mixed channel:
// MembFrontmatterPath, whose parameter is the path with array positions
// elided to "_", and MembFrontmatterParams, whose parameter is the elided
// indices in the params codec's canonical form, attached only when the path
// crosses an array. That is the canonical leeway JSON mapping (path verbatim
// on lmv, indices on mvhp) translated into a schema that has no verbatim
// channel — the jsonbench trial's construction, and the reason the path is
// stored per attribute rather than dictionary-encoded.
var (
	MembKindFrontmatter    = NkRegistry.MustBegin("mdFrontmatterKind", 54).End()
	MembFrontmatterDoc     = NkRegistry.MustBegin("mdFrontmatterDoc", 55).End()
	MembFrontmatterDocHash = NkRegistry.MustBegin("mdFrontmatterDocHash", 56).End()
	MembFrontmatterPath    = NkRegistry.MustBegin("mdFrontmatterPath", 57).End()
	MembFrontmatterParams  = NkRegistry.MustBegin("mdFrontmatterParams", 58).End()
)
