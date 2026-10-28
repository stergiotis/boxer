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

// A document handed over as a lading snapshot (ADR-0339 §SD5, proposed): the
// mount, the snapshot instant and the document's path in it, so the window
// reads the images the document embeds from the same snapshot. The ordinals
// continue after the ad-hoc cohort's.
var (
	MembMdeditLaunchMount = KeelsonHrNkRegistry.MustBegin("mdeditLaunchMount", 247).
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembMdeditLaunchSnapshot = KeelsonHrNkRegistry.MustBegin("mdeditLaunchSnapshot", 248).
					MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembMdeditLaunchPath = KeelsonHrNkRegistry.MustBegin("mdeditLaunchPath", 249).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
)
