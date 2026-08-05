// Package capmapvocab is the capability corpus's leeway natural-key
// vocabulary: the memberships that tag a capability or a relation row in
// `boxer.facts` (ADR-0168 §SD1, §SD2, §SD6).
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
// # Tag value 16, and why the number is written down
//
// This package owns **tag value 16**, allocated by ADR-0168 §SD6.
//
// Nothing in the tree records which tag values are taken — ADR-0106 §SD8 notes
// only that zero is reserved — so §SD6 makes the allocating ADR the register
// and requires the allocation be named here, where the next author will be
// standing when they need one.
//
// Allocation is by *base*, not by number, because a base reserves an
// open-ended range: [registry.MembershipTagValueRegistry.Begin] mints
// `base + tv` for any even `tv`, so a registry based at 2 can mint 4, 6, 8 and
// upward whenever it grows. Taking merely "the next free integer" would put a
// new vocabulary inside an existing one's growth path.
//
// The rule, therefore: **a new vocabulary takes the next unused multiple of
// 16, and owns the even offsets up to the following multiple** — 16 here means
// 16, 18 … 30, which is more headroom than either existing registry's declared
// size. Two bases predate the rule and are grandfathered at their current
// size: 1 (keelson vdd, `valueLabel`) and 2 (keelson runtime, `runtimeMembers`).
// A vocabulary added after this one takes 32.
//
// TestTagValuesAreDisjointFromOtherVocabularies is what enforces it; a
// collision would not be a compile error, it would be two different facts
// wearing the same membership id.
package capmapvocab

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Contract is this vocabulary's leeway contract — the vcs-managed convention,
// matching the runtime's. It requires the tag value handed to Begin be even;
// the base is what places the result.
var Contract = contract.NewVcsManagedContract()

// NamingStyle is the canonical form for capmap membership names. It matches
// the runtime vocabulary's, so a query joining capability facts to runtime
// facts reads the same way on both sides.
const NamingStyle = naming.LowerSpinalCase

// TagValueBase is this vocabulary's reserved base — see the package comment
// for why allocation is by base rather than by single value.
const TagValueBase = 16

// TagValueRegistry allocates tag values for capmap membership categories. It
// lives in its own scope so it cannot collide with the keelson or runtime
// registries, which are different vocabularies in the same table.
var TagValueRegistry = registry.MustNewTagValueRegistry[*contract.VcsManagedContract](
	identifier.TagValue(TagValueBase), NamingStyle, 4, Contract,
)

// MembersTagValue is the tag value rooted at offset 0 of [TagValueRegistry],
// covering every membership registered below.
var MembersTagValue = TagValueRegistry.MustBegin("capmapMembers", 0).End()

// NkRegistry is the natural-key registry for capmap memberships. Every Memb*
// constant below lives in it.
var NkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	MembersTagValue.GetTagValue(), 32, NamingStyle, identifier.UntaggedId(0), Contract,
)

// Membership constants for `boxer.facts` rows carrying capability-corpus data.
//
// Ordering matters and is append-only: membership ids follow declaration
// order, and rows already written carry them. A new membership goes at the end
// of its group, never in the middle.
var (
	// Kinds. The row's attribute value carries the kind label for
	// readability; the membership id is what identifies which kind the row is.
	MembKindCapability = NkRegistry.MustBegin("capmapKindCapability").End()
	MembKindRelation   = NkRegistry.MustBegin("capmapKindRelation").End()

	// Capability identity and placement. Slug is the corpus's identity and is
	// also what the row's natural key is derived from; it is a column as well
	// because a query should not have to invert a digest.
	MembCapSlug      = NkRegistry.MustBegin("capmapCapabilitySlug").End()
	MembCapName      = NkRegistry.MustBegin("capmapCapabilityName").End()
	MembCapAbbrev    = NkRegistry.MustBegin("capmapCapabilityAbbrev").End()
	MembCapSynopsis  = NkRegistry.MustBegin("capmapCapabilitySynopsis").End()
	MembCapDomain    = NkRegistry.MustBegin("capmapCapabilityDomain").End()
	MembCapCatalog   = NkRegistry.MustBegin("capmapCapabilityCatalog").End()
	MembCapOwner     = NkRegistry.MustBegin("capmapCapabilityOwner").End()
	MembCapLevel     = NkRegistry.MustBegin("capmapCapabilityLevel").End()
	MembCapVaultPath = NkRegistry.MustBegin("capmapCapabilityVaultPath").End()

	// Assessment. Both carry the not-assessed sentinel rather than being
	// omitted, so "unassessed" is a value a query can select on instead of an
	// absence it has to infer.
	MembCapMaturity = NkRegistry.MustBegin("capmapCapabilityMaturity").End()
	MembCapPain     = NkRegistry.MustBegin("capmapCapabilityPain").End()

	// Body prose. Section is a mixed low-card-ref: the membership says "this
	// is a body section" and the high-card parameter carries the heading,
	// because headings are authored text and cannot be registered names. This
	// is the labelled-text shape ADR-0168 §SD5 adopts — the label rides the
	// membership, the prose stays prose.
	MembCapSection = NkRegistry.MustBegin("capmapCapabilitySection").End()

	// Lifecycle. Both are mixed low-card-refs carrying the phase as their
	// high-card parameter, which is what keeps who and when attached to the
	// phase they belong to without eight pairs of registered names.
	MembCapLifecycleBy = NkRegistry.MustBegin("capmapCapabilityLifecycleBy").End()
	MembCapLifecycleAt = NkRegistry.MustBegin("capmapCapabilityLifecycleAt").End()

	// Relation endpoints, on the foreignKey section (ADR-0109 multi-membership
	// under distinct role memberships).
	//
	// Source always resolves — it is the capability the link was read from.
	// Target is present only when the link resolved to a capability in the
	// corpus, which is why the target's text is carried separately: a broken
	// link and a citation have a target worth recording and no fact to point
	// at.
	MembRelSource = NkRegistry.MustBegin("capmapRelationSource").End()
	MembRelTarget = NkRegistry.MustBegin("capmapRelationTarget").End()

	// Relation attributes. TargetText is the slug or citation key as written;
	// Resolution says whether it resolved, and how.
	MembRelTargetText = NkRegistry.MustBegin("capmapRelationTargetText").End()
	MembRelKind       = NkRegistry.MustBegin("capmapRelationKind").End()
	MembRelResolution = NkRegistry.MustBegin("capmapRelationResolution").End()

	// Section provenance for a body wikilink — which heading the link sat
	// under, so "what does this capability's Standards section cite" is a
	// query rather than a scan.
	MembRelSection = NkRegistry.MustBegin("capmapRelationSection").End()

	// Similarity score, on the f64 section, for similarity relations only.
	MembRelNcd = NkRegistry.MustBegin("capmapRelationNcd").End()
)

// AllMembs enumerates every registered capmap membership. Tests iterate it to
// assert the invariants that matter — non-zero, unique, and disjoint from the
// other vocabularies sharing the table.
var AllMembs = []registry.RegisteredNaturalKey{
	MembKindCapability, MembKindRelation,
	MembCapSlug, MembCapName, MembCapAbbrev, MembCapSynopsis,
	MembCapDomain, MembCapCatalog, MembCapOwner, MembCapLevel, MembCapVaultPath,
	MembCapMaturity, MembCapPain,
	MembCapSection,
	MembCapLifecycleBy, MembCapLifecycleAt,
	MembRelSource, MembRelTarget,
	MembRelTargetText, MembRelKind, MembRelResolution,
	MembRelSection,
	MembRelNcd,
}
