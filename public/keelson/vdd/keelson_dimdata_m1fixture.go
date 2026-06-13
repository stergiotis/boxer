package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// M1-fixture memberships — the demonstration fact-kind used by
// keelson/runtime/codec/m1fixture to validate the ADR-0042 wire shape
// end-to-end against clickhouse-local. Names are prefixed `m1` so they
// are easy to spot and retire once the generator's first real
// production fact kind lands.
var (
	MembM1Source = KeelsonHrNkRegistry.MustBegin("m1Source").
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1Severity = KeelsonHrNkRegistry.MustBegin("m1Severity").
			MustAddRestriction("u8Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1MajorVer = KeelsonHrNkRegistry.MustBegin("m1MajorVer").
			MustAddRestriction("u16Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1Sequence = KeelsonHrNkRegistry.MustBegin("m1Sequence").
			MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1LatencyNanos = KeelsonHrNkRegistry.MustBegin("m1LatencyNanos").
				MustAddRestriction("u64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1CpuPct = KeelsonHrNkRegistry.MustBegin("m1CpuPct").
			MustAddRestriction("f32Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1LoadAvg1 = KeelsonHrNkRegistry.MustBegin("m1LoadAvg1").
			MustAddRestriction("f64Array", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1Healthy = KeelsonHrNkRegistry.MustBegin("m1Healthy").
			MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1PeerV4 = KeelsonHrNkRegistry.MustBegin("m1PeerV4").
			MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1PeerV6 = KeelsonHrNkRegistry.MustBegin("m1PeerV6").
			MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	MembM1LastSuccess = KeelsonHrNkRegistry.MustBegin("m1LastSuccess").
				MustAddRestriction("timeArray", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembM1OperatorName = KeelsonHrNkRegistry.MustBegin("m1OperatorName").
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityZeroToOne).End()
	MembM1Tags = KeelsonHrNkRegistry.MustBegin("m1Tags").
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	MembM1CapBits = KeelsonHrNkRegistry.MustBegin("m1CapBits").
			MustAddRestriction("u32Array", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
)
