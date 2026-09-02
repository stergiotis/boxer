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
// The vocabulary exists for one flow (its ADR records it): an editor hands a
// markdown document to the SQL playground by persisting it as a fact and
// opening play on a query that selects it back. Append-shaped by design —
// every send is a new row, the entity is the content itself, and nothing here
// is ever updated or deleted.
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
