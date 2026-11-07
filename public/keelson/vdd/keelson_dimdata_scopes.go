package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// The terrainscope launch config (ADR-0135 §SD7; apps/terrainscope/launchcfg,
// kind terrainscopeLaunch): the observer and the target as WGS84 degrees,
// the two heights above terrain in metres and the ensemble size. A zero
// height or sample count leaves the window's default. The ordinals continue
// after the agent console's.
var (
	MembTerrainscopeLaunchObserverLat = KeelsonHrNkRegistry.MustBegin("terrainscopeLaunchObserverLat", 263).
						MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTerrainscopeLaunchObserverLon = KeelsonHrNkRegistry.MustBegin("terrainscopeLaunchObserverLon", 264).
						MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTerrainscopeLaunchTargetLat = KeelsonHrNkRegistry.MustBegin("terrainscopeLaunchTargetLat", 265).
					MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTerrainscopeLaunchTargetLon = KeelsonHrNkRegistry.MustBegin("terrainscopeLaunchTargetLon", 266).
					MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTerrainscopeLaunchObserverHeight = KeelsonHrNkRegistry.MustBegin("terrainscopeLaunchObserverHeight", 267).
						MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTerrainscopeLaunchTargetHeight = KeelsonHrNkRegistry.MustBegin("terrainscopeLaunchTargetHeight", 268).
						MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembTerrainscopeLaunchSamples = KeelsonHrNkRegistry.MustBegin("terrainscopeLaunchSamples", 269).
					MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)

// The writingstylescope launch config (apps/writingstylescope/launchcfg,
// kind writingstylescopeLaunch): the two documents as markdown text and the
// section-length floor in bytes, where zero leaves the window's default.
var (
	MembWritingstylescopeLaunchDocA = KeelsonHrNkRegistry.MustBegin("writingstylescopeLaunchDocA", 270).
					MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWritingstylescopeLaunchDocB = KeelsonHrNkRegistry.MustBegin("writingstylescopeLaunchDocB", 271).
					MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembWritingstylescopeLaunchFloor = KeelsonHrNkRegistry.MustBegin("writingstylescopeLaunchFloor", 272).
						MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)

// The fibscope launch config (apps/fibscope/launchcfg, kind fibscopeLaunch):
// the 64-bit word to open the window on; zero leaves the worked example.
var (
	MembFibscopeLaunchId = KeelsonHrNkRegistry.MustBegin("fibscopeLaunchId", 273).
		MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
