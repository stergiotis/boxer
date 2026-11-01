package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// The trail viewer's launch config (ADR-0350, proposed): the case — a key
// kind and a value — the window opens on, an optional time window (a span
// ending now, or a fixed start and end, in Unix nanoseconds), an optional
// tab, and an optional saved view to open by name. The case kind is the one
// scalar every config carries; the rest are optional. The ordinals continue
// after mdedit's location.
var (
	MembTvLaunchCase = KeelsonHrNkRegistry.MustBegin("tvLaunchCase", 255).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTvLaunchValue = KeelsonHrNkRegistry.MustBegin("tvLaunchValue", 256).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembTvLaunchLast = KeelsonHrNkRegistry.MustBegin("tvLaunchLast", 257).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembTvLaunchFrom = KeelsonHrNkRegistry.MustBegin("tvLaunchFrom", 258).
				MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembTvLaunchTo = KeelsonHrNkRegistry.MustBegin("tvLaunchTo", 259).
			MustAddRestriction("i64Array", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembTvLaunchTab = KeelsonHrNkRegistry.MustBegin("tvLaunchTab", 260).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembTvLaunchView = KeelsonHrNkRegistry.MustBegin("tvLaunchView", 261).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
)
