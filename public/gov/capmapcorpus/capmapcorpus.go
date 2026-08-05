// Package capmapcorpus reads a business-capability vault into a queryable
// model: one [Competence] per unit of what the business can do, and one
// [Relation] per link between them.
//
// # "competence", not "capability"
//
// The literature calls these units *business capabilities*, and so does the
// vault: its files are `capability.md`, its links are `[[slug/capability]]`.
// boxer cannot. "Capability" is already taken here — ADR-0026 gives it to the
// runtime's security capabilities, the things `capslock` audits and a
// `capabilitygrant` records — and one word for two unrelated ideas in one tree
// makes every mention ambiguous.
//
// So this package is the translation point. Everything on the vault side of it
// keeps the industry's word; everything boxer names — this model, the
// `capmap*` membership vocabulary, the `competence` keelson tables — says
// **competence** (ADR-0168 §SD6). Reading a `capability.md` into a [Competence]
// is not an inconsistency; it is the boundary doing its job.
//
// The vault is Obsidian-shaped markdown and is the source of truth
// (ADR-0168 §SD3). A competence that has children is a directory holding a
// `capability.md`; a leaf is a plain `{slug}.md` beside its siblings. The slug
// is the directory name or the filename without its extension, normalised so
// that `AdversarialRobustness`, `adversarial_robustness` and
// `adversarial-robustness` are one competence.
//
// # Point it at a competence tree
//
// [ParseDir] takes the root of a *competence* tree, not the root of an
// Obsidian vault that happens to contain one. Slugs are the corpus's identity
// and are unique within a read, whereas a vault commonly keeps other note
// kinds — standards, technologies — whose names overlap competence names by
// design. On the vault this model was built against, a `Change-Data-Capture`
// technology note and a `change-data-capture` competence both exist, and
// reading them as one namespace is a slug collision rather than a corpus.
//
// # Two grains, deliberately
//
// Relations are their own rows rather than array columns on a competence
// (ADR-0168 §SD2). A link has attributes of its own — a similarity score, the
// description section a wikilink was found in, whether its target resolved —
// and there is nowhere to put those on an endpoint. It also makes level-4
// multi-parenting ordinary rather than a special case: a competence with three
// parents has three [Relation] rows.
//
// That reshaping is what retires the four parallel lint arrays the vault
// tooling used to carry. A broken link is not a separate finding to be
// computed and stored; it is a [Relation] whose [Relation.Resolution] is
// [ResolutionUnresolved]. Lint is a filter over relations
// (see [UnresolvedRelations]), not a fifth pass.
//
// # Prose stays prose
//
// A competence's markdown body is kept as [Section] values — a heading and its
// text — rather than decomposed into a parsed syntax tree (ADR-0168 §SD5).
// This is the shape leeway already uses for text documents: a sequence of
// labelled chunks, where the label is the membership. What the tree carries
// that is worth querying is its links, and those are extracted into
// [Relation]s; the remaining prose is prose. Keeping the source verbatim is
// also what lets the vault be regenerated from the model.
//
// # Scope
//
// The package is a pure library over markdown: no ClickHouse, no CLI, no
// Arrow. Those belong to its consumers — `boxer capmap` adds the ingest verb
// that writes these rows into `boxer.facts`, and the keelson providers expose
// the decoded result as tables.
package capmapcorpus

import (
	"time"
)

