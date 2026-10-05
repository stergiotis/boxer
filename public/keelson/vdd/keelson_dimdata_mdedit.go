package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// The mdedit launch config (ADR-0178, update of 2026-10-03): a document
// another app hands over — its markdown and a name to show and to offer
// when it is saved. The ordinals continue after the app center's.
var (
	MembMdeditLaunchText = KeelsonHrNkRegistry.MustBegin("mdeditLaunchText", 227).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembMdeditLaunchName = KeelsonHrNkRegistry.MustBegin("mdeditLaunchName", 228).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
