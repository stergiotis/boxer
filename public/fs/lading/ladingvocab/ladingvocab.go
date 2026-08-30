// Package ladingvocab is the lading store's leeway natural-key vocabulary:
// the memberships that tag a file-tree row in the store's own facts-shaped
// tables, and the mount policy record in `boxer.facts` (ADR-0198 §SD2).
//
// It mirrors [github.com/stergiotis/boxer/public/keelson/runtime/sysmvocab],
// [github.com/stergiotis/boxer/public/gov/capmapvocab] and
// [github.com/stergiotis/boxer/public/keelson/runtime/vocab], which do the
// same job for metric samples, the competence corpus and runtime facts. What
// keeps them apart is the tag value below.
//
// The DTOs written against these names live beside it, under
// [github.com/stergiotis/boxer/public/fs/lading]; the ids reach the generated
// stores at generation time, through
// [github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen]'s
// MembershipIds snapshot.
//
// # Why `lading` and not `fs`
//
// A membership named `fsMode` beside a DTO field named `Mode` reads as
// something the standard library's `io/fs` defines. It does not: these names
// belong to one store's row shape, and the store is one of several things
// under `public/fs` that could be called an fs. `lading` is the store's own
// name — a bill of lading is issued once for one voyage, lists exactly what
// was loaded, and is never amended, only superseded by the next one, which is
// this store's contract for a snapshot verbatim.
//
// # Tag value 2178315, and why the number is written down
//
// This package claims **tag value 2178315**, the seventh of the width-32
// class (ADR-0183 D0), one above `meshdemo`'s. A width-32 tag holds about
// 4.3e9 ids, so one claimed value is a vocabulary's whole allocation and
// there is no growth path to reserve room beside.
//
// The claim goes through [github.com/stergiotis/boxer/public/identity/tagmint],
// which refuses a value another package already claimed, and the committed
// assignment tables are compared across the repo (ADR-0183 D1). A collision
// would not be a compile error — it would be two unrelated facts wearing the
// same membership id, and every query over either would be quietly wrong.
//
// # Four kinds, and which rows carry which
//
// An ordinary node of a snapshot is one `ladingEntry`. The root row
// (`naturalKey = '.'`) is a `ladingEntry` *and* a `ladingSnapshot`: it stats
// the tree's root like any other node, and it carries the totals and the
// policy actually applied, which is what makes it the commit record
// (ADR-0198 §SD6). A block of a file's content is one `ladingBlock`, on the
// store's other table. A mount's declared policy is one `ladingMount`, and it
// is the only one of the four that lives in `boxer.facts` — it is runtime
// state, not snapshot data (ADR-0198 §SD2).
package ladingvocab

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Contract is this vocabulary's leeway contract — the vcs-managed convention,
// matching every other version-controlled vocabulary in the tree. It requires
// a tag value from the vocabulary width class and each membership's ordinal
// declared in source.
var Contract = contract.NewVcsManagedContract()