// Competence is one unit of what the business can do: the frontmatter
// metadata, and the body as labelled prose. The vault calls it a business
// capability; see the package doc for why boxer does not.
//
// It carries no numeric id. Identity is [Competence.NaturalKey], a blake3
// digest over the normalised slug, which is what the facts table keys on; the
// `id` column there is assigned by the store at insert. Consumers that need to
// join two competences do it by [Competence.Slug], which is what [Relation]
// endpoints name.
type Competence struct {
	// Slug is the normalised identifier, unique within the vault.
	Slug string
	// NaturalKey is blake3-16 over the normalised slug (see [NaturalKey]).
	NaturalKey []byte
	// VaultPath is the file's path relative to the vault root, which is what
	// a reader needs to open it for editing.
	VaultPath string

	Name     string
	Abbrev   string
	Synopsis string
	Domain   string
	Catalog  string
	Owner    string

	// Level is the hierarchy depth: 1 macro, 2 meso, 3 micro, 4 building
	// block. Level 4 may have several parents.
	Level uint8
	// Maturity and Pain are 0..5, or [NotAssessed] when no judgement has been
	// recorded. The distinction matters: an unassessed competence is not a
	// zero-maturity one, and averaging over the sentinel would be wrong.
	Maturity uint8
	Pain     uint8

	// Sections are the body's h1-delimited chunks in document order. Headings
	// are not constrained to a fixed set — an unrecognised one is kept rather
	// than dropped, so the vault round-trips.
	Sections []Section
	// Lifecycle holds only the phases that carry a record. A phase nothing has
	// been recorded for is absent rather than zero-valued.
	Lifecycle []LifecycleEvent
}

// Corpus is one read of a vault: the competences it defines, the relations
// they declare, and the files that were not competences.
//
// Skipped is part of the result rather than a log line because a vault is
// commonly a mixed tree. The one this model was built against keeps 1,804
// competences alongside 820 reference notes — standards and technologies that
// competences cite — and those notes are named as citations (`AMQP-1.0`,
// `Jouppi-1990`), which no competence slug can be. Reporting them lets a
// caller tell "pointed at the wrong directory" from "this tree also holds
// reference notes", which a silent skip would not.
type Corpus struct {
	Competences []Competence
	Relations   []Relation
	Skipped     []SkippedFile
}

// SkippedFile is a markdown file the read did not treat as a competence.
type SkippedFile struct {
	// Path is relative to the vault root.
	Path string
	// Reason says why in one clause, suitable for showing to an operator.
	Reason string
}

// NotAssessed is the Maturity / Pain sentinel for "no judgement recorded".
// It is deliberately far from the 0..5 scale so a reader that forgets to
// filter it produces an obviously wrong answer rather than a plausible one.
const NotAssessed uint8 = 255

// Section is one h1-delimited chunk of a competence's body: the heading, and
// the markdown beneath it with surrounding whitespace trimmed.
type Section struct {
	Heading string
	Text    string
}

// LifecycleEvent records that a competence reached a phase — who moved it
// there and when. Either field may be empty or zero: the vault often carries a
// date with no name, or a name with no date.
type LifecycleEvent struct {
	Phase PhaseE
	By    string
	At    time.Time
}

// PhaseE is a competence lifecycle phase — where a competence sits in its
// organisational life, as distinct from how well it is executed, which is
// Maturity.
type PhaseE string

// The eight phases, in lifecycle order. [AllPhases] preserves that order.
const (
	PhaseIdentified  PhaseE = "identified"
	PhaseDefined     PhaseE = "defined"
	PhaseAssessed    PhaseE = "assessed"
	PhasePlanned     PhaseE = "planned"
	PhaseBuilding    PhaseE = "building"
	PhaseOperational PhaseE = "operational"
	PhaseOptimizing  PhaseE = "optimizing"
	PhaseRetiring    PhaseE = "retiring"
)

// AllPhases lists the phases in lifecycle order, which is also the order
// frontmatter keys are read in and the order a reader should present them.
func AllPhases() (phases []PhaseE) {
	return []PhaseE{
		PhaseIdentified, PhaseDefined, PhaseAssessed, PhasePlanned,
		PhaseBuilding, PhaseOperational, PhaseOptimizing, PhaseRetiring,
	}
}

