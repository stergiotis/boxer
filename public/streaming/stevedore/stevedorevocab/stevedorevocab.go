// Package stevedorevocab is the stevedore leeway natural-key vocabulary: the
// memberships of the item envelope that crosses a pipeline (ADR-0252 §SD2)
// and of the dead-letter row a lander writes (ADR-0252 §SD3).
//
// It mirrors [github.com/stergiotis/boxer/public/semistructured/markdown/mddocvocab];
// what keeps the vocabularies apart is the tag value below. The envelope kind
// lives in [github.com/stergiotis/boxer/public/streaming/stevedore/stevedoreitem]
// with a generated wire codec, the dead-letter kind in
// [github.com/stergiotis/boxer/public/streaming/stevedore/stevedorefacts] with
// a generated facts-bound store. Variable names follow the codec generator's
// convention, Memb<Name> for membership <name>, so both generators resolve the
// same ids from this file.
package stevedorevocab

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Contract is this vocabulary's leeway contract — the vcs-managed convention,
// matching every other vocabulary sharing the facts table.
var Contract = contract.NewVcsManagedContract()

// NamingStyle matches the peer vocabularies.
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, claimed from the width-32
// class every version-controlled vocabulary claims from (ADR-0183 D0). The
// mint refuses a value another package already claimed, and the committed
// assignment table beside this package is what makes a re-pointed id visible
// in review. The same tag also mints file references (ADR-0252 §SD4): a
// reference is a tagged id whose body hashes a request's origin.
var TagValueClaim = tagmint.MustClaim("stevedore", 2178317, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
const MaxExpectedMemberships = 1 << 16

// NkRegistry is the natural-key registry for stevedore memberships. The size
// is a capacity hint only.
var NkRegistry = registry.MustNewNaturalKeyRegistry(
	TagValueClaim, 32, NamingStyle, Contract,
)

// The item envelope (ADR-0252 §SD2): what one message of a fan-out carries
// beside the application's payload, so a lander can route it and a redelivery
// repeats it. Each states its ordinal: the number beside the name is the id's
// body, and rows already written carry it (ADR-0183 D0).
var (
	// MembStevedoreKindItem is the kind label of an envelope.
	MembStevedoreKindItem = NkRegistry.MustBegin("stevedoreKindItem", 0).End()
	// MembStevedoreRef is the file reference: a tagged id derived from the
	// request's origin, identical on every redelivery.
	MembStevedoreRef = NkRegistry.MustBegin("stevedoreRef", 1).End()
	// MembStevedoreOrigin is the origin string the request carried, verbatim,
	// so a reader can find the request without the mint.
	MembStevedoreOrigin = NkRegistry.MustBegin("stevedoreOrigin", 2).End()
	// MembStevedoreOrdinal is the item's position among the items one request
	// produced, from zero.
	MembStevedoreOrdinal = NkRegistry.MustBegin("stevedoreOrdinal", 3).End()
	// MembStevedoreLine is the first source line the item came from, when the
	// handler knows one; zero otherwise.
	MembStevedoreLine = NkRegistry.MustBegin("stevedoreLine", 4).End()
	// MembStevedoreOffset is the byte offset the item came from, when the
	// handler knows one; zero otherwise.
	MembStevedoreOffset = NkRegistry.MustBegin("stevedoreOffset", 5).End()
	// MembStevedoreSplit marks an item as one part of a body the pipeline
	// split into messages, for reassembly (ADR-0252 §SD4); a first part is
	// otherwise indistinguishable from an unsplit item.
	MembStevedoreSplit = NkRegistry.MustBegin("stevedoreSplit", 11).End()
	// MembStevedorePart is the part's index, from zero, of a split item.
	MembStevedorePart = NkRegistry.MustBegin("stevedorePart", 6).End()
	// MembStevedoreParts is the part count, carried on the last part; zero
	// until it is known.
	MembStevedoreParts = NkRegistry.MustBegin("stevedoreParts", 7).End()
	// MembStevedoreLast marks the last part of a split body.
	MembStevedoreLast = NkRegistry.MustBegin("stevedoreLast", 8).End()
	// MembStevedorePayloadKind is the vocabulary kind name the payload's bytes
	// claim, or "" for bytes that are not a kind.
	MembStevedorePayloadKind = NkRegistry.MustBegin("stevedorePayloadKind", 9).End()
	// MembStevedorePayload is the application's payload, verbatim.
	MembStevedorePayload = NkRegistry.MustBegin("stevedorePayload", 10).End()
)

// The dead-letter row (ADR-0252 §SD3): what a lander gave up on, and why.
var (
	// MembStevedoreKindDeadLetter is the kind label of a dead-letter row.
	MembStevedoreKindDeadLetter = NkRegistry.MustBegin("stevedoreKindDeadLetter", 32).End()
	// MembStevedoreDeadRef is the envelope's reference, when one decoded.
	MembStevedoreDeadRef = NkRegistry.MustBegin("stevedoreDeadRef", 33).End()
	// MembStevedoreDeadOrigin is the envelope's origin, when one decoded.
	MembStevedoreDeadOrigin = NkRegistry.MustBegin("stevedoreDeadOrigin", 34).End()
	// MembStevedoreDeadOrdinal is the envelope's ordinal, when one decoded.
	MembStevedoreDeadOrdinal = NkRegistry.MustBegin("stevedoreDeadOrdinal", 35).End()
	// MembStevedoreDeadClass is the failure class: permanent, or incomplete
	// for a split body whose last part never arrived.
	MembStevedoreDeadClass = NkRegistry.MustBegin("stevedoreDeadClass", 36).End()
	// MembStevedoreDeadError is the error text.
	MembStevedoreDeadError = NkRegistry.MustBegin("stevedoreDeadError", 37).End()
	// MembStevedoreDeadTopic, MembStevedoreDeadPartition and
	// MembStevedoreDeadOffset locate the message on its topic.
	MembStevedoreDeadTopic     = NkRegistry.MustBegin("stevedoreDeadTopic", 38).End()
	MembStevedoreDeadPartition = NkRegistry.MustBegin("stevedoreDeadPartition", 39).End()
	MembStevedoreDeadOffset    = NkRegistry.MustBegin("stevedoreDeadOffset", 40).End()
	// MembStevedoreDeadMessage is the message's bytes, verbatim, so the
	// failure can be replayed.
	MembStevedoreDeadMessage = NkRegistry.MustBegin("stevedoreDeadMessage", 41).End()
)

// AllMembs lists every membership, in ordinal order, for the tests that
// check the registry against this file.
var AllMembs = []registry.RegisteredNaturalKey{
	MembStevedoreKindItem, MembStevedoreRef, MembStevedoreOrigin, MembStevedoreOrdinal,
	MembStevedoreLine, MembStevedoreOffset, MembStevedorePart, MembStevedoreParts,
	MembStevedoreLast, MembStevedorePayloadKind, MembStevedorePayload,
	MembStevedoreSplit, MembStevedoreKindDeadLetter, MembStevedoreDeadRef, MembStevedoreDeadOrigin,
	MembStevedoreDeadOrdinal, MembStevedoreDeadClass, MembStevedoreDeadError,
	MembStevedoreDeadTopic, MembStevedoreDeadPartition, MembStevedoreDeadOffset,
	MembStevedoreDeadMessage,
}
