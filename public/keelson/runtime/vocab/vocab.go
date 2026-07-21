// Package vocab is the runtime's leeway natural-key vocabulary per
// ADR-0026 §SD6. Each constant below is a registered membership whose
// uint64 id (via GetId().Value()) is what the generated DML builders'
// AddMembership{LowCardRef,HighCardRef,MixedLowCardRef} methods take.
//
// The string constants in factsschema/memberships.go are the *conceptual*
// names ("runtime.kind.grant", "runtime.subjectFilter.pattern", …) used
// in code documentation and human-facing logs; the camelCase names below
// are the registered NATURAL keys (leeway naming convention requires single
// stylable tokens, not dotted paths).
//
// Built on boxer's stopa/registry pattern — mirrors spinnaker/vdd.
package vocab

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
)

// Contract is the runtime's leeway contract — vcs-managed convention (even
// TagValue ids).
var Contract = contract.NewVcsManagedContract()

// NamingStyle is the canonical form for runtime membership names. Spinnaker
// uses LowerSpinalCase too — keep consistent for cross-table query ergonomics.
const NamingStyle = naming.LowerSpinalCase

// TagValueRegistry allocates TagValues for runtime membership categories.
// Lives in its own scope so it does not collide with spinnaker's
// VcsTagValueRegistry — different binaries, different namespaces. The offset
// is 2, not 0: fibonacci-coded tags reserve tag value 0 as invalid
// (ADR-0106 SD8), and the vcs-managed convention keeps effective ids even.
var TagValueRegistry = registry.MustNewTagValueRegistry[*contract.VcsManagedContract](
	identifier.TagValue(2), NamingStyle, 4, Contract,
)

// MembersTagValue is the TagValue rooted at offset 0 of TagValueRegistry; it
// covers every runtime membership registered below.
var MembersTagValue = TagValueRegistry.MustBegin("runtimeMembers", 0).End()

// NkRegistry is the natural-key registry for runtime memberships. All Memb*
// constants below live in this registry.
var NkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	MembersTagValue.GetTagValue(), 32, NamingStyle, identifier.UntaggedId(0), Contract,
)

