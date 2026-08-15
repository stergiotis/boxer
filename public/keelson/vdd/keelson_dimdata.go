package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/naturalkey"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

var KeelsonContract = contract.NewVcsManagedContract()

const NaturalKeyFormat = naturalkey.SerializationFormatJson
const NamingStyle = naming.LowerSpinalCase

var KeelsonTagValueRegistry = registry.MustNewTagValueRegistry[*contract.VcsManagedContract](1, NamingStyle, 8, KeelsonContract)

var (
	ValueLabelIdTagValue = KeelsonTagValueRegistry.MustBegin("valueLabel", 0).End()
)

// KeelsonHrNkRegistry pre-sizes for ~130 in-tree memberships (12 dimdata
// files; lw alone declares 55) with comfortable headroom for the next
// migration wave. Bumped from 64 → 256 in ADR-0042's post-Phase-C
// follow-up.
var KeelsonHrNkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	ValueLabelIdTagValue.GetTagValue(), 256, NamingStyle, KeelsonContract)

var (
	MembParent     = KeelsonHrNkRegistry.MustBegin("parent", 0).End()
	MembChild      = KeelsonHrNkRegistry.MustBegin("child", 1).End()
	MembNaturalKey = KeelsonHrNkRegistry.MustBegin("naturalKey", 2).End()
)
