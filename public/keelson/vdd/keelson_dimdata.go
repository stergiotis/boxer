package vdd

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/naturalkey"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

var KeelsonContract = contract.NewVcsManagedContract()

const NaturalKeyFormat = naturalkey.SerializationFormatJson
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, claimed from the width-32
// class every version-controlled vocabulary claims from — the first of the
// class (ADR-0183 D0).
//
// It was 1 before the re-key: the shortest fibonacci code in the scheme, whose
// 62-bit body is its widest. Spending that on ~140 membership names while the
// high-cardinality runtime id generators competed for the remaining short
// prefixes was the allocation exactly inverted.
var TagValueClaim = tagmint.MustClaim("keelsonVdd", 2178309, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
// The width-32 class holds about 4.3e9, so the number is headroom rather than
// a quota; it is stated so a future claim from a narrower class is refused
// rather than silently too small.
const MaxExpectedMemberships = 1 << 20

// KeelsonHrNkRegistry pre-sizes for ~130 in-tree memberships (12 dimdata
// files; lw alone declares 55) with comfortable headroom for the next
// migration wave. Bumped from 64 → 256 in ADR-0042's post-Phase-C
// follow-up.
var KeelsonHrNkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	TagValueClaim, 256, NamingStyle, KeelsonContract)

var (
	MembParent     = KeelsonHrNkRegistry.MustBegin("parent", 0).End()
	MembChild      = KeelsonHrNkRegistry.MustBegin("child", 1).End()
	MembNaturalKey = KeelsonHrNkRegistry.MustBegin("naturalKey", 2).End()
)
