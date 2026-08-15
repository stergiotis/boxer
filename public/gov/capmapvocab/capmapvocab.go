// Package capmapvocab is the competence corpus's leeway natural-key
// vocabulary: the memberships that tag a competence or a relation row in
// `boxer.facts` (ADR-0168 §SD1, §SD2, §SD6).
//
// The names say *competence*, not *capability*: boxer gives "capability" to
// the runtime's security capabilities (ADR-0026), so the corpus takes its own
// word (§SD6). The vault keeps the industry's; capmapcorpus is where the two
// meet.
//
// Each constant below is a registered membership whose uint64 id — via
// GetId().Value() — is what the generated DML builders'
// AddMembership{LowCardRef,MixedLowCardRef} methods take. The names here are
// the registered natural keys; leeway naming requires single stylable tokens,
// so they are camelCase identifiers that normalise to lower-spinal-case rather
// than dotted paths.
//
// It mirrors [github.com/stergiotis/boxer/public/keelson/runtime/vocab], which
// does the same job for runtime facts in the same table. Two vocabularies
// share `boxer.facts`, so what keeps their rows apart is the tag value below.
//
// # The claimed tag value
//
// This package claims **tag value 2178311**, the third of the width-32 class
// (ADR-0183 D0). ADR-0168 §SD6 allocated 16 for it; the re-key moved every
// vocabulary into one class, and the claim is now made through
// [github.com/stergiotis/boxer/public/identity/tagmint] rather than written
// down and hoped for.
//
// The old regime allocated by *base* — a vocabulary took the next unused
// multiple of 16 and owned the even offsets above it — because the tag-value
// registry could mint `base + tv` as a vocabulary grew, and a bare "next free
// integer" would land inside someone's growth path. A width-32 tag carries
// about 4.3e9 ids under one value, so the growth path is inside the tag rather
// than beside it, and one claimed value is the whole allocation.
//
// The claim is checked rather than trusted: tagmint refuses a value another
// package already claimed, and the committed assignment tables are compared
// across the repo (ADR-0183 D1). A collision would not be a compile error —
// it would be two different facts wearing the same membership id.
package capmapvocab

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Contract is this vocabulary's leeway contract — the vcs-managed convention,
// matching the runtime's. It requires the tag value handed to Begin be even;
// the base is what places the result.
var Contract = contract.NewVcsManagedContract()

// NamingStyle is the canonical form for capmap membership names. It matches
// the runtime vocabulary's, so a query joining competence facts to runtime
// facts reads the same way on both sides.
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, claimed from the width-32
// class every version-controlled vocabulary claims from (ADR-0183 D0). The
// third of the class.
//
// It was 16, a hand-picked base with room reserved below it for further capmap
// categories. The width-32 body holds about 4.3e9 ids, so one claimed value is
// room enough and the reservation is no longer needed.
var TagValueClaim = tagmint.MustClaim("capmap", 2178311, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
const MaxExpectedMemberships = 1 << 16

// NkRegistry is the natural-key registry for capmap memberships. Every Memb*
// constant below lives in it.
var NkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	TagValueClaim, 32, NamingStyle, Contract,
)