// Membership constants — vocabulary for boxer.facts rows per ADR-0026 §SD6.
var (
	// Kinds (low-card-ref): the attribute value carries the kind label
	// string (e.g. "grant") for readability; the membership id identifies
	// which kind the row belongs to.
	MembKindGrant = NkRegistry.MustBegin("runtimeKindGrant").End()
	MembKindAudit = NkRegistry.MustBegin("runtimeKindAudit").End()
	MembKindState = NkRegistry.MustBegin("runtimeKindState").End()
	MembKindEvent = NkRegistry.MustBegin("runtimeKindEvent").End()
	MembKindLog   = NkRegistry.MustBegin("runtimeKindLog").End()

	// App identity (mixed-low-card-ref + high-card-parameter): LowCardRef
	// is MembRuntimeApp; the HighCardParameter carries the AppIdT bytes.
	MembRuntimeApp = NkRegistry.MustBegin("runtimeApp").End()

	// Grant fields
	MembGrantSubjectPattern = NkRegistry.MustBegin("runtimeSubjectFilterPattern").End()
	MembGrantDirection      = NkRegistry.MustBegin("runtimeSubjectFilterDirection").End()
	MembGrantReason         = NkRegistry.MustBegin("runtimeSubjectFilterReason").End()
	MembGrantSticky         = NkRegistry.MustBegin("runtimeSubjectFilterSticky").End()
	MembGrantedVia          = NkRegistry.MustBegin("runtimeSubjectFilterGrantedVia").End()

	// Audit fields
	MembAuditRequestSubject = NkRegistry.MustBegin("runtimeAuditRequestSubject").End()
	MembAuditResult         = NkRegistry.MustBegin("runtimeAuditResult").End()
	MembAuditLatencyMs      = NkRegistry.MustBegin("runtimeAuditLatencyMs").End()
	MembAuditRequestSizeB   = NkRegistry.MustBegin("runtimeAuditRequestSizeB").End()
	MembAuditResponseSizeB  = NkRegistry.MustBegin("runtimeAuditResponseSizeB").End()

	// Persist fields. The PersistKey membership is used both in the symbol
	// section (the key string) and in the blob section (the value bytes).
	// PersistTombstone is set on the bool section when a row marks a
	// previously-persisted key as deleted; LatestState short-circuits
	// found=false when it encounters this membership.
	MembPersistKey       = NkRegistry.MustBegin("runtimePersistKey").End()
	MembPersistTombstone = NkRegistry.MustBegin("runtimePersistTombstone").End()

	// Event fields
	MembEventTopic = NkRegistry.MustBegin("runtimeEventTopic").End()

	// Runtime-run identity (kind + per-run fields). MembKindRuntimeRun
	// tags a row that records one process boot — the runtime-started
	// event. MembRuntimeRun is the mixed-low-card-ref + high-card-param
	// membership carrying the run_id bytes; app-lifecycle rows tag
	// themselves with this so a JOIN-by-run_id is a single column scan.
	MembKindRuntimeRun  = NkRegistry.MustBegin("runtimeKindRuntimeRun").End()
	MembRuntimeRun      = NkRegistry.MustBegin("runtimeRun").End()
	MembRunHostname     = NkRegistry.MustBegin("runtimeRunHostname").End()
	MembRunPid          = NkRegistry.MustBegin("runtimeRunPid").End()
	MembRunGoVersion    = NkRegistry.MustBegin("runtimeRunGoVersion").End()
	MembRunVcsRevision  = NkRegistry.MustBegin("runtimeRunVcsRevision").End()
	MembRunVcsModified  = NkRegistry.MustBegin("runtimeRunVcsModified").End()
	MembRunVcsBuildInfo = NkRegistry.MustBegin("runtimeRunVcsBuildInfo").End()
	MembRunModulePath   = NkRegistry.MustBegin("runtimeRunModulePath").End()

	// Heartbeat (kind only — the row carries no extra payload). A
	// heartbeat row tagged MembKindRuntimeHeartbeat + MembRuntimeRun
	// mixed-LCR(run_id) is emitted periodically while the runtime is
	// alive. Readers compare the latest heartbeat ts to a crash-detection
	// threshold; a runtime-start with no later heartbeats and no stopped
	// app-lifecycle rows indicates a crashed process.
	MembKindRuntimeHeartbeat = NkRegistry.MustBegin("runtimeKindRuntimeHeartbeat").End()

	// App-lifecycle (kind + per-event fields). MembKindAppLifecycle tags
	// the row; MembLifecyclePhase carries "started" / "stopped" on the
	// symbol section; MembLifecycleStopReason carries an optional free-
	// form reason for stop events ("user-close" / "mount-error" /
	// "shutdown"); MembLifecycleTileKey carries the dock-host tile key
	// on the u64 section so two tiles for the same app are
	// distinguishable in the audit trail.
	MembKindAppLifecycle     = NkRegistry.MustBegin("runtimeKindAppLifecycle").End()
	MembLifecyclePhase       = NkRegistry.MustBegin("runtimeLifecyclePhase").End()
	MembLifecycleStopReason  = NkRegistry.MustBegin("runtimeLifecycleStopReason").End()
	MembLifecycleTileKey     = NkRegistry.MustBegin("runtimeLifecycleTileKey").End()

	// Log fields — applied on rows tagged MembKindLog by logbridge / chstore.
	// MembLogLevel / MembLogCaller / MembLogService are low-cardinality
	// (process-stable enumerations) and live on the symbol section.
	// MembLogMessage / MembLogError carry free-form text on the string
	// section; MembLogStack is multi-line text. MembLogField is the
	// catch-all for arbitrary user-supplied zerolog fields — always applied
	// as MembershipSpecMixedLowCardRefHighCardParameters with the field
	// NAME as the high-card parameter and the value placed in the typed
	// section that matches the field's CBOR-decoded runtime type.
	MembLogLevel   = NkRegistry.MustBegin("runtimeLogLevel").End()
	MembLogMessage = NkRegistry.MustBegin("runtimeLogMessage").End()
	MembLogCaller  = NkRegistry.MustBegin("runtimeLogCaller").End()
	MembLogError   = NkRegistry.MustBegin("runtimeLogError").End()
	MembLogStack   = NkRegistry.MustBegin("runtimeLogStack").End()
	MembLogService = NkRegistry.MustBegin("runtimeLogService").End()
	MembLogField   = NkRegistry.MustBegin("runtimeLogField").End()

	// Query-run fields (ADR-0115 S1) — applied on rows tagged
	// MembKindQueryRun by the queryrunsd capture pipeline
	// (runtime/queryrunfacts): one fact per terminal system.query_log
	// event. The natural key is the ClickHouse query_id; app / run
	// identity reuses MembRuntimeApp / MembRuntimeRun above, lifted from
	// the client's log_comment stamp (ADR-0115 SD7).
	//
	// Event type ("QueryFinish" / "ExceptionBeforeStart" /
	// "ExceptionWhileProcessing"), query kind ("Select" / "Insert" / …)
	// and the stamped play lane are process-stable enumerations on the
	// symbol section. Counters (duration, IO, result size, peak memory,
	// normalized_query_hash) live on the u64 section; the exception code
	// on the i64 section; exception text, the capped inline query text
	// (interning is deferred to ADR-0112) and the four identity
	// fingerprints on the string section. MembQueryRunProfileEvent is the
	// per-ProfileEvents-counter membership, always applied as
	// MembershipSpecMixedLowCardRefHighCardParameters with the event NAME
	// as the high-card parameter and the count on the u64 section — the
	// MembLogField pattern.
	MembKindQueryRun            = NkRegistry.MustBegin("runtimeKindQueryRun").End()
	MembQueryRunEventType       = NkRegistry.MustBegin("runtimeQueryRunEventType").End()
	MembQueryRunQueryKind       = NkRegistry.MustBegin("runtimeQueryRunQueryKind").End()
	MembQueryRunLane            = NkRegistry.MustBegin("runtimeQueryRunLane").End()
	MembQueryRunDurationMs      = NkRegistry.MustBegin("runtimeQueryRunDurationMs").End()
	MembQueryRunReadRows        = NkRegistry.MustBegin("runtimeQueryRunReadRows").End()
	MembQueryRunReadBytes       = NkRegistry.MustBegin("runtimeQueryRunReadBytes").End()
	MembQueryRunWrittenRows     = NkRegistry.MustBegin("runtimeQueryRunWrittenRows").End()
	MembQueryRunWrittenBytes    = NkRegistry.MustBegin("runtimeQueryRunWrittenBytes").End()
	MembQueryRunResultRows      = NkRegistry.MustBegin("runtimeQueryRunResultRows").End()
	MembQueryRunResultBytes     = NkRegistry.MustBegin("runtimeQueryRunResultBytes").End()
	MembQueryRunMemoryPeakBytes = NkRegistry.MustBegin("runtimeQueryRunMemoryPeakBytes").End()
	MembQueryRunNormalizedHash  = NkRegistry.MustBegin("runtimeQueryRunNormalizedHash").End()
	MembQueryRunExceptionCode   = NkRegistry.MustBegin("runtimeQueryRunExceptionCode").End()
	MembQueryRunExceptionText   = NkRegistry.MustBegin("runtimeQueryRunExceptionText").End()
	MembQueryRunQueryText       = NkRegistry.MustBegin("runtimeQueryRunQueryText").End()
	MembQueryRunAuthoredFp      = NkRegistry.MustBegin("runtimeQueryRunAuthoredFp").End()
	MembQueryRunSentFp          = NkRegistry.MustBegin("runtimeQueryRunSentFp").End()
	MembQueryRunChainFp         = NkRegistry.MustBegin("runtimeQueryRunChainFp").End()
	MembQueryRunEnvFp           = NkRegistry.MustBegin("runtimeQueryRunEnvFp").End()
	MembQueryRunProfileEvent    = NkRegistry.MustBegin("runtimeQueryRunProfileEvent").End()

	// App-launch (kind + per-request fields), ADR-0135 §SD6 — one row per
	// accepted `windowhost.open` request, written beside the app-lifecycle
	// "started" row. Target app / run identity reuse MembRuntimeApp /
	// MembRuntimeRun; the opened window's key reuses MembLifecycleTileKey
	// so the launch row joins its lifecycle row on the same column.
	// MembLaunchCaller is the requesting app, attributed from the bus
	// envelope (Msg.Sender) — mixed-low-card-ref with the caller AppIdT
	// bytes as the high-card parameter, the MembRuntimeApp pattern.
	// MembLaunchConfigKind carries the config's vocabulary kind name on
	// the symbol section; MembLaunchConfig the raw facts-CBOR config
	// bytes on the blob section (bounded by the host's 64 KiB cap).
	MembKindLaunch       = NkRegistry.MustBegin("runtimeKindLaunch").End()
	MembLaunchCaller     = NkRegistry.MustBegin("runtimeLaunchCaller").End()
	MembLaunchConfigKind = NkRegistry.MustBegin("runtimeLaunchConfigKind").End()
	MembLaunchConfig     = NkRegistry.MustBegin("runtimeLaunchConfig").End()
)

