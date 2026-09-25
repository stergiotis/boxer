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

	// Persist fields. On the generated state store (`boxer.persiststate`,
	// ADR-0105 D3a) MembPersistKey tags the key string and MembPersistValue
	// (below, ordinal 73) the value bytes; app identity there is
	// MembRuntimeApp on the Owner component every state row carries. On the
	// legacy boxer.facts state rows PersistKey tagged both the symbol (key)
	// and blob (value) attributes, and PersistTombstone on the bool section
	// marked a key as deleted. Nothing writes the tombstone term any more:
	// the workingset and column-width deletes that were its last writers
	// moved to the state store with ADR-0105's Update of 2026-08-15, where a
	// delete is the generated store's lifecycle tombstone. It stays
	// registered because rows on disk carry it.
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
	// That describes the facts rows. Since ADR-0105's Update of 2026-08-15
	// a workingset is a component on the state store instead: the same
	// terms except the kind tag — a kind there is which component a row
	// carries — and the tombstone, which is the store's lifecycle column.
	// The facts rows written before the move stay readable as trail.
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
	//
	// As for workingsets, that describes the facts rows: since ADR-0105's
	// Update of 2026-08-15 an override is a component on the state store,
	// reusing every term here except the kind tag and the tombstone.
	MembKindColumnWidth   = NkRegistry.MustBegin("runtimeKindColumnWidth", 67).End()
	MembColWidthTier      = NkRegistry.MustBegin("runtimeColWidthTier", 68).End()
	MembColWidthScope     = NkRegistry.MustBegin("runtimeColWidthScope", 69).End()
	MembColWidthColumnKey = NkRegistry.MustBegin("runtimeColWidthColumnKey", 70).End()
	MembColWidthPoints    = NkRegistry.MustBegin("runtimeColWidthPoints", 71).End()
	MembColWidthFontSize  = NkRegistry.MustBegin("runtimeColWidthFontSize", 72).End()

	// Persist value (ADR-0105, Update 2026-08-15) — the state bytes on the
	// generated persist store's blob section. Minted when that store moved
	// from declaration-order ids to this vocabulary, so a store field
	// reorder can no longer renumber what is on disk; a fresh ordinal
	// rather than a second use of MembPersistKey so the value reads under
	// its own name from SQL.
	MembPersistValue = NkRegistry.MustBegin("runtimePersistValue", 73).End()

	// watchbill (ADR-0223) — the job row and its events on the store-owned
	// watchbill tables. One membership per attribute, because each is its
	// own section there: a section of one attribute is what lets a claim
	// rewrite one array element in place (ADR-0223 §SD3). The owner app and
	// the requester run reuse MembRuntimeApp and MembRuntimeRun; the worker
	// run has a membership of its own so the two runs read apart in SQL.
	MembWatchbillKind          = NkRegistry.MustBegin("watchbillKind", 74).End()
	MembWatchbillSubject       = NkRegistry.MustBegin("watchbillSubject", 75).End()
	MembWatchbillQueue         = NkRegistry.MustBegin("watchbillQueue", 76).End()
	MembWatchbillPriority      = NkRegistry.MustBegin("watchbillPriority", 77).End()
	MembWatchbillMaxAttempts   = NkRegistry.MustBegin("watchbillMaxAttempts", 78).End()
	MembWatchbillBackoff       = NkRegistry.MustBegin("watchbillBackoff", 79).End()
	MembWatchbillBackoffBaseMs = NkRegistry.MustBegin("watchbillBackoffBaseMs", 80).End()
	MembWatchbillTimeoutMs     = NkRegistry.MustBegin("watchbillTimeoutMs", 81).End()
	MembWatchbillArgsKind      = NkRegistry.MustBegin("watchbillArgsKind", 82).End()
	MembWatchbillArgs          = NkRegistry.MustBegin("watchbillArgs", 83).End()
	MembWatchbillState         = NkRegistry.MustBegin("watchbillState", 84).End()
	MembWatchbillAttempt       = NkRegistry.MustBegin("watchbillAttempt", 85).End()
	MembWatchbillRunAfter      = NkRegistry.MustBegin("watchbillRunAfter", 86).End()
	MembWatchbillWorkerRun     = NkRegistry.MustBegin("watchbillWorkerRun", 87).End()
	MembWatchbillFinishedAt    = NkRegistry.MustBegin("watchbillFinishedAt", 88).End()
	MembWatchbillLastError     = NkRegistry.MustBegin("watchbillLastError", 89).End()
	MembWatchbillEventState    = NkRegistry.MustBegin("watchbillEventState", 90).End()
	MembWatchbillEventAttempt  = NkRegistry.MustBegin("watchbillEventAttempt", 91).End()
	MembWatchbillEventWorker   = NkRegistry.MustBegin("watchbillEventWorkerRun", 92).End()
	MembWatchbillEventError    = NkRegistry.MustBegin("watchbillEventError", 93).End()
	MembWatchbillEventNote     = NkRegistry.MustBegin("watchbillEventNote", 94).End()

	// watchbill worker presence (ADR-0237) — one boxer.facts row per worker
	// run at its start and one at a clean stop: what the run drains. The
	// row is append-only; liveness is the run's heartbeat, not a field
	// here. Kind label on the symbol section as the other kinds; the run
	// gets a membership of its own so the store's scan can filter on it
	// without the reflect path; kinds and queues are symbol arrays.
	MembKindWatchbillWorker         = NkRegistry.MustBegin("runtimeKindWatchbillWorker", 95).End()
	MembWatchbillPresenceRun        = NkRegistry.MustBegin("watchbillWorkerRunId", 96).End()
	MembWatchbillPresenceHost       = NkRegistry.MustBegin("watchbillWorkerHost", 97).End()
	MembWatchbillPresencePhase      = NkRegistry.MustBegin("watchbillWorkerPhase", 98).End()
	MembWatchbillPresenceKinds      = NkRegistry.MustBegin("watchbillWorkerKinds", 99).End()
	MembWatchbillPresenceQueues     = NkRegistry.MustBegin("watchbillWorkerQueues", 100).End()
	MembWatchbillPresenceMaxWorkers = NkRegistry.MustBegin("watchbillWorkerMaxWorkers", 101).End()

	// llm calls (ADR-0254 §SD4) — one boxer.facts row per completion the
	// host's model service answered or refused: who asked, why, what it
	// cost, how it ended. Append-only; bodies are not here (a second kind
	// of its own, when kept). Kind label on the symbol section as the other
	// kinds; the call id gets a membership of its own so a scan can filter
	// on it; counts are u32/u64 units, flags bools.
	MembKindLlmCall            = NkRegistry.MustBegin("runtimeKindLlmCall", 102).End()
	MembLlmCallId              = NkRegistry.MustBegin("llmCallId", 103).End()
	MembLlmCallApp             = NkRegistry.MustBegin("llmCallApp", 104).End()
	MembLlmCallInstance        = NkRegistry.MustBegin("llmCallInstance", 105).End()
	MembLlmCallPurpose         = NkRegistry.MustBegin("llmCallPurpose", 106).End()
	MembLlmCallSensitivity     = NkRegistry.MustBegin("llmCallSensitivity", 107).End()
	MembLlmCallModel           = NkRegistry.MustBegin("llmCallModel", 108).End()
	MembLlmCallEndpointHost    = NkRegistry.MustBegin("llmCallEndpointHost", 109).End()
	MembLlmCallMessages        = NkRegistry.MustBegin("llmCallMessages", 110).End()
	MembLlmCallTools           = NkRegistry.MustBegin("llmCallTools", 111).End()
	MembLlmCallPromptBytes     = NkRegistry.MustBegin("llmCallPromptBytes", 112).End()
	MembLlmCallCompletionBytes = NkRegistry.MustBegin("llmCallCompletionBytes", 113).End()
	MembLlmCallInputTokens     = NkRegistry.MustBegin("llmCallInputTokens", 114).End()
	MembLlmCallOutputTokens    = NkRegistry.MustBegin("llmCallOutputTokens", 115).End()
	MembLlmCallToolCalls       = NkRegistry.MustBegin("llmCallToolCalls", 116).End()
	MembLlmCallFinishReason    = NkRegistry.MustBegin("llmCallFinishReason", 117).End()
	MembLlmCallElapsedMs       = NkRegistry.MustBegin("llmCallElapsedMs", 118).End()
	MembLlmCallIncomplete      = NkRegistry.MustBegin("llmCallIncomplete", 119).End()
	MembLlmCallRefused         = NkRegistry.MustBegin("llmCallRefused", 120).End()
	MembLlmCallError           = NkRegistry.MustBegin("llmCallError", 121).End()

	// vizeval scorecards (ADR-0257 §SD8) — one boxer.facts row per candidate
	// scored over a scenario at a build: which rendering, of which data, how
	// far it got, and its metrics. Append-only. Metrics are two parallel
	// arrays, names and values, so a new metric needs no new membership; the
	// gates a scenario named are split into passed and failed. The candidate's
	// canonical JSON and the free-text reason are strings, the rest symbols.
	MembKindVizevalScore   = NkRegistry.MustBegin("runtimeKindVizevalScore", 122).End()
	MembVizevalScenario    = NkRegistry.MustBegin("vizevalScenario", 123).End()
	MembVizevalCandidateId = NkRegistry.MustBegin("vizevalCandidateId", 124).End()
	MembVizevalSink        = NkRegistry.MustBegin("vizevalSink", 125).End()
	MembVizevalCandidate   = NkRegistry.MustBegin("vizevalCandidate", 126).End()
	MembVizevalBuild       = NkRegistry.MustBegin("vizevalBuild", 127).End()
	MembVizevalBatchDigest = NkRegistry.MustBegin("vizevalBatchDigest", 128).End()
	MembVizevalRows        = NkRegistry.MustBegin("vizevalRows", 129).End()
	MembVizevalStatus      = NkRegistry.MustBegin("vizevalStatus", 130).End()
	MembVizevalReason      = NkRegistry.MustBegin("vizevalReason", 131).End()
	MembVizevalDir         = NkRegistry.MustBegin("vizevalDir", 132).End()
	MembVizevalArea        = NkRegistry.MustBegin("vizevalArea", 133).End()
	MembVizevalMetricName  = NkRegistry.MustBegin("vizevalMetricName", 134).End()
	MembVizevalMetricValue = NkRegistry.MustBegin("vizevalMetricValue", 135).End()
	MembVizevalGatePassed  = NkRegistry.MustBegin("vizevalGatePassed", 136).End()
	MembVizevalGateFailed  = NkRegistry.MustBegin("vizevalGateFailed", 137).End()
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
	MembPersistValue,
	MembKindWatchbillWorker, MembWatchbillPresenceRun, MembWatchbillPresenceHost, MembWatchbillPresencePhase,
	MembWatchbillPresenceKinds, MembWatchbillPresenceQueues, MembWatchbillPresenceMaxWorkers,
	MembKindLlmCall, MembLlmCallId, MembLlmCallApp, MembLlmCallInstance, MembLlmCallPurpose, MembLlmCallSensitivity,
	MembLlmCallModel, MembLlmCallEndpointHost, MembLlmCallMessages, MembLlmCallTools, MembLlmCallPromptBytes,
	MembLlmCallCompletionBytes, MembLlmCallInputTokens, MembLlmCallOutputTokens, MembLlmCallToolCalls,
	MembLlmCallFinishReason, MembLlmCallElapsedMs, MembLlmCallIncomplete, MembLlmCallRefused, MembLlmCallError,
	MembKindVizevalScore, MembVizevalScenario, MembVizevalCandidateId, MembVizevalSink, MembVizevalCandidate,
	MembVizevalBuild, MembVizevalBatchDigest, MembVizevalRows, MembVizevalStatus, MembVizevalReason,
	MembVizevalDir, MembVizevalArea, MembVizevalMetricName, MembVizevalMetricValue,
	MembVizevalGatePassed, MembVizevalGateFailed,
}