// Membership constants for `boxer.facts` rows carrying competence-corpus data.
//
// Each membership states its ordinal, which is the body of its id, and rows
// already written carry it ([registry.HumanReadableNaturalKeyRegistry.Begin]
// composes the two). A new membership takes the next unused number; the
// registry refuses a repeat, and where the declaration sits in this block is
// now a question of reading order only. It did not use to be — the tag
// membership below sits after the relation ones because appending to its own
// group would once have renumbered everything after it (ADR-0183 D0).
// Changing an ordinal already here is the breaking act.
// TestMembershipIdsAreGoldenPinned holds the whole table.
var (
	// Kinds. The row's attribute value carries the kind label for
	// readability; the membership id is what identifies which kind the row is.
	MembKindCompetence = NkRegistry.MustBegin("capmapKindCompetence", 0).End()
	MembKindRelation   = NkRegistry.MustBegin("capmapKindRelation", 1).End()

	// Competence identity and placement. Slug is the corpus's identity and is
	// also what the row's natural key is derived from; it is a column as well
	// because a query should not have to invert a digest.
	MembCompSlug      = NkRegistry.MustBegin("capmapCompetenceSlug", 2).End()
	MembCompName      = NkRegistry.MustBegin("capmapCompetenceName", 3).End()
	MembCompAbbrev    = NkRegistry.MustBegin("capmapCompetenceAbbrev", 4).End()
	MembCompSynopsis  = NkRegistry.MustBegin("capmapCompetenceSynopsis", 5).End()
	MembCompDomain    = NkRegistry.MustBegin("capmapCompetenceDomain", 6).End()
	MembCompCatalog   = NkRegistry.MustBegin("capmapCompetenceCatalog", 7).End()
	MembCompOwner     = NkRegistry.MustBegin("capmapCompetenceOwner", 8).End()
	MembCompLevel     = NkRegistry.MustBegin("capmapCompetenceLevel", 9).End()
	MembCompVaultPath = NkRegistry.MustBegin("capmapCompetenceVaultPath", 10).End()

	// Assessment. Both carry the not-assessed sentinel rather than being
	// omitted, so "unassessed" is a value a query can select on instead of an
	// absence it has to infer.
	MembCompMaturity = NkRegistry.MustBegin("capmapCompetenceMaturity", 11).End()
	MembCompPain     = NkRegistry.MustBegin("capmapCompetencePain", 12).End()

	// Body prose. Section is a mixed low-card-ref: the membership says "this
	// is a body section" and the high-card parameter carries the heading,
	// because headings are authored text and cannot be registered names. This
	// is the labelled-text shape ADR-0168 §SD5 adopts — the label rides the
	// membership, the prose stays prose.
	MembCompSection = NkRegistry.MustBegin("capmapCompetenceSection", 13).End()

	// Lifecycle. Both are mixed low-card-refs carrying the phase as their
	// high-card parameter, which is what keeps who and when attached to the
	// phase they belong to without eight pairs of registered names.
	MembCompLifecycleBy = NkRegistry.MustBegin("capmapCompetenceLifecycleBy", 14).End()
	MembCompLifecycleAt = NkRegistry.MustBegin("capmapCompetenceLifecycleAt", 15).End()

	// Relation endpoints, on the foreignKey section (ADR-0109 multi-membership
	// under distinct role memberships).
	//
	// Source always resolves — it is the competence the link was read from.
	// Target is present only when the link resolved to a competence in the
	// corpus, which is why the target's text is carried separately: a broken
	// link and a citation have a target worth recording and no fact to point
	// at.
	MembRelSource = NkRegistry.MustBegin("capmapRelationSource", 16).End()
	MembRelTarget = NkRegistry.MustBegin("capmapRelationTarget", 17).End()

	// Relation attributes. TargetText is the slug or citation key as written;
	// Resolution says whether it resolved, and how.
	MembRelTargetText = NkRegistry.MustBegin("capmapRelationTargetText", 18).End()
	MembRelKind       = NkRegistry.MustBegin("capmapRelationKind", 19).End()
	MembRelResolution = NkRegistry.MustBegin("capmapRelationResolution", 20).End()

	// Section provenance for a body wikilink — which heading the link sat
	// under, so "what does this competence's Standards section cite" is a
	// query rather than a scan.
	MembRelSection = NkRegistry.MustBegin("capmapRelationSection", 21).End()

	// Similarity score, on the f64 section, for similarity relations only.
	MembRelNcd = NkRegistry.MustBegin("capmapRelationNcd", 22).End()

	// MembCompTag is a competence's triage tag, one attribute per tag on the
	// symbol section — low-cardinality by nature, since tags are a small
	// vocabulary applied across the corpus. It is a competence attribute
	// declared after the relation ones because ids are ordinals and this
	// membership was added later; see the block comment.
	MembCompTag = NkRegistry.MustBegin("capmapCompetenceTag", 23).End()
)

// AllMembs enumerates every registered capmap membership. Tests iterate it to
// assert the invariants that matter — non-zero, unique, and disjoint from the
// other vocabularies sharing the table.
var AllMembs = []registry.RegisteredNaturalKey{
	MembKindCompetence, MembKindRelation,
	MembCompSlug, MembCompName, MembCompAbbrev, MembCompSynopsis,
	MembCompDomain, MembCompCatalog, MembCompOwner, MembCompLevel, MembCompVaultPath,
	MembCompMaturity, MembCompPain,
	MembCompSection,
	MembCompLifecycleBy, MembCompLifecycleAt,
	MembRelSource, MembRelTarget,
	MembRelTargetText, MembRelKind, MembRelResolution,
	MembRelSection,
	MembRelNcd,
	MembCompTag,
}