// AllMembs is the enumerated set of registered runtime memberships. Tests
// iterate to assert invariants (non-zero ids, unique ids).
var AllMembs = []registry.RegisteredNaturalKey{
	MembKindGrant, MembKindAudit, MembKindState, MembKindEvent, MembKindLog,
	MembKindRuntimeRun, MembKindRuntimeHeartbeat, MembKindAppLifecycle,
	MembRuntimeApp, MembRuntimeRun,
	MembGrantSubjectPattern, MembGrantDirection, MembGrantReason, MembGrantSticky, MembGrantedVia,
	MembAuditRequestSubject, MembAuditResult, MembAuditLatencyMs, MembAuditRequestSizeB, MembAuditResponseSizeB,
	MembPersistKey, MembPersistTombstone,
	MembEventTopic,
	MembRunHostname, MembRunPid, MembRunGoVersion, MembRunVcsRevision, MembRunVcsModified, MembRunVcsBuildInfo, MembRunModulePath,
	MembLifecyclePhase, MembLifecycleStopReason, MembLifecycleTileKey,
	MembLogLevel, MembLogMessage, MembLogCaller, MembLogError, MembLogStack, MembLogService, MembLogField,
	MembKindQueryRun, MembQueryRunEventType, MembQueryRunQueryKind, MembQueryRunLane,
	MembQueryRunDurationMs, MembQueryRunReadRows, MembQueryRunReadBytes,
	MembQueryRunWrittenRows, MembQueryRunWrittenBytes, MembQueryRunResultRows, MembQueryRunResultBytes,
	MembQueryRunMemoryPeakBytes, MembQueryRunNormalizedHash,
	MembQueryRunExceptionCode, MembQueryRunExceptionText, MembQueryRunQueryText,
	MembQueryRunAuthoredFp, MembQueryRunSentFp, MembQueryRunChainFp, MembQueryRunEnvFp,
	MembQueryRunProfileEvent,
	MembKindLaunch, MembLaunchCaller, MembLaunchConfigKind, MembLaunchConfig,
}
