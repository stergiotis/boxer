//go:build llm_generated_opus47

package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/naturalkey"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
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
	ValueLabelIdTagValue.GetTagValue(), 256, NamingStyle, 0, KeelsonContract)

var (
	MembParent     = KeelsonHrNkRegistry.MustBegin("parent").End()
	MembChild      = KeelsonHrNkRegistry.MustBegin("child").End()
	MembNaturalKey = KeelsonHrNkRegistry.MustBegin("naturalKey").End()
)
