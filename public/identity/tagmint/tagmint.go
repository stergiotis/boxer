// Package tagmint is the authority for identifier tag values: the one place a
// tag value is claimed, and the only source of the token the registries that
// mint tagged ids demand.
//
// The name says what happens here. A *tag* is the fibonacci-coded prefix of a
// [identifier.TaggedId] (ADR-0106); a *mint* is where a value is struck. It
// pairs with `leeway/namemint` one level up: a vocabulary package claims its
// tag value here, then registers names into a namemint registry, which
// composes the two into ids. Reading the import graph tells that story.
//
// # Why claiming rather than constructing
//
// A tag value used to be a number a vocabulary package picked and passed to a
// registry constructor. Nothing checked the picks against each other, so two
// vocabularies sharing one table could — and did — pick the same value
// (ADR-0183's Context: an app vocabulary duplicated the runtime's, latent only
// because they share no table). Ids from two vocabularies are disjoint exactly
// when their tag values differ, so that collision is an id collision waiting
// for a shared table.
//
// Here the value stays declared in the consumer, as one literal at the claim
// site, and this package checks it: the name and the value must both be
// unclaimed, and the value's width class must hold the ids the family says it
// will have. The result is a [ClaimedTagValue] that cannot be constructed
// anywhere else, so a registry that demands one cannot be built around the
// check.
//
// Claims are per link set, like every init-populated registry. That is enough
// to catch a collision in any binary that links both claimants, and not enough
// to prove global disjointness — the committed assignment goldens
// (ADR-0183 D1) are what make it total.
package tagmint