// Relation is one link between two competences — the second grain of the
// model, and its own row.
//
// Endpoints are slugs rather than ids because the corpus assigns no ids; an
// ingest maps them to whatever key its store uses. Source always exists (it is
// the competence the link was read from); Target may not, which is exactly
// what [Relation.Resolution] reports.
type Relation struct {
	// SourceSlug is the competence the link was declared in.
	SourceSlug string
	// Target is the link target: a normalised slug when the text could be one,
	// and the raw text verbatim when it could not (see [ResolutionExternal]).
	// It is kept in every case — for a broken link the target is the useful
	// part of the finding, and for a citation it is the citation key.
	Target     string
	Kind       RelationKindE
	Resolution ResolutionE

	// Section is the heading a [RelationKindWikilink] was found under, which
	// makes "what does this competence's Standards section cite" answerable.
	// Empty for the frontmatter-derived kinds.
	Section string
	// Ncd is the normalised compression distance for
	// [RelationKindSimilar], and zero otherwise.
	Ncd float64

	// qualified records that the link was written as `[[slug/capability]]`
	// rather than `[[slug]]` — the vault's spelling, kept verbatim. Both name
	// the same competence, so Target is the same either way; the spelling only
	// decides whether the link also resolves inside Obsidian, which is what
	// keeps a correctly-written link from being reported as [ResolutionDirRef].
	// Unexported because it is a property of the source text, not of the
	// relation.
	qualified bool
}

// RelationKindE distinguishes where a relation came from, which is also what
// it means.
type RelationKindE string

const (
	// RelationKindParent is a hierarchy edge from frontmatter `parent_ids`.
	RelationKindParent RelationKindE = "parent"
	// RelationKindSimilar is a scored resemblance from frontmatter `similar`.
	RelationKindSimilar RelationKindE = "similar"
	// RelationKindWikilink is a `[[link]]` found in a body section, and
	// carries the section it was found under.
	RelationKindWikilink RelationKindE = "wikilink"
)

// ResolutionE reports whether a relation's target was found in the corpus, and
// how.
//
// The zero value is [ResolutionUnresolved] on purpose: a relation that no pass
// has resolved reads as broken rather than as fine, so forgetting to resolve
// cannot silently produce a clean lint.
type ResolutionE uint8

const (
	// ResolutionUnresolved means the target is a well-formed slug that no
	// competence carries — a broken link, and the only state that is a defect.
	//
	// It is an upper bound on the defects rather than the defect count: a link
	// into a sibling reference tree the corpus deliberately does not read
	// lands here too, and nothing in the data separates the two. On the
	// reference corpus that split was 217 genuinely dangling against 1,481
	// pointing at sibling trees.
	ResolutionUnresolved ResolutionE = iota
	// ResolutionDirect means the target was found by its own slug.
	ResolutionDirect
	// ResolutionDirRef means the target exists, but only as a directory-backed
	// competence — stored at `{slug}/capability.md`. The link resolves in this
	// model and dangles in Obsidian, which looks for `{slug}.md`. Worth
	// separating from Direct because the fix is mechanical: write
	// `[[slug/capability]]`.
	ResolutionDirRef
	// ResolutionExternal means the target is not a well-formed competence slug
	// at all, so it names something outside the corpus — a cited paper, a
	// regulation, an RFC, a decision record.
	//
	// This is a category, not a rounding error: on this repository's own
	// competence catalog roughly a quarter of body links are of this kind,
	// almost all of them in the Standards and Obligations sections, which are
	// bibliographies. Counting them as broken links would bury the real ones.
	ResolutionExternal
)

// String renders the resolution as the stable lower-case token consumers
// store and query by.
//
// The names are part of the data once a corpus has been ingested, so they are
// spelled out here rather than derived from the identifiers: renaming a
// constant must not silently rewrite what a stored row says.
func (inst ResolutionE) String() (s string) {
	switch inst {
	case ResolutionDirect:
		return "direct"
	case ResolutionDirRef:
		return "dirref"
	case ResolutionExternal:
		return "external"
	case ResolutionUnresolved:
		return "unresolved"
	default:
		return "unknown"
	}
}
