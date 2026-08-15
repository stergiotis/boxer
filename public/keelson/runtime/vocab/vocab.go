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
// Built on boxer's namemint/registry pattern — mirrors spinnaker/vdd.
package vocab

import (
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Contract is the runtime's leeway contract — vcs-managed convention (even
// TagValue ids).
var Contract = contract.NewVcsManagedContract()

// NamingStyle is the canonical form for runtime membership names. Spinnaker
// uses LowerSpinalCase too — keep consistent for cross-table query ergonomics.
const NamingStyle = naming.LowerSpinalCase

// TagValueClaim is this vocabulary's tag value, claimed from the width-32
// class every version-controlled vocabulary claims from (ADR-0183 D0). The
// second of the class, one above vdd's.
//
// It used to be 2, picked by hand — and the jsonbench trial's vocabulary
// independently picked 2 as well, which made every runtime id and every
// jsonbench id identical. They shared no table, so nothing broke; the claim is
// what makes that a refusal instead of a coincidence.
var TagValueClaim = tagmint.MustClaim("keelsonRuntime", 2178310, MaxExpectedMemberships)

// MaxExpectedMemberships is what this vocabulary tells the mint it will need.
// The width-32 class holds about 4.3e9, so the number is headroom rather than
// a quota; it is stated so a future claim from a narrower class is refused
// rather than silently too small.
const MaxExpectedMemberships = 1 << 20

// NkRegistry is the natural-key registry for runtime memberships. All Memb*
// constants below live in this registry.
var NkRegistry = registry.MustNewNaturalKeyRegistry[*contract.VcsManagedContract](
	TagValueClaim, 32, NamingStyle, Contract,
)

// Membership constants — vocabulary for boxer.facts rows per ADR-0026 §SD6.
var (
	// Kinds (low-card-ref): the attribute value carries the kind label
	// string (e.g. "grant") for readability; the membership id identifies
	// which kind the row belongs to.
	MembKindGrant = NkRegistry.MustBegin("runtimeKindGrant", 0).End()
	MembKindAudit = NkRegistry.MustBegin("runtimeKindAudit", 1).End()
	MembKindState = NkRegistry.MustBegin("runtimeKindState", 2).End()
	MembKindEvent = NkRegistry.MustBegin("runtimeKindEvent", 3).End()
	MembKindLog   = NkRegistry.MustBegin("runtimeKindLog", 4).End()

	// App identity (mixed-low-card-ref + high-card-parameter): LowCardRef
	// is MembRuntimeApp; the HighCardParameter carries the AppIdT bytes.
	MembRuntimeApp = NkRegistry.MustBegin("runtimeApp", 5).End()

	// Grant fields
	MembGrantSubjectPattern = NkRegistry.MustBegin("runtimeSubjectFilterPattern", 6).End()
	MembGrantDirection      = NkRegistry.MustBegin("runtimeSubjectFilterDirection", 7).End()
	MembGrantReason         = NkRegistry.MustBegin("runtimeSubjectFilterReason", 8).End()
	MembGrantSticky         = NkRegistry.MustBegin("runtimeSubjectFilterSticky", 9).End()
	MembGrantedVia          = NkRegistry.MustBegin("runtimeSubjectFilterGrantedVia", 10).End()

	// Audit fields
	MembAuditRequestSubject = NkRegistry.MustBegin("runtimeAuditRequestSubject", 11).End()
	MembAuditResult         = NkRegistry.MustBegin("runtimeAuditResult", 12).End()
	MembAuditLatencyMs      = NkRegistry.MustBegin("runtimeAuditLatencyMs", 13).End()
	MembAuditRequestSizeB   = NkRegistry.MustBegin("runtimeAuditRequestSizeB", 14).End()
	MembAuditResponseSizeB  = NkRegistry.MustBegin("runtimeAuditResponseSizeB", 15).End()

	// Persist fields. On the legacy boxer.facts state rows PersistKey tagged
	// both the symbol (key) and blob (value) attributes, and PersistTombstone
	// on the bool section marked a key as deleted — the tombstone term is
	// still what DeleteWorkingset and DeleteColumnWidth write. App persist
	// state itself now lives on the generated persist store (ADR-0105 D3a).
	MembPersistKey       = NkRegistry.MustBegin("runtimePersistKey", 16).End()
	MembPersistTombstone = NkRegistry.MustBegin("runtimePersistTombstone", 17).End()

	// Event fields
	MembEventTopic = NkRegistry.MustBegin("runtimeEventTopic", 18).End()

	// Runtime-run identity (kind + per-run fields). MembKindRuntimeRun
	// tags a row that records one process boot — the runtime-started
	// event. MembRuntimeRun is the mixed-low-card-ref + high-card-param
	// membership carrying the run_id bytes; app-lifecycle rows tag
	// themselves with this so a JOIN-by-run_id is a single column scan.
	MembKindRuntimeRun  = NkRegistry.MustBegin("runtimeKindRuntimeRun", 19).End()
	MembRuntimeRun      = NkRegistry.MustBegin("runtimeRun", 20).End()
	MembRunHostname     = NkRegistry.MustBegin("runtimeRunHostname", 21).End()
	MembRunPid          = NkRegistry.MustBegin("runtimeRunPid", 22).End()
	MembRunGoVersion    = NkRegistry.MustBegin("runtimeRunGoVersion", 23).End()
	MembRunVcsRevision  = NkRegistry.MustBegin("runtimeRunVcsRevision", 24).End()
	MembRunVcsModified  = NkRegistry.MustBegin("runtimeRunVcsModified", 25).End()
	MembRunVcsBuildInfo = NkRegistry.MustBegin("runtimeRunVcsBuildInfo", 26).End()
	MembRunModulePath   = NkRegistry.MustBegin("runtimeRunModulePath", 27).End()

	// Heartbeat (kind only — the row carries no extra payload). A
	// heartbeat row tagged MembKindRuntimeHeartbeat + MembRuntimeRun
	// mixed-LCR(run_id) is emitted periodically while the runtime is
	// alive. Readers compare the latest heartbeat ts to a crash-detection
	// threshold; a runtime-start with no later heartbeats and no stopped
	// app-lifecycle rows indicates a crashed process.
	MembKindRuntimeHeartbeat = NkRegistry.MustBegin("runtimeKindRuntimeHeartbeat", 28).End()

	// App-lifecycle (kind + per-event fields). MembKindAppLifecycle tags
	// the row; MembLifecyclePhase carries "started" / "stopped" on the
	// symbol section; MembLifecycleStopReason carries an optional free-
	// form reason for stop events ("user-close" / "mount-error" /
	// "shutdown"); MembLifecycleTileKey carries the dock-host tile key
	// on the u64 section so two tiles for the same app are
	// distinguishable in the audit trail.
	MembKindAppLifecycle    = NkRegistry.MustBegin("runtimeKindAppLifecycle", 29).End()
	MembLifecyclePhase      = NkRegistry.MustBegin("runtimeLifecyclePhase", 30).End()
	MembLifecycleStopReason = NkRegistry.MustBegin("runtimeLifecycleStopReason", 31).End()
	MembLifecycleTileKey    = NkRegistry.MustBegin("runtimeLifecycleTileKey", 32).End()

	// Log fields — applied on rows tagged MembKindLog by logbridge / chstore.
	// MembLogLevel / MembLogCaller / MembLogService are low-cardinality
	// (process-stable enumerations) and live on the symbol section.
	// MembLogMessage / MembLogError carry free-form text on the string
	// section; MembLogStack is multi-line text. MembLogField is the
	// catch-all for arbitrary user-supplied zerolog fields — always applied
	// as MembershipSpecMixedLowCardRefHighCardParameters with the field
	// NAME as the high-card parameter and the value placed in the typed
	// section that matches the field's CBOR-decoded runtime type.
	MembLogLevel   = NkRegistry.MustBegin("runtimeLogLevel", 33).End()
	MembLogMessage = NkRegistry.MustBegin("runtimeLogMessage", 34).End()
	MembLogCaller  = NkRegistry.MustBegin("runtimeLogCaller", 35).End()
	MembLogError   = NkRegistry.MustBegin("runtimeLogError", 36).End()
	MembLogStack   = NkRegistry.MustBegin("runtimeLogStack", 37).End()
	MembLogService = NkRegistry.MustBegin("runtimeLogService", 38).End()
	MembLogField   = NkRegistry.MustBegin("runtimeLogField", 39).End()

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
	MembKindQueryRun            = NkRegistry.MustBegin("runtimeKindQueryRun", 40).End()
	MembQueryRunEventType       = NkRegistry.MustBegin("runtimeQueryRunEventType", 41).End()
	MembQueryRunQueryKind       = NkRegistry.MustBegin("runtimeQueryRunQueryKind", 42).End()
	MembQueryRunLane            = NkRegistry.MustBegin("runtimeQueryRunLane", 43).End()
	MembQueryRunDurationMs      = NkRegistry.MustBegin("runtimeQueryRunDurationMs", 44).End()
	MembQueryRunReadRows        = NkRegistry.MustBegin("runtimeQueryRunReadRows", 45).End()
	MembQueryRunReadBytes       = NkRegistry.MustBegin("runtimeQueryRunReadBytes", 46).End()
	MembQueryRunWrittenRows     = NkRegistry.MustBegin("runtimeQueryRunWrittenRows", 47).End()
	MembQueryRunWrittenBytes    = NkRegistry.MustBegin("runtimeQueryRunWrittenBytes", 48).End()
	MembQueryRunResultRows      = NkRegistry.MustBegin("runtimeQueryRunResultRows", 49).End()
	MembQueryRunResultBytes     = NkRegistry.MustBegin("runtimeQueryRunResultBytes", 50).End()
	MembQueryRunMemoryPeakBytes = NkRegistry.MustBegin("runtimeQueryRunMemoryPeakBytes", 51).End()
	MembQueryRunNormalizedHash  = NkRegistry.MustBegin("runtimeQueryRunNormalizedHash", 52).End()
	MembQueryRunExceptionCode   = NkRegistry.MustBegin("runtimeQueryRunExceptionCode", 53).End()
	MembQueryRunExceptionText   = NkRegistry.MustBegin("runtimeQueryRunExceptionText", 54).End()
	MembQueryRunQueryText       = NkRegistry.MustBegin("runtimeQueryRunQueryText", 55).End()
	MembQueryRunAuthoredFp      = NkRegistry.MustBegin("runtimeQueryRunAuthoredFp", 56).End()
	MembQueryRunSentFp          = NkRegistry.MustBegin("runtimeQueryRunSentFp", 57).End()
	MembQueryRunChainFp         = NkRegistry.MustBegin("runtimeQueryRunChainFp", 58).End()
	MembQueryRunEnvFp           = NkRegistry.MustBegin("runtimeQueryRunEnvFp", 59).End()
	MembQueryRunProfileEvent    = NkRegistry.MustBegin("runtimeQueryRunProfileEvent", 60).End()

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
	MembKindLaunch       = NkRegistry.MustBegin("runtimeKindLaunch", 61).End()
	MembLaunchCaller     = NkRegistry.MustBegin("runtimeLaunchCaller", 62).End()
	MembLaunchConfigKind = NkRegistry.MustBegin("runtimeLaunchConfigKind", 63).End()
	MembLaunchConfig     = NkRegistry.MustBegin("runtimeLaunchConfig", 64).End()

	// App-workingset (kind + name), ADR-0148 §SD6 — one row per saved
	// workingset: the launch config that would reproduce the closing
	// window's user-authored state, written at the closing edge exactly as
	// the launch row records the opening edge. The record IS the app's
	// launch-config DTO (§SD2), so the columns that coincide reuse the
	// launch cohort's terms rather than minting parallel ones:
	// MembRuntimeApp / MembRuntimeRun for identity, MembLifecycleTileKey
	// for the closing window's key, MembLaunchConfigKind /
	// MembLaunchConfig for the payload, MembLifecycleStopReason for the
	// save provenance ("user-close" / "shutdown" / …), and
	// MembPersistTombstone on the bool section for a DeleteWorkingset row
	// (the persist-state tombstone pattern). Only two terms are new: the
	// kind tag, and the caller-chosen set name on the symbol section (v1
	// wires exactly one name, "default" — §SD3).
	//
	// The ordinals continue the block above rather than reusing any: each
	// registration states its own, and persisted facts rows carry it (the
	// ADR-0135 ordering constraint, now enforced by the registry itself —
	// ADR-0183 D0).
	MembKindWorkingset = NkRegistry.MustBegin("runtimeKindWorkingset", 65).End()
	MembWorkingsetName = NkRegistry.MustBegin("runtimeWorkingsetName", 66).End()

	// Table column-width override (ADR-0151, Update 2026-07-30) — one row
	// per override entry rather than one document per app, so the trail is
	// the history and last-writer-wins lands at entry granularity instead
	// of document granularity. App identity reuses MembRuntimeApp and a
	// cleared override reuses MembPersistTombstone on the bool section, the
	// persist-state tombstone pattern that DeleteWorkingset also follows.
	//
	// The identity of an entry is (app, tier, scope, columnKey). Tier is one
	// of "instance" / "shape" / "column" (§SD1) and is genuinely
	// low-cardinality; scope carries the tableTag for the instance tier and
	// the shape hash for the shape tier, and is empty for the column tier,
	// whose whole point is to apply anywhere in the app. ColumnKey is the
	// blake3short of (name, typeDiscriminator) — a type change is meant to
	// invalidate the override, which falls out of the key rather than
	// needing a rule.
	//
	// Points and FontSize ride the f64 section as a pair because a width is
	// only meaningful against the font it was captured at; resolution
	// rescales proportionally when the two disagree (§SD1).
	//
	// Fresh ordinals for the same reason the workingset terms took theirs:
	// persisted rows carry the id a name was given.
	MembKindColumnWidth   = NkRegistry.MustBegin("runtimeKindColumnWidth", 67).End()
	MembColWidthTier      = NkRegistry.MustBegin("runtimeColWidthTier", 68).End()
	MembColWidthScope     = NkRegistry.MustBegin("runtimeColWidthScope", 69).End()
	MembColWidthColumnKey = NkRegistry.MustBegin("runtimeColWidthColumnKey", 70).End()
	MembColWidthPoints    = NkRegistry.MustBegin("runtimeColWidthPoints", 71).End()
	MembColWidthFontSize  = NkRegistry.MustBegin("runtimeColWidthFontSize", 72).End()
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
	MembKindWorkingset, MembWorkingsetName,
	MembKindColumnWidth, MembColWidthTier, MembColWidthScope,
	MembColWidthColumnKey, MembColWidthPoints, MembColWidthFontSize,
}
