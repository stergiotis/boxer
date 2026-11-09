package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// The launch config that opens an editor on a handed-over text
// (externaledit/launchcfg): the edit's id. A scalar, so the ExactlyOne
// cardinality of every other launch config holds. The ordinal continues
// after the last cohort minted in this registry.
var (
	MembExtEditLaunchId = KeelsonHrNkRegistry.MustBegin("extEditLaunchId", 274).
		MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
