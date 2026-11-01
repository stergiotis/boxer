package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// MembAcLaunchAppId is the app center's launch config (ADR-0260 §SD1): the
// app whose page the window opens on. One scalar, so the ExactlyOne
// cardinality of every other launch config holds. The ordinal continues
// after the keelson.query cohort.
var MembAcLaunchAppId = KeelsonHrNkRegistry.MustBegin("acLaunchAppId", 226).
	MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()

// MembAcLaunchFilter is the Running tab's filter the window opens with
// (ADR-0351 §SD6): the slow-window notice opens the tab on the slow window.
// Empty filters nothing.
var MembAcLaunchFilter = KeelsonHrNkRegistry.MustBegin("acLaunchFilter", 262).
	MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