import (
	"iter"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// VocabularyTagWidth is the fibonacci code width every version-controlled
// vocabulary claims from — a uniform 32/32 split of the 64-bit id: about
// 4.3·10⁹ ids per vocabulary, and 1,346,269 claimable tag values in the class.
//
// The split being one constant is worth more than the last bit of packing: it
// is what lets `identsql` views and [identifier.IdTag.SameTag] mask compares
// speak of one width rather than one per vocabulary. Vocabularies used to sit
// on the scheme's shortest prefixes — the widest bodies — which spent the
// scarce short codes on its smallest families while the high-cardinality
// runtime generators competed for what was left (ADR-0183 D0).
const VocabularyTagWidth = 32

// RuntimeMintTagValue is reserved for ids minted outside version control:
// memberships coined at runtime, and names carried by data passing through.
// It is the maximum of the [VocabularyTagWidth] class, so it reads as the far
// end of the same space the vocabularies claim from rather than as a number
// from nowhere, and it is claimed here at init like any other value — a second
// claimant is refused by uniqueness, not by convention.
//
// A version-controlled vocabulary may not use it: `namemint`'s VCS-managed
// contract refuses it against the effective tag. Who allocates bodies under it
// is a separate decision, deliberately not made here (ADR-0183 D8).
const RuntimeMintTagValue identifier.TagValue = 3524577

// ClaimedTagValue is proof that a tag value passed through [Claim]. It carries
// a reference to the claim this package holds, so the zero value — the only
// one another package can produce — reports false from [ClaimedTagValue.IsValid]
// and is refused by every constructor that asks for a claim.
type ClaimedTagValue struct {
	c *claim
}

type claim struct {
	name           string
	value          identifier.TagValue
	maxExpectedIds uint64
	origin         string
}

var (
	mu        sync.Mutex
	byName    = map[string]*claim{}
	byValue   = map[identifier.TagValue]*claim{}
	claimList []*claim
)

// Claim registers name against value and returns the token the id registries
// require.
//
// name identifies the claimant in errors and in the audit views; it is a
// vocabulary's name, not a membership's. maxExpectedIds is what the family
// says it will need — the check is that the claimed value's width class can
// hold that many bodies, so a family outgrowing its class is a refusal at init
// rather than an overflow panic much later.
//
// Both the name and the value must be unclaimed in this link set. The error
// names the code location that claimed first, because the useful question when
// two packages collide is which one to move.
func Claim(name string, value identifier.TagValue, maxExpectedIds uint64) (r ClaimedTagValue, err error) {
	if name == "" {
		err = eb.Build().Errorf("a tag-value claim needs a name")
		return
	}
	if !value.IsValid() {
		err = eb.Build().Str("name", name).Errorf("tag value 0 is the invalid sentinel and cannot be claimed")
		return
	}
	if maxExpectedIds == 0 {
		err = eb.Build().Str("name", name).Errorf("a claim states how many ids the family will hold; 0 says it holds none")
		return
	}
	tag := value.GetTag()
	if !tag.IsValid() {
		err = eb.Build().Str("name", name).Uint32("tagValue", value.Value()).Errorf("tag value does not encode to a valid tag")
		return
	}
	cl, cerr := fibonacci.WidthClassOf(tag.GetTagWidth())
	if cerr != nil {
		err = eb.Build().Str("name", name).Uint32("tagValue", value.Value()).Errorf("tag value lies outside the addressable width classes: %w", cerr)
		return
	}
	if cl.MaxBodyIncl < maxExpectedIds {
		err = eb.Build().Str("name", name).Uint32("tagValue", value.Value()).Int("width", cl.Width).Uint64("holds", cl.MaxBodyIncl).Uint64("declared", maxExpectedIds).Errorf("the tag value's width class holds fewer ids than declared — claim a value from a narrower class")
		return
	}

	mu.Lock()
	defer mu.Unlock()
	if prev, has := byName[name]; has {
		err = eb.Build().Str("name", name).Str("claimedAt", prev.origin).Errorf("tag-value name is already claimed")
		return
	}
	if prev, has := byValue[value]; has {
		err = eb.Build().Str("name", name).Uint32("tagValue", value.Value()).Str("claimant", prev.name).Str("claimedAt", prev.origin).Errorf("the tag value is already claimed — ids under one tag value are one id space, so two claimants collide")
		return
	}
	c := &claim{
		name:           name,
		value:          value,
		maxExpectedIds: maxExpectedIds,
		origin:         origin(),
	}
	byName[name] = c
	byValue[value] = c
	claimList = append(claimList, c)
	r = ClaimedTagValue{c: c}
	return
}

// MustClaim is [Claim] for a package-level declaration, where a refusal is a
// build-time fact wearing a run-time coat: there is no recovery, and the panic
// names the collision.
func MustClaim(name string, value identifier.TagValue, maxExpectedIds uint64) (r ClaimedTagValue) {
	var err error
	r, err = Claim(name, value, maxExpectedIds)
	if err != nil {
		log.Panic().Err(err).Str("name", name).Uint32("tagValue", value.Value()).Msg("unable to claim tag value")
	}
	return
}

// IsValid reports whether inst came from [Claim]. The zero ClaimedTagValue —
// what a `ClaimedTagValue{}` literal outside this package produces — is not
// valid, which is what makes the token unforgeable in practice.
func (inst ClaimedTagValue) IsValid() bool {
	return inst.c != nil
}

// Value returns the claimed tag value, or the invalid 0 for an unclaimed token.
func (inst ClaimedTagValue) Value() identifier.TagValue {
	if inst.c == nil {
		return 0
	}
	return inst.c.value
}

// Tag returns the claimed value's fibonacci-coded tag, or the invalid 0 tag for
// an unclaimed token.
func (inst ClaimedTagValue) Tag() identifier.IdTag {
	return inst.Value().GetTag()
}

// Name returns the claimant's name, or "" for an unclaimed token.
func (inst ClaimedTagValue) Name() string {
	if inst.c == nil {
		return ""
	}
	return inst.c.name
}

// MaxExpectedIds returns the cardinality the claimant declared, or 0 for an
// unclaimed token.
func (inst ClaimedTagValue) MaxExpectedIds() uint64 {
	if inst.c == nil {
		return 0
	}
	return inst.c.maxExpectedIds
}

// Origin returns the source location that made the claim, or "" for an
// unclaimed token.
func (inst ClaimedTagValue) Origin() string {
	if inst.c == nil {
		return ""
	}
	return inst.c.origin
}

// IterateClaims yields every claim in this link set, in claim order. It is
// what an audit — the assignment goldens' union test, a claims-as-facts
// publication (ADR-0183 D3) — reads.
func IterateClaims() iter.Seq[ClaimedTagValue] {
	return func(yield func(ClaimedTagValue) bool) {
		mu.Lock()
		snapshot := make([]*claim, len(claimList))
		copy(snapshot, claimList)
		mu.Unlock()
		for _, c := range snapshot {
			if !yield(ClaimedTagValue{c: c}) {
				return
			}
		}
	}
}

// Lookup returns the claim registered under name.
func Lookup(name string) (r ClaimedTagValue, has bool) {
	mu.Lock()
	defer mu.Unlock()
	c, has := byName[name]
	if !has {
		return
	}
	return ClaimedTagValue{c: c}, true
}

// RuntimeMint is the claim on [RuntimeMintTagValue], made here so the
// reservation is a claim like any other: a vocabulary that picks the same
// value is refused by uniqueness at init, with this claim's origin named.
var RuntimeMint = MustClaim("runtimeMint", RuntimeMintTagValue, 1)

// origin reports the first caller outside this package, so a claim points at
// its declaration rather than at MustClaim.
//
// The frame is skipped by function name rather than by file path: this
// package's own tests sit in the same directory, and a path filter would walk
// straight past them into the testing runtime.
const selfPrefix = "github.com/stergiotis/boxer/public/identity/tagmint."

func origin() string {
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:]) // skip runtime.Callers + origin
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		if frame.Function != "" && !strings.HasPrefix(frame.Function, selfPrefix) {
			return frame.File + ":" + strconv.Itoa(frame.Line)
		}
		if !more {
			break
		}
	}
	return "unknown"
}
