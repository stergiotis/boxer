package vdd

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
)

// keelson.query memberships (ADR-0253 §SD2): the request an app holding
// `keelson.query.<table>` sends and the reply the host's introspection
// service answers with. The table itself is not here — it is a provider on
// the introspection registry (ADR-0094 §SD1); these are the wire forms that
// cross the bus.
//
// Kind-narrow (`kqReq…` / `kqReply…`) like the appstate cohort: every field
// is ExactlyOne, and the reply body is one blob rather than rows, because the
// service hands back the engine's bytes in the FORMAT the request named and
// leaves decoding to the reader. Ordinals continue after the appstate cohort.
var (
	// MembKqReqTable names the one introspection table the statement may
	// read — the subject's last token carries the same name and the two
	// must agree.
	MembKqReqTable = KeelsonHrNkRegistry.MustBegin("kqReqTable", 218).
			MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembKqReqSql is the statement, without a FORMAT clause.
	MembKqReqSql = KeelsonHrNkRegistry.MustBegin("kqReqSql", 219).
			MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembKqReqFormat is the ClickHouse FORMAT the reply body is in; empty
	// takes the engine's default. Symbol section: the set of formats a
	// reader asks for is small and closed.
	MembKqReqFormat = KeelsonHrNkRegistry.MustBegin("kqReqFormat", 220).
			MustAddRestriction("symbol", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembKqReqParamName is the bare names of the `{name:Type}` placeholders
	// a statement binds (ADR-0133 §SD2), zipped by index with the values.
	// Arbitrary cardinality — a statement may bind none. Appended after the
	// reply cohort, since the ids were taken in that order.
	MembKqReqParamName = KeelsonHrNkRegistry.MustBegin("kqReqParamName", 224).
				MustAddRestriction("symbolArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()
	// MembKqReqParamValue is the raw values, one per name.
	MembKqReqParamValue = KeelsonHrNkRegistry.MustBegin("kqReqParamValue", 225).
				MustAddRestriction("textArray", common.MembershipSpecLowCardRef, registry.CardinalityArbitrary).End()

	// MembKqReplyOk says the statement ran; the shared `reason` carries why
	// not — a refusal by the service or a failure in the engine.
	MembKqReplyOk = KeelsonHrNkRegistry.MustBegin("kqReplyOk", 221).
			MustAddRestriction("bool", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembKqReplyContentType is what the engine reported for the body.
	MembKqReplyContentType = KeelsonHrNkRegistry.MustBegin("kqReplyContentType", 222).
				MustAddRestriction("stringArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
	// MembKqReplyBody is the result bytes in the request's FORMAT.
	MembKqReplyBody = KeelsonHrNkRegistry.MustBegin("kqReplyBody", 223).
			MustAddRestriction("blobArray", common.MembershipSpecLowCardRef, registry.CardinalityExactlyOne).End()
)