// NamingStyle is the canonical form for snapshot-store membership names. It
// matches the vocabularies sharing `boxer.facts`, so a query joining a mount
// policy to runtime facts reads the same way on both sides.
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, claimed from the width-32
// class every version-controlled vocabulary claims from (ADR-0183 D0). The
// seventh of the class: 2178309 keelson vdd, …310 keelson runtime, …311
// capmap, …312 sysmetrics, …313 jsonbench, …314 meshdemo, …315 here.
//
// This comment is the class's only registry, so the next claim reads it to
// find a free number. **…316 through …334 are taken** — nineteen consecutive
// values held by a consuming repository, which crossed to this scheme on
// 2026-08-20 and could not keep its previous values: they were short-prefix
// tags this contract reserves for the runtime generators.
//
// **…335 is taken** by shadow-boxer's `photofacts` vocabulary, claimed
// 2026-08-30 as `shadowboxerPhoto`. It crossed for the same reason and at the
// same cost: its hand-picked base 32 encodes to a width-8 tag, so every
// membership id moved and the facts already written under the old base had to
// be re-crawled. Two repositories have now paid that, which is the argument
// for a consumer claiming here before it writes rows rather than after.
//
// The next free value is 2178336.
var TagValueClaim = tagmint.MustClaim("lading", 2178315, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
// The width-32 class holds about 4.3e9, so the number is headroom rather than
// a quota; it is stated so a future claim from a narrower class is refused
// rather than silently truncated.
const MaxExpectedMemberships = 1 << 16

// NkRegistry is the natural-key registry for snapshot-store memberships.
// Every Memb* constant below lives in it. The size is a capacity hint only —
// an id is what its registration declares.
var NkRegistry = registry.MustNewNaturalKeyRegistry(
	TagValueClaim, 64, NamingStyle, Contract,
)

// Membership constants for the fs snapshot store.
//
// Each states its ordinal: the number beside the name is the id's body, and
// rows already written carry it. A new membership takes the next unused
// ordinal and may be declared anywhere — the registry refuses a repeat, and
// nothing moves because of where it sits (ADR-0183 D0). Changing an ordinal
// already here is the breaking act, and the assignment golden is what makes
// it visible in review.
var (
	// The four kind markers. A row's attribute value carries the kind label
	// for readability; the membership id is what identifies which kind the
	// row is. A generated store's Scan<Kind> filters on a kind's own
	// memberships and does not need these — they exist so hand-written SQL
	// can select one kind without enumerating its attributes, which is what
	// the fs() macro expansion and the fssnap materialised view do.
	MembKindEntry    = NkRegistry.MustBegin("ladingKindEntry", 0).End()
	MembKindSnapshot = NkRegistry.MustBegin("ladingKindSnapshot", 1).End()
	MembKindBlock    = NkRegistry.MustBegin("ladingKindBlock", 2).End()
	MembKindMount    = NkRegistry.MustBegin("ladingKindMount", 3).End()

	// One node of one snapshot. The backbone already carries which mount,
	// which snapshot and which path (ADR-0198 §SD3), so nothing here repeats
	// them.

	// NodeKind is the node's own kind — `file`, `dir`, `symlink` or `other`
	// — and is stored rather than derived because it is a LowCardinality
	// symbol lane a query groups by directly, where Mode is a leeway
	// attribute a reader has to extract first. The tree columns `is_dir` /
	// `is_symlink` are the indexed spelling of the same fact, materialised
	// over Mode. Spelled NodeKind, not Kind, so nothing reads it as the kind
	// marker beside it.
	MembNodeKind = NkRegistry.MustBegin("ladingNodeKind", 4).End()
	// Content is what the store holds for this node: `none` (stat only),
	// `blocks` (in the block table) or `ref` (fetched from the live source).
	MembContent = NkRegistry.MustBegin("ladingContent", 5).End()
	// Mode is the fs.FileMode bits as the walk read them, including the type
	// bits — so a reader that wants Lstat semantics has them without a second
	// column.
	MembMode = NkRegistry.MustBegin("ladingMode", 6).End()
	MembSize = NkRegistry.MustBegin("ladingSize", 7).End()
	// Mtime is the source's modification time, not the walk's. Sources differ
	// in resolution — SFTP carries whole seconds — and the store does not
	// round or fill: what the walk read is what is stored.
	MembMtime = NkRegistry.MustBegin("ladingMtime", 8).End()
	// LinkTarget is a symlink's target verbatim, unresolved: resolving it
	// would need the source's mount table, which a snapshot does not have.
	MembLinkTarget = NkRegistry.MustBegin("ladingLinkTarget", 9).End()
	// ContentHash is the BLAKE3 digest of the whole file (ADR-0198 §SD5;
	// CS009 bans crypto/sha256). Absent where Content is `none`.
	MembContentHash = NkRegistry.MustBegin("ladingContentHash", 10).End()
	// BlockSize and Blocks describe how the content was cut, so a reader can
	// address a byte range without first scanning the block table.
	MembBlockSize = NkRegistry.MustBegin("ladingBlockSize", 11).End()
	MembBlocks    = NkRegistry.MustBegin("ladingBlocks", 12).End()
	// Text records that the file was cut at newlines rather than at a fixed
	// offset, which is what makes a line-oriented query over its blocks
	// boundary-safe.
	MembText = NkRegistry.MustBegin("ladingText", 13).End()
	// Err is the walk's error for this node, stored rather than raised: a
	// tree with an unreadable directory still snapshots, and the failure is
	// queryable instead of lost (ADR-0198 §SD6 — the walk continues).
	MembErr = NkRegistry.MustBegin("ladingErr", 14).End()

	// The root row's second component: the snapshot's totals and the policy
	// actually applied. Its presence is what makes a snapshot complete, so
	// these are never written before the rest of the walk is durable.
	MembSnapEntries = NkRegistry.MustBegin("ladingSnapEntries", 15).End()
	MembSnapBytes   = NkRegistry.MustBegin("ladingSnapBytes", 16).End()
	// The applied policy, recorded beside the totals rather than only in the
	// mount's policy record, because the policy record is mutable runtime
	// state and a snapshot must stay interpretable after it changes.
	MembTtlClass  = NkRegistry.MustBegin("ladingTtlClass", 17).End()
	MembTextRule  = NkRegistry.MustBegin("ladingTextRule", 18).End()
	MembInlineMax = NkRegistry.MustBegin("ladingInlineMax", 19).End()

	// One block of one file's content, on the store's block table.
	MembData = NkRegistry.MustBegin("ladingData", 20).End()
	// BlockHash is this block's own BLAKE3 digest, present in the corpus
	// profile only. It is what `BLAKE3(data) != hash` audits in SQL.
	MembBlockHash = NkRegistry.MustBegin("ladingBlockHash", 21).End()
	// Line0 is the 1-based line number of the block's first line, for text
	// blocks. It is what lets a grep-shaped query report a line number
	// without reading every earlier block.
	MembLine0 = NkRegistry.MustBegin("ladingLine0", 22).End()

	// A mount's declared policy — the only kind of the four that lives in
	// `boxer.facts`. Its memberships are deliberately not shared with the
	// applied-policy ones above: the two say different things (declared now
	// versus applied then), and keeping them apart also keeps the two kinds
	// generatable into one package should that ever be wanted.
	MembMountName  = NkRegistry.MustBegin("ladingMountName", 23).End()
	MembMountStore = NkRegistry.MustBegin("ladingMountStore", 24).End()
	// The mount's retention class — a class, not a free duration, and whole
	// days only: an expiry within a day would leave a partition partially
	// expired, which `ttl_only_drop_parts = 1` never clears on a background
	// merge (measured, ADR-0198 `## Updates` 2026-08-19).
	MembMountTtlClass = NkRegistry.MustBegin("ladingMountTtlClass", 25).End()
	// The text-classification rule and the inline threshold, which together
	// decide how a file's content is cut and whether it is stored at all.
	MembMountTextRule  = NkRegistry.MustBegin("ladingMountTextRule", 26).End()
	MembMountInlineMax = NkRegistry.MustBegin("ladingMountInlineMax", 27).End()
)

// AllMembs is every membership this vocabulary registers, in ordinal order.
// The disjointness and round-trip guards iterate it, so a membership left out
// here is a membership nothing checks.
var AllMembs = []registry.RegisteredNaturalKey{
	MembKindEntry, MembKindSnapshot, MembKindBlock, MembKindMount,

	MembNodeKind, MembContent, MembMode, MembSize, MembMtime, MembLinkTarget,
	MembContentHash, MembBlockSize, MembBlocks, MembText, MembErr,

	MembSnapEntries, MembSnapBytes, MembTtlClass, MembTextRule, MembInlineMax,

	MembData, MembBlockHash, MembLine0,

	MembMountName, MembMountStore, MembMountTtlClass, MembMountTextRule,
	MembMountInlineMax,
}
