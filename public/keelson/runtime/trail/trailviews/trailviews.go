// Package trailviews composes read-only ClickHouse views over the audit
// trail on `boxer.facts` (ADR-0277): one flat view per trail kind and a
// unified timeline across all of them — data marts, row for row — and three
// aggregates over those: one row per agent action with its outcome, one per
// conversation, one per agent task.
//
// The trail is written for audit: every row is an entity of leeway
// sections, and reading one attribute means locating it by membership.
// These views do that once, so a reader — a person in play, or an agent
// granted `clickhouse:<host>` — gets one column per attribute, one row per
// event, with a one-line headline it can scan before it drills down.
//
// # Data marts and aggregates
//
// The two classes are named for what a reader may rely on, in the terms of
// ADR-0051's query categories:
//
//   - `dm_…` — a data mart: output rows are 1:1 with `facts` rows and keyed
//     by the fact's own id. The per-kind views select and project; the
//     timeline is the UNION ALL of disjoint per-kind sets (a trail row
//     carries exactly one domain kind). Each carries the facts backbone —
//     id, timestamp, natural key — under its physical names.
//   - `agg_…` — analytical with lineage: each output row is derived from
//     several facts rows and lists them in `facts-ids`, so a reader can go
//     from a digest row to the exact rows behind it.
//
// # Leeway-shaped
//
// Every view is itself a leeway table. Each value column is minted with
// `LW_PLAIN(expr, '<name>', '<canonical type>', 'item:oq')` — an opaque
// plain column whose physical name carries its logical name and canonical
// type (`oq:purpose:s:::0:`) — and its value is cast to that type, so the
// name tells the truth. leeway.columns decodes the views like any leeway
// table, play reaches a column by handle (`opaque:purpose`, `opaque:*`),
// and an `INSERT … SELECT` into a materialized leeway table adopts the
// names unchanged. A reader outside play's pass pipeline quotes the
// physical name instead; [View.Columns] lists both.
//
// # Written against the SQL read surface
//
// The data marts over facts are authored with column handles (ADR-0116)
// and the `LW_GET` family (ADR-0181 §SD3), naming memberships by their
// registered natural key, and every output through `LW_PLAIN`. [prepare]
// expands them client-side against the trail store's generated schema —
// handles to physical names, `LW_GET` to the read-back helpers, names to
// ids, `LW_PLAIN` to minted aliases — as queryrunfacts' history query
// does; a view stores the expanded SELECT, because a server-side view runs
// outside play's pass pipeline.
//
// The timeline and the aggregates read the views below them through
// `{name}` placeholders, resolved to the physical names the upstream view
// minted (the same [lwsql.Composer] `LW_PLAIN` expands through, so the two
// cannot disagree), and mint their own outputs with `LW_PLAIN` again.
//
// # Names, and what a view is a snapshot of
//
// The audit trail is general to keelson, and so are the timeline and the
// egress view. Every view over what the agentic side writes — model calls,
// messages, agent actions, grants, captures, disclosures and the action,
// conversation and task aggregates — carries the agentic tag in its name
// (`…_trail_llm_…`).
//
// ClickHouse expands a SQL UDF into a view's stored query when the view is
// created, so a view keeps the read-surface bodies it was created with.
// Each view is stamped with [Stamp] in its COMMENT; re-posting [Compose]'s
// statements after a surface install is what refreshes them.
//
// # What the views do not add
//
// They read what the trail records and nothing more: an agent action
// carries its argument digest, not its arguments, and no operation result
// is recorded at all; a message's text is present only when the
// conversation was kept (ADR-0264). A view cannot recover either, and a
// column reading empty is the trail saying so, not the view losing it.
//
// Sensitivity labels are passed through as columns (`confined`,
// `tainted`), not enforced: a view is SQL, and what a reader may do with
// a confined row is the reader's dispatch decision, not the view's.
package trailviews

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/internal/lowlevel"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chviews"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/registry"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// ViewsVersion is the revision of the views this package composes. Bump it
// whenever a view's columns or meaning change, so the stamp a deployed view
// carries tells an operator it predates the build.
const ViewsVersion = 3

// agenticTag marks the views over what the agentic side of keelson writes —
// model calls and their messages, and what agents did under a grant. The
// audit trail itself is general to keelson; these views are the part of it
// that is about agents, and the tag says so in every one of their names.
// It is spelled `llm` today; renaming it is this one constant.
const agenticTag = "llm"

// The class prefixes (ADR-0051's categories): a data mart is 1:1 with facts
// rows; an aggregate derives each row from several and names them.
const (
	prefixDataMart  = "dm_"
	prefixAggregate = "agg_"
)

// The view names, unqualified. Each is created in the database the facts
// table lives in. The general ones — the timeline over every kind, and the
// egress fetches — carry no agentic tag.
const (
	ViewModelCalls       = prefixDataMart + "trail_" + agenticTag + "_model_calls"
	ViewModelMessages    = prefixDataMart + "trail_" + agenticTag + "_model_messages"
	ViewAgentActions     = prefixDataMart + "trail_" + agenticTag + "_agent_actions"
	ViewAgentGrants      = prefixDataMart + "trail_" + agenticTag + "_agent_grants"
	ViewAgentCaptures    = prefixDataMart + "trail_" + agenticTag + "_agent_captures"
	ViewAgentDisclosures = prefixDataMart + "trail_" + agenticTag + "_agent_disclosures"
	ViewHttpFetches      = prefixDataMart + "trail_http_fetches"
	ViewAdhocBundles     = prefixDataMart + "trail_adhoc_bundles"
	ViewTimeline         = prefixDataMart + "trail_timeline"
	ViewActionOutcomes   = prefixAggregate + "trail_" + agenticTag + "_agent_action_outcomes"
	ViewConversations    = prefixAggregate + "trail_" + agenticTag + "_conversations"
	ViewTasks            = prefixAggregate + "trail_" + agenticTag + "_tasks"
)

// AllViewNames lists the views in creation order, unqualified.
func AllViewNames() (names []string) {
	names = make([]string, 0, len(kindViews)+4)
	for _, kv := range kindViews {
		names = append(names, kv.name)
	}
	return append(names, ViewTimeline, ViewActionOutcomes, ViewConversations, ViewTasks)
}

// Stamp is what every view records in its ClickHouse COMMENT: this
// package's revision and the read-surface revision whose function bodies
// the view inlines. ClickHouse expands a SQL UDF into a view's stored query
// at CREATE time (see chviews), so a view is a snapshot of both; comparing
// system.tables.comment against Stamp is how a stale one is found rather
// than read.
func Stamp() (stamp chviews.Stamp) {
	return chviews.Stamp("keelson trail views v" + strconv.Itoa(ViewsVersion) +
		" over leeway SQL read surface v" + strconv.Itoa(lwsqlsurface.Version))
}

// View is one composed view: its unqualified name, the statement that
// creates it — expanded and ready to post — and its output columns.
type View struct {
	Name    string
	Sql     string
	Columns []Column
}

// Column is one output column of a view: the logical name a handle uses
// (`opaque:<Name>` for a minted column), its canonical type, the ClickHouse
// type its values are cast to, and the physical name it is selected by.
// Backbone columns are the facts table's own and keep their names.
type Column struct {
	Name          string
	CanonicalType string
	ClickHouse    string
	Physical      string
	Backbone      bool
}

// The canonical types the views mint, and the ClickHouse type each value is
// cast to. Kept to the handful the trail needs; a type outside it fails
// composition rather than minting a name the value does not match.
var clickhouseTypes = map[string]string{
	"s":    "String",
	"sh":   "Array(String)",
	"b":    "Bool",
	"u32":  "UInt32",
	"u64":  "UInt64",
	"i64":  "Int64",
	"u64h": "Array(UInt64)",
	"z64":  "DateTime64(9, 'UTC')",
}

// castTo renders the cast that makes expr's value match ctype.
func castTo(expr string, ctype string) (out string, err error) {
	ch, ok := clickhouseTypes[ctype]
	if !ok {
		return "", eb.Build().Str("canonicalType", ctype).Errorf("trailviews: no ClickHouse type for canonical type")
	}
	if ctype == "z64" {
		return "toDateTime64(" + expr + ", 9, 'UTC')", nil
	}
	return "CAST(" + expr + ", '" + ch + "')", nil
}

// mintItem is the item kind every minted column declares: an opaque plain
// value — the views carry no identity, routing or lifecycle of their own
// beyond the facts backbone they pass through.
const mintItem = "item:oq"

// minter composes physical names exactly as LW_PLAIN's expansion does.
type minter struct {
	composer *lwsql.Composer
}

func newMinter() (inst minter, err error) {
	c, err := lwsql.NewComposer(lwsql.DefaultTableSegments())
	if err != nil {
		return minter{}, eh.Errorf("trailviews: composer: %w", err)
	}
	return minter{composer: c}, nil
}

// column builds a minted column's description.
func (inst minter) column(name string, ctype string) (c Column, err error) {
	ch, ok := clickhouseTypes[ctype]
	if !ok {
		return Column{}, eb.Build().Str("column", name).Str("canonicalType", ctype).Errorf("trailviews: unsupported canonical type")
	}
	physical, err := inst.composer.PlainColumn(name, ctype, []string{mintItem})
	if err != nil {
		return Column{}, eb.Build().Str("column", name).Errorf("trailviews: mint: %w", err)
	}
	return Column{Name: name, CanonicalType: ctype, ClickHouse: ch, Physical: physical}, nil
}

// output is one projected value, authored: an expression, the logical name
// it is minted under, and its canonical type.
type output struct {
	expr  string
	name  string
	ctype string
}

// mint renders an output as an authored LW_PLAIN call over the cast value.
func (o output) mint() (sql string, err error) {
	cast, err := castTo(o.expr, o.ctype)
	if err != nil {
		return "", eb.Build().Str("column", o.name).Errorf("trailviews: %w", err)
	}
	return fmt.Sprintf("LW_PLAIN(%s, '%s', '%s', '%s')", cast, o.name, o.ctype, mintItem), nil
}

// readShape is how a column is read off its section.
type readShape uint8

const (
	// scalar reads a section that stores one value per attribute
	// (`symbol`, `bool`) through LW_GET.
	scalar readShape = iota
	// first reads the single value of an attribute on an array-valued
	// section — the DTO field tagged `unit` — as the first of its run.
	first
	// list reads every value of an attribute on an array-valued section.
	list
)

// column is one projected trail attribute: the logical name it is minted
// under, the section its membership lives on, and how it is read.
type column struct {
	name    string
	section string
	memb    registry.RegisteredNaturalKey
	shape   readShape
}

// canonicalType is the type a trail attribute is minted with, from its
// section and how it is read: the section fixes the element type, the
// shape whether the value is one element or the run.
func (c column) canonicalType() (ctype string, err error) {
	elem := map[string]string{
		"symbol": "s", "bool": "b", "stringArray": "s", "textArray": "s", "symbolArray": "s",
		"u32Array": "u32", "u64Array": "u64", "i64Array": "i64",
	}[c.section]
	if elem == "" {
		return "", eb.Build().Str("section", c.section).Errorf("trailviews: no canonical type for section")
	}
	if c.shape != list {
		return elem, nil
	}
	switch elem {
	case "s":
		return "sh", nil
	case "u64":
		return "u64h", nil
	}
	return "", eb.Build().Str("section", c.section).Errorf("trailviews: no list type for section")
}

// kindView is a per-kind data mart: the kind membership that selects its
// rows and the attributes it projects beside the shared context columns.
type kindView struct {
	name    string
	kind    registry.RegisteredNaturalKey
	columns []column
}

// The handles the authored SQL names directly. Everything else is an LW_
// call, which resolves its own lanes.
const (
	hId         = "`id:id`"
	hNaturalKey = "`id:naturalKey`"
	hTs         = "`timestamp:ts`"
	// hSymLr is the symbol section's membership lane, named so the kind
	// test is a has(): a literal membership test is what the skip index
	// prunes on, and no LW_ call is.
	hSymLr = "`symbol:lr`"
)

// The backbone columns a data mart passes through, by the placeholder name
// a derived view reads them as, and the prefix of their physical name in
// the facts schema.
var backbone = []struct {
	name   string
	prefix string
}{
	{"id", "id:id:"},
	{"ts", "ts:ts:"},
	{"natural-key", "id:naturalKey:"},
}

// plainChannel is the one membership channel every trail attribute rides:
// the generated emitters add each through AddMembershipLowCardRef.
const plainChannel = "chan:low-card-ref"

// contextColumns are the identifiers every trail row may carry
// (ADR-0277 §SD1): Origin always, Conversation and Delegation when the
// writer stated them. Absent components read as type defaults.
var contextColumns = []column{
	{"run", "symbol", vocab.MembRuntimeRun, scalar},
	{"app", "symbol", vocab.MembRuntimeApp, scalar},
	{"instance", "u64Array", vocab.MembLifecycleTileKey, first},
	{"conversation", "stringArray", vocab.MembTrailConversation, first},
	{"turn", "stringArray", vocab.MembTrailTurn, first},
	{"round", "u32Array", vocab.MembTrailRound, first},
	{"task", "stringArray", vocab.MembTrailTask, first},
	{"task-epoch", "u64Array", vocab.MembTrailTaskEpoch, first},
	{"task-call", "stringArray", vocab.MembTrailCall, first},
}

// causeColumns are the Cause component (ADR-0277 §SD1): the model call and
// tool call a coordinator says asked for the row. Stated, not verified.
var causeColumns = []column{
	{"cause-model-call", "stringArray", vocab.MembTrailCauseModelCall, first},
	{"cause-tool-call", "stringArray", vocab.MembTrailCauseToolCall, first},
	{"cause-tool-index", "u32Array", vocab.MembTrailCauseToolIndex, first},
}

// kindViews are the per-kind data marts, in the order they are created.
// The columns follow each DTO in trail/*_dto.go: the section is the one
// the DTO's tag names, `unit` fields read as first, the rest as list.
var kindViews = []kindView{
	{name: ViewModelCalls, kind: vocab.MembKindLlmCall, columns: []column{
		{"call-id", "stringArray", vocab.MembLlmCallId, first},
		{"parent", "stringArray", vocab.MembLlmCallParent, first},
		{"purpose", "symbol", vocab.MembLlmCallPurpose, scalar},
		{"sensitivity", "symbol", vocab.MembLlmCallSensitivity, scalar},
		{"model", "symbol", vocab.MembLlmCallModel, scalar},
		{"endpoint-host", "symbol", vocab.MembLlmCallEndpointHost, scalar},
		{"reported-model", "symbol", vocab.MembLlmCallReportedModel, scalar},
		{"provider-id", "stringArray", vocab.MembLlmCallProviderId, first},
		{"messages", "u32Array", vocab.MembLlmCallMessages, first},
		{"tools", "u32Array", vocab.MembLlmCallTools, first},
		{"tools-digest", "stringArray", vocab.MembLlmCallToolsDigest, first},
		{"max-tokens", "u32Array", vocab.MembLlmCallMaxTokens, first},
		{"prompt-bytes", "u64Array", vocab.MembLlmCallPromptBytes, first},
		{"completion-bytes", "u64Array", vocab.MembLlmCallCompletionBytes, first},
		{"input-tokens", "u32Array", vocab.MembLlmCallInputTokens, first},
		{"output-tokens", "u32Array", vocab.MembLlmCallOutputTokens, first},
		{"tool-calls", "u32Array", vocab.MembLlmCallToolCalls, first},
		{"finish-reason", "symbol", vocab.MembLlmCallFinishReason, scalar},
		{"elapsed-ms", "u64Array", vocab.MembLlmCallElapsedMs, first},
		{"incomplete", "bool", vocab.MembLlmCallIncomplete, scalar},
		{"refused", "bool", vocab.MembLlmCallRefused, scalar},
		{"error", "stringArray", vocab.MembLlmCallError, list},
		{"retention", "symbol", vocab.MembLlmCallRetention, scalar},
		{"retained-from", "u32Array", vocab.MembLlmCallRetainedFrom, first},
		{"history-hash", "stringArray", vocab.MembLlmCallHistoryHash, first},
	}},
	{name: ViewModelMessages, kind: vocab.MembKindLlmMessage, columns: []column{
		{"call-id", "stringArray", vocab.MembLlmMessageCallId, first},
		{"ordinal", "u32Array", vocab.MembLlmMessageOrdinal, first},
		{"role", "symbol", vocab.MembLlmMessageRole, scalar},
		{"sensitivity", "symbol", vocab.MembLlmMessageSensitivity, scalar},
		{"bytes", "u64Array", vocab.MembLlmMessageBytes, first},
		{"digest", "stringArray", vocab.MembLlmMessageDigest, first},
		{"tool-call-id", "stringArray", vocab.MembLlmMessageToolCallId, first},
		{"tool-call-ids", "stringArray", vocab.MembLlmMessageToolCallIds, list},
		{"tool-names", "symbolArray", vocab.MembLlmMessageToolNames, list},
		{"images", "stringArray", vocab.MembLlmMessageImages, list},
		// The body (ADR-0264): present only on a kept conversation's rows.
		{"content", "textArray", vocab.MembLlmMessageContent, first},
		{"reasoning", "textArray", vocab.MembLlmMessageReasoning, first},
		{"tool-calls-json", "textArray", vocab.MembLlmMessageToolCalls, list},
	}},
	{name: ViewAgentActions, kind: vocab.MembKindAgentAction, columns: []column{
		{"key", "stringArray", vocab.MembAgentActionKey, first},
		{"target-instance", "u64Array", vocab.MembAgentActionInstance, first},
		{"target-app", "symbol", vocab.MembAgentActionApp, scalar},
		{"operation", "symbol", vocab.MembAgentActionOperation, scalar},
		{"effect", "symbol", vocab.MembAgentActionEffect, scalar},
		{"args-digest", "stringArray", vocab.MembAgentActionArgsDigest, first},
		{"decision", "symbol", vocab.MembAgentActionDecision, scalar},
		{"phase", "symbol", vocab.MembAgentActionPhase, scalar},
		{"reason", "stringArray", vocab.MembAgentActionReason, list},
		{"call-title", "stringArray", vocab.MembAgentActionCallTitle, list},
		{"call-reason", "stringArray", vocab.MembAgentActionCallReason, list},
		{"budget-left", "u32Array", vocab.MembAgentActionBudgetLeft, first},
		{"test", "bool", vocab.MembAgentActionTest, scalar},
		{"tainted", "bool", vocab.MembAgentActionTainted, scalar},
		{"confined", "bool", vocab.MembAgentActionConfined, scalar},
	}},
	{name: ViewAgentGrants, kind: vocab.MembKindAgentGrant, columns: []column{
		{"event", "symbol", vocab.MembAgentGrantEvent, scalar},
		{"plan", "stringArray", vocab.MembAgentGrantPlan, first},
		{"plan-digest", "stringArray", vocab.MembAgentGrantPlanDigest, first},
		{"entries", "stringArray", vocab.MembAgentGrantEntries, list},
		{"launches", "stringArray", vocab.MembAgentGrantLaunches, list},
		{"destinations", "stringArray", vocab.MembAgentGrantDestinations, list},
		{"calls-budget", "u32Array", vocab.MembAgentGrantCallsBudget, first},
		{"deadline-ms", "i64Array", vocab.MembAgentGrantDeadlineMs, first},
		{"decided-by", "symbol", vocab.MembAgentGrantDecidedBy, scalar},
		{"reason", "stringArray", vocab.MembAgentGrantReason, list},
	}},
	{name: ViewAgentCaptures, kind: vocab.MembKindAgentCapture, columns: []column{
		{"format", "symbol", vocab.MembAgentCaptureFormat, scalar},
		{"windows", "u64Array", vocab.MembAgentCaptureWindows, list},
		{"decision", "symbol", vocab.MembAgentCaptureDecision, scalar},
		{"policy", "symbol", vocab.MembAgentCapturePolicy, scalar},
		{"obligations", "stringArray", vocab.MembAgentCaptureObligations, list},
		{"spans-digest", "stringArray", vocab.MembAgentCaptureSpansDigest, first},
		{"digest", "stringArray", vocab.MembAgentCaptureDigest, first},
		{"bytes", "u64Array", vocab.MembAgentCaptureBytes, first},
		{"phase", "symbol", vocab.MembAgentCapturePhase, scalar},
		{"reason", "stringArray", vocab.MembAgentCaptureReason, list},
		{"confined", "bool", vocab.MembAgentCaptureConfined, scalar},
	}},
	{name: ViewAgentDisclosures, kind: vocab.MembKindAgentDisclosure, columns: []column{
		{"image", "stringArray", vocab.MembAgentDisclosureImage, first},
		{"digest", "stringArray", vocab.MembAgentDisclosureDigest, first},
		{"root-digest", "stringArray", vocab.MembAgentDisclosureRootDigest, first},
		{"source", "symbol", vocab.MembAgentDisclosureSource, scalar},
		{"width", "u32Array", vocab.MembAgentDisclosureWidth, first},
		{"height", "u32Array", vocab.MembAgentDisclosureHeight, first},
		{"bytes", "u64Array", vocab.MembAgentDisclosureBytes, first},
		{"level", "symbol", vocab.MembAgentDisclosureLevel, scalar},
		{"local-only", "bool", vocab.MembAgentDisclosureLocalOnly, scalar},
		{"decision", "symbol", vocab.MembAgentDisclosureDecision, scalar},
		{"decided-by", "symbol", vocab.MembAgentDisclosureDecidedBy, scalar},
		{"endpoint", "stringArray", vocab.MembAgentDisclosureEndpoint, list},
		{"reason", "stringArray", vocab.MembAgentDisclosureReason, list},
	}},
	{name: ViewHttpFetches, kind: vocab.MembKindHttpFetch, columns: []column{
		{"destination", "symbol", vocab.MembHttpFetchDestination, scalar},
		{"purpose", "symbol", vocab.MembHttpFetchPurpose, scalar},
		{"sensitivity", "symbol", vocab.MembHttpFetchSensitivity, scalar},
		{"method", "symbol", vocab.MembHttpFetchMethod, scalar},
		{"url", "stringArray", vocab.MembHttpFetchUrl, first},
		{"status", "u32Array", vocab.MembHttpFetchStatus, first},
		{"bytes", "u64Array", vocab.MembHttpFetchBytes, first},
		{"elapsed-ms", "u64Array", vocab.MembHttpFetchElapsedMs, first},
		{"refused", "bool", vocab.MembHttpFetchRefused, scalar},
		{"error", "stringArray", vocab.MembHttpFetchError, list},
	}},
	{name: ViewAdhocBundles, kind: vocab.MembKindAdhocDataset, columns: []column{
		{"operation", "symbol", vocab.MembAdhocDatasetOperation, scalar},
		{"outcome", "symbol", vocab.MembAdhocDatasetOutcome, scalar},
		{"reason", "stringArray", vocab.MembAdhocDatasetReason, list},
		{"bundle", "symbol", vocab.MembAdhocDatasetBundle, scalar},
		{"revision", "u64Array", vocab.MembAdhocDatasetRevision, first},
		{"owner-app", "symbol", vocab.MembAdhocDatasetOwnerApp, scalar},
		{"owner-instance", "u64Array", vocab.MembAdhocDatasetOwnerInstance, first},
		{"local-names", "stringArray", vocab.MembAdhocDatasetLocalNames, list},
		{"aliases", "stringArray", vocab.MembAdhocDatasetAliases, list},
		{"handles", "stringArray", vocab.MembAdhocDatasetHandles, list},
		{"rows", "u64Array", vocab.MembAdhocDatasetRows, list},
		{"bytes", "u64Array", vocab.MembAdhocDatasetBytes, list},
		{"stream-digests", "stringArray", vocab.MembAdhocDatasetStreamDigests, list},
		{"document-digest", "stringArray", vocab.MembAdhocDatasetDocumentDigest, first},
		{"attested", "bool", vocab.MembAdhocDatasetAttested, scalar},
	}},
}

// withCause names the kinds whose rows may carry a Cause component.
var withCause = map[string]bool{
	ViewAgentActions:     true,
	ViewAgentGrants:      true,
	ViewAgentDisclosures: true,
	ViewAdhocBundles:     true,
}

// composer carries what one composition needs: the database the views go
// into, the facts table they read, the minter, and the schemas of the
// views composed so far, which later views read through placeholders.
type composer struct {
	database string
	table    string
	mint     minter
	schemas  map[string][]Column
}

// Compose returns every view over database.table, in creation order: the
// per-kind data marts first, then the timeline over them, then the
// aggregates over those. Empty arguments select the facts table's defaults
// (factsschema). The views are created in the table's database under fixed
// names, so one database holds the views of one facts table.
//
// Each statement is CREATE OR REPLACE and stamped (see [Stamp]): posting
// the list again re-expands every view against the functions installed
// now, which is the whole refresh. The read surface (lwsqlsurface) must be
// installed before, and the facts table must exist.
func Compose(database string, table string) (views []View, err error) {
	if database == "" {
		database = factsschema.DatabaseName
	}
	if table == "" {
		table = factsschema.TableName
	}
	m, err := newMinter()
	if err != nil {
		return nil, err
	}
	inst := &composer{database: database, table: database + "." + table, mint: m, schemas: map[string][]Column{}}
	for _, kv := range kindViews {
		var v View
		v, err = inst.kindView(kv)
		if err != nil {
			return nil, eb.Build().Str("view", kv.name).Errorf("trailviews: %w", err)
		}
		views = append(views, v)
	}
	for _, d := range []struct {
		name    string
		sources []string
		outputs []output
		body    string
	}{
		{ViewTimeline, kindViewNames(), nil, ""},
		{ViewActionOutcomes, []string{ViewAgentActions}, outcomesOutputs, outcomesBody},
		{ViewConversations, []string{ViewTimeline, ViewActionOutcomes}, conversationsOutputs, conversationsBody},
		{ViewTasks, []string{ViewTimeline, ViewActionOutcomes}, tasksOutputs, tasksBody},
	} {
		var v View
		if d.name == ViewTimeline {
			v, err = inst.timeline()
		} else {
			v, err = inst.derived(d.name, d.sources, d.outputs, d.body)
		}
		if err != nil {
			return nil, eb.Build().Str("view", d.name).Errorf("trailviews: %w", err)
		}
		views = append(views, v)
	}
	return views, nil
}

// Statements is [Compose] as bare statements, for an executor that posts
// one at a time.
func Statements(database string, table string) (stmts []string, err error) {
	views, err := Compose(database, table)
	if err != nil {
		return nil, err
	}
	stmts = make([]string, 0, len(views))
	for _, v := range views {
		stmts = append(stmts, v.Sql)
	}
	return stmts, nil
}

func kindViewNames() (names []string) {
	for _, kv := range kindViews {
		names = append(names, kv.name)
	}
	return names
}

// createView wraps an expanded SELECT and stamps it. The SELECT is what was
// expanded; the DDL around it is not part of the grammar the passes read.
func (inst *composer) createView(name string, sel string) (sql string) {
	return fmt.Sprintf("CREATE OR REPLACE VIEW %s.%s AS\n%s\nCOMMENT %s", inst.database, name, sel, Stamp().Literal())
}

// backboneColumns are the facts backbone a data mart passes through, under
// the facts table's physical names.
func backboneColumns() (cols []Column, err error) {
	names := factsColumnNames()
	for _, b := range backbone {
		found := false
		for _, n := range names {
			if strings.HasPrefix(n, b.prefix) {
				cols = append(cols, Column{Name: b.name, Physical: n, Backbone: true})
				found = true
				break
			}
		}
		if !found {
			return nil, eb.Build().Str("column", b.prefix).Errorf("trailviews: backbone column not in the facts schema")
		}
	}
	return cols, nil
}

// kindView composes one per-kind data mart: authored with handles, LW_GET
// and LW_PLAIN, then expanded against the facts schema.
func (inst *composer) kindView(kv kindView) (v View, err error) {
	cols := make([]column, 0, len(contextColumns)+len(causeColumns)+len(kv.columns))
	cols = append(cols, contextColumns...)
	if withCause[kv.name] {
		cols = append(cols, causeColumns...)
	}
	cols = append(cols, kv.columns...)

	schema, err := backboneColumns()
	if err != nil {
		return View{}, err
	}
	b := strings.Builder{}
	fmt.Fprintf(&b, "SELECT\n  %s,\n  %s,\n  %s", hId, hTs, hNaturalKey)
	for _, c := range cols {
		var ctype, minted string
		ctype, err = c.canonicalType()
		if err != nil {
			return View{}, eb.Build().Str("column", c.name).Errorf("%w", err)
		}
		minted, err = output{expr: read(c), name: c.name, ctype: ctype}.mint()
		if err != nil {
			return View{}, err
		}
		fmt.Fprintf(&b, ",\n  %s", minted)
		var col Column
		col, err = inst.mint.column(c.name, ctype)
		if err != nil {
			return View{}, err
		}
		schema = append(schema, col)
	}
	// The kind test stays an id: has() over the membership lane takes a
	// literal, and it is the term the skip index prunes on.
	fmt.Fprintf(&b, "\nFROM %s\nWHERE has(%s, %d)", inst.table, hSymLr, kv.kind.GetId().Value())

	sel, err := prepare(b.String(), inst.table)
	if err != nil {
		return View{}, err
	}
	inst.schemas[kv.name] = schema
	return View{Name: kv.name, Sql: inst.createView(kv.name, sel), Columns: schema}, nil
}

// membName is the spelling a membership is named by in an LW_ call: the
// registry's own folded natural key, which the extraction pass resolves
// back to the id before the statement ships (ADR-0171 §SD4).
func membName(m registry.RegisteredNaturalKey) (name string) {
	return string(m.GetNaturalKey())
}

// read renders one column's read expression, authored.
func read(c column) (expr string) {
	switch c.shape {
	case scalar:
		return fmt.Sprintf("LW_GET('%s', '%s', '%s')", c.section, membName(c.memb), plainChannel)
	case first:
		return fmt.Sprintf("arrayElement(LW_GET_LIST('%s', '%s', '%s'), 1)", c.section, membName(c.memb), plainChannel)
	default:
		return fmt.Sprintf("LW_GET_LIST('%s', '%s', '%s')", c.section, membName(c.memb), plainChannel)
	}
}

// membershipIds resolves the names the views use, over the runtime
// vocabulary — the one vocabulary the trail writes.
type membershipIds struct{}

// LookupMembership is constructsql.MembershipIdsI.
func (membershipIds) LookupMembership(name string) (id uint64, err error) {
	e, err := vocab.NkRegistry.Lookup(naming.StylableName(name))
	if err != nil {
		return 0, eb.Build().Str("name", name).Errorf("trailviews: membership is not in the runtime vocabulary: %w", err)
	}
	return e.GetId().Value(), nil
}

// prepare rewrites an authored per-kind SELECT into the one a view stores:
// handles to physical names, LW_GET calls to the read-back expressions,
// LW_PLAIN calls to minted aliases. All resolve against the trail store's
// generated schema, so a misspelled handle or section fails here rather
// than on the server.
//
// Handles first: the later passes emit physical names, which contain
// colons, and the handle resolver must not read one as a handle.
func prepare(sql string, table string) (out string, err error) {
	database, _, qualified := strings.Cut(table, ".")
	if !qualified || database == "" {
		return "", eb.Build().Str("table", table).Errorf("trailviews: table is not database-qualified")
	}
	resolver := lwsql.NewResolver(passes.NewStaticSchemaProvider(
		map[string][]string{table: factsColumnNames()}))
	return applyPasses(sql,
		passes.ResolveColumnNames(resolver, database, nil),
		constructsql.ExtractExpandPassWithIds(resolver, membershipIds{}, database),
		constructsql.ExpandPass,
	)
}

func applyPasses(sql string, ps ...nanopass.Pass) (out string, err error) {
	out = sql
	for _, pass := range ps {
		out, err = pass.Apply(env.NewEnvironment(), out)
		if err != nil {
			return "", eb.Build().Str("pass", pass.Name).Errorf("trailviews: pass failed: %w", err)
		}
	}
	return out, nil
}

// factsColumnNames is the facts table's physical column list off the trail
// store's generated Arrow schema — the one its writes go through.
func factsColumnNames() (names []string) {
	fields := lowlevel.CreateSchemaFactsTable().Fields()
	names = make([]string, 0, len(fields))
	for i := range fields {
		names = append(names, fields[i].Name)
	}
	return names
}

// placeholder is `{name}`: a column of a source view, by logical name.
var placeholder = regexp.MustCompile(`\{([a-z0-9-]+)\}`)

// resolve replaces each placeholder in sql with the quoted physical name the
// source views selected that column as. A name must mean one column across
// the sources: two sources spelling it with different types is an error,
// as is a name no source has.
func (inst *composer) resolve(sql string, sources []string) (out string, err error) {
	byName := map[string]Column{}
	for _, s := range sources {
		for _, c := range inst.schemas[s] {
			if prev, ok := byName[c.Name]; ok && prev.Physical != c.Physical {
				return "", eb.Build().Str("column", c.Name).Str("view", s).Errorf("trailviews: column means two things across the sources")
			}
			byName[c.Name] = c
		}
	}
	out = placeholder.ReplaceAllStringFunc(sql, func(m string) string {
		name := m[1 : len(m)-1]
		c, ok := byName[name]
		if !ok {
			if err == nil {
				err = eb.Build().Str("column", name).Errorf("trailviews: no source view has the column")
			}
			return m
		}
		return `"` + c.Physical + `"`
	})
	return out, err
}

// projection renders outputs as minted projection items and records the
// schema they produce.
func (inst *composer) projection(outputs []output) (items []string, schema []Column, err error) {
	for _, o := range outputs {
		var item string
		item, err = o.mint()
		if err != nil {
			return nil, nil, err
		}
		var col Column
		col, err = inst.mint.column(o.name, o.ctype)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		schema = append(schema, col)
	}
	return items, schema, nil
}

// derived composes an aggregate: body is a SELECT whose projection is the
// `%[1]s` verb, reading its sources through placeholders.
func (inst *composer) derived(name string, sources []string, outputs []output, body string) (v View, err error) {
	items, schema, err := inst.projection(outputs)
	if err != nil {
		return View{}, err
	}
	sql := fmt.Sprintf(body, "  "+strings.Join(items, ",\n  "), inst.database)
	sql, err = inst.resolve(sql, sources)
	if err != nil {
		return View{}, err
	}
	sql, err = applyPasses(sql, constructsql.ExpandPass)
	if err != nil {
		return View{}, err
	}
	inst.schemas[name] = schema
	return View{Name: name, Sql: inst.createView(name, sql), Columns: schema}, nil
}

// headlineChars bounds the text a headline quotes, so a timeline row stays
// one scannable line; the full text is in the `text` column.
const headlineChars = 160

// timelineContext are the columns every timeline branch passes through
// unchanged from its per-kind view: the facts backbone and the context.
var timelineContext = []string{"id", "ts", "natural-key", "run", "app", "instance", "conversation", "turn", "round", "task", "task-epoch", "task-call"}

// timelineBranch is one kind's contribution to the timeline: the view it
// reads and the shared outputs computed from that view's columns.
type timelineBranch struct {
	view string
	kind string
	// ref, seq, parentRef, status, subject, headline, text, confined,
	// tainted, tokensIn, tokensOut, elapsedMs — in that order.
	exprs [12]string
}

// timelineShape names and types the computed timeline columns, in the
// order every branch's exprs follows.
var timelineShape = []struct{ name, ctype string }{
	{"ref", "s"}, {"seq", "u32"}, {"parent-ref", "s"}, {"status", "s"}, {"subject", "s"},
	{"headline", "s"}, {"text", "s"}, {"confined", "b"}, {"tainted", "b"},
	{"tokens-in", "u64"}, {"tokens-out", "u64"}, {"elapsed-ms", "u64"},
}

// timelineBranches are the per-kind branches. `headline` is a one-line
// summary built from the row's own columns; `text` the longer text where
// the kind has one (a message body, a grant plan, a call's title);
// `subject` what the event is about (a purpose, an app.operation, a
// destination); `ref` the row's own identifier and `parent-ref` what it
// hangs off, so a reader can walk from an action to the model call that
// asked for it. Order by (ts, seq): a call's messages share its timestamp
// and seq is their ordinal.
var timelineBranches = []timelineBranch{
	{ViewModelCalls, "llmCall", [12]string{
		"{call-id}", "{round}", "{parent}",
		"if({refused}, 'refused', if({incomplete}, 'incomplete', {finish-reason}))",
		"{purpose}",
		"concat('model call ', {purpose}, ' -> ', {model}, ' (', toString({input-tokens}), ' in / ', toString({output-tokens}), ' out, ', " +
			"toString({tool-calls}), ' tool calls, ', toString({elapsed-ms}), ' ms, ', {finish-reason}, " +
			"if({refused}, ', REFUSED', ''), if({incomplete}, ', INCOMPLETE', ''), ')')",
		"arrayStringConcat({error}, ' | ')",
		"{sensitivity} = 'confined'", "false", "{input-tokens}", "{output-tokens}", "{elapsed-ms}",
	}},
	{ViewModelMessages, "llmMessage", [12]string{
		"concat({call-id}, '#', toString({ordinal}))", "{ordinal}", "{call-id}", "{role}", "{role}",
		fmt.Sprintf("concat({role}, ' message #', toString({ordinal}), "+
			"if(length({tool-names}) > 0, concat(' [', arrayStringConcat({tool-names}, ', '), ']'), ''), "+
			"if({content} != '', concat(': ', substringUTF8(replaceAll({content}, '\\n', ' '), 1, %d)), "+
			"concat(' (', toString({bytes}), ' bytes, text not kept)')))", headlineChars),
		"{content}", "{sensitivity} = 'confined'", "false", "0", "0", "0",
	}},
	{ViewAgentActions, "agentAction", [12]string{
		"{key}", "0", "{cause-model-call}", "{phase}", "concat({target-app}, '.', {operation})",
		"concat({operation}, ' on ', {target-app}, '#', toString({target-instance}), ' [', {effect}, '] ', {decision}, ' -> ', {phase}, " +
			"if(length({call-title}) > 0, concat(': ', arrayStringConcat({call-title}, ' ')), ''), " +
			"if(length({reason}) > 0, concat(' (', arrayStringConcat({reason}, ' | '), ')'), ''))",
		"arrayStringConcat(arrayConcat({call-title}, {call-reason}), '\\n')",
		"{confined}", "{tainted}", "0", "0", "0",
	}},
	{ViewAgentGrants, "agentGrant", [12]string{
		"{plan-digest}", "0", "{cause-model-call}", "{event}", "{event}",
		fmt.Sprintf("concat('grant ', {event}, if({decided-by} != '', concat(' by ', {decided-by}), ''), "+
			"if({plan} != '', concat(': ', substringUTF8(replaceAll({plan}, '\\n', ' '), 1, %d)), ''), "+
			"if(length({destinations}) > 0, concat(' [', arrayStringConcat({destinations}, ', '), ']'), ''))", headlineChars),
		"{plan}", "false", "false", "0", "0", "0",
	}},
	{ViewAgentCaptures, "agentCapture", [12]string{
		"{digest}", "0", "''", "{phase}", "{format}",
		"concat('capture ', {format}, ' of ', toString(length({windows})), ' window(s) ', {decision}, ' -> ', {phase}, " +
			"if(length({reason}) > 0, concat(' (', arrayStringConcat({reason}, ' | '), ')'), ''))",
		"''", "{confined}", "true", "0", "0", "0",
	}},
	{ViewAgentDisclosures, "agentDisclosure", [12]string{
		"{digest}", "0", "{cause-model-call}", "{decision}", "{source}",
		"concat('disclosure of ', {source}, ' image at level ', {level}, ' ', {decision}, " +
			"if({decided-by} != '', concat(' by ', {decided-by}), ''), " +
			"if(length({endpoint}) > 0, concat(' to ', arrayStringConcat({endpoint}, ', ')), ''))",
		"arrayStringConcat({reason}, ' | ')", "{local-only}", "false", "0", "0", "0",
	}},
	{ViewHttpFetches, "httpFetch", [12]string{
		"{url}", "0", "''", "if({refused}, 'refused', toString({status}))", "{destination}",
		"concat({method}, ' ', {destination}, ' ', {url}, ' -> ', if({refused}, 'refused', toString({status})), " +
			"' (', toString({bytes}), ' bytes, ', toString({elapsed-ms}), ' ms)')",
		"arrayStringConcat({error}, ' | ')", "{sensitivity} = 'confined'", "false", "0", "0", "{elapsed-ms}",
	}},
	{ViewAdhocBundles, "adhocDataset", [12]string{
		"{document-digest}", "0", "{cause-model-call}", "{outcome}", "{bundle}",
		"concat('bundle ', {bundle}, ' ', {operation}, ' r', toString({revision}), ' -> ', {outcome}, " +
			"if(length({local-names}) > 0, concat(' [', arrayStringConcat({local-names}, ', '), ']'), ''), " +
			"if({attested}, ' (attested)', ''), " +
			"if(length({reason}) > 0, concat(' (', arrayStringConcat({reason}, ' | '), ')'), ''))",
		"arrayStringConcat({reason}, ' | ')", "false", "false", "0", "0", "0",
	}},
}

// timeline composes the unified timeline: one row per trail event of any
// kind, in a shared shape — a data mart over all of them, since the
// per-kind sets are disjoint and each keeps its facts row's backbone.
//
// The `kind` column carries the trail's own kind label (llmCall,
// agentAction, …), not a view name.
func (inst *composer) timeline() (v View, err error) {
	var branches []string
	var schema []Column
	for i, br := range timelineBranches {
		outputs := []output{{"'" + br.kind + "'", "kind", "s"}}
		for j, shape := range timelineShape {
			outputs = append(outputs, output{br.exprs[j], shape.name, shape.ctype})
		}
		items, s, perr := inst.projection(outputs)
		if perr != nil {
			return View{}, perr
		}
		ctx := make([]string, 0, len(timelineContext))
		for _, c := range timelineContext {
			ctx = append(ctx, "{"+c+"}")
		}
		sel := fmt.Sprintf("SELECT\n  %s,\n  %s\nFROM %s.%s", strings.Join(ctx, ", "), strings.Join(items, ",\n  "), inst.database, br.view)
		sel, err = inst.resolve(sel, []string{br.view})
		if err != nil {
			return View{}, eb.Build().Str("branch", br.view).Errorf("%w", err)
		}
		branches = append(branches, sel)
		if i == 0 {
			for _, c := range timelineContext {
				for _, sc := range inst.schemas[br.view] {
					if sc.Name == c {
						schema = append(schema, sc)
					}
				}
			}
			schema = append(schema, s...)
		}
	}
	sql, err := applyPasses(strings.Join(branches, "\nUNION ALL\n"), constructsql.ExpandPass)
	if err != nil {
		return View{}, err
	}
	inst.schemas[ViewTimeline] = schema
	return View{Name: ViewTimeline, Sql: inst.createView(ViewTimeline, sql), Columns: schema}, nil
}

// The terminal phases of a dispatched operation (opwire.PhaseE), grouped
// by what they mean to a reader: it happened, it is waiting on someone, or
// it did not happen. accepted and running are in flight and in none.
const (
	phasesDone    = "('applied', 'rendered', 'completed')"
	phasesWaiting = "('proposed', 'input_required')"
	phasesNotDone = "('denied', 'refused', 'rejected', 'stale', 'conflict', 'expired', 'failed', 'cancelled')"
)

// actionKey is the outcome grouping key. Rows written before ADR-0277
// carry no dispatcher key (it rode a per-kind membership retired with
// them); each such row stands alone, keyed `row:<id>`, rather than all of
// them collapsing into one.
const actionKey = "if({key} = '', concat('row:', toString({id})), {key})"

// The action outcomes aggregate: one row per agent action — per dispatcher
// key — rather than per trail row. The dispatcher writes a row when it
// decides and another when the call reaches its final phase (a call
// refused at dispatch writes only the first), so counting timeline rows
// counts most actions twice; this aggregate is what the digests count.
var outcomesOutputs = []output{
	{actionKey, "action-key", "s"},
	{"any({run})", "run", "s"},
	{"any({app})", "app", "s"},
	{"any({instance})", "instance", "u64"},
	{"anyIf({conversation}, {conversation} != '')", "conversation", "s"},
	{"anyIf({turn}, {turn} != '')", "turn", "s"},
	{"anyIf({task}, {task} != '')", "task", "s"},
	{"max({task-epoch})", "task-epoch", "u64"},
	{"anyIf({task-call}, {task-call} != '')", "task-call", "s"},
	{"anyIf({cause-model-call}, {cause-model-call} != '')", "cause-model-call", "s"},
	{"any({target-app})", "target-app", "s"},
	{"any({target-instance})", "target-instance", "u64"},
	{"any({operation})", "operation", "s"},
	{"any({effect})", "effect", "s"},
	{"min({ts})", "first-ts", "z64"},
	{"max({ts})", "last-ts", "z64"},
	{"arrayMap(e -> e.2, arraySort(groupArray(({ts}, {phase}))))", "phases", "sh"},
	{"argMax({phase}, {ts})", "outcome", "s"},
	{"argMax({phase}, {ts}) IN " + phasesDone, "done", "b"},
	{"argMax({phase}, {ts}) IN " + phasesWaiting, "waiting", "b"},
	{"argMax({phase}, {ts}) IN " + phasesNotDone, "not-done", "b"},
	{"argMaxIf({reason}, {ts}, length({reason}) > 0)", "reason", "sh"},
	{"argMinIf({call-title}, {ts}, length({call-title}) > 0)", "call-title", "sh"},
	{"argMinIf({call-reason}, {ts}, length({call-reason}) > 0)", "call-reason", "sh"},
	{"min({budget-left})", "budget-left", "u32"},
	{"max({test})", "test", "b"},
	{"max({tainted})", "tainted", "b"},
	{"max({confined})", "confined", "b"},
	{"groupArray({id})", "facts-ids", "u64h"},
}

const outcomesBody = `SELECT
%[1]s
FROM %[2]s.` + ViewAgentActions + `
GROUP BY ` + actionKey

// The conversations aggregate: one row per conversation — its span, what
// ran in it, how its agent actions ended, and its opening question and
// latest answer where the text was kept (ADR-0264).
var conversationsOutputs = []output{
	{"t.conversation", "conversation", "s"},
	{"t.first_ts", "first-ts", "z64"},
	{"t.last_ts", "last-ts", "z64"},
	{"dateDiff('second', t.first_ts, t.last_ts)", "span-s", "i64"},
	{"t.apps", "apps", "sh"},
	{"t.turns", "turns", "u64"},
	{"t.model_calls", "model-call-count", "u64"},
	{"t.calls_not_done", "model-calls-not-done", "u64"},
	{"t.tokens_in", "tokens-in", "u64"},
	{"t.tokens_out", "tokens-out", "u64"},
	{"t.messages", "message-count", "u64"},
	{"t.messages_kept", "messages-kept", "u64"},
	{"a.actions", "actions", "u64"},
	{"a.actions_done", "actions-done", "u64"},
	{"a.actions_waiting", "actions-waiting", "u64"},
	{"a.actions_not_done", "actions-not-done", "u64"},
	{"a.operations", "operations", "sh"},
	{"t.tasks", "tasks", "sh"},
	{"t.any_confined", "any-confined", "b"},
	{"t.any_tainted", "any-tainted", "b"},
	{"t.first_question", "first-question", "s"},
	{"t.last_answer", "last-answer", "s"},
	{"t.facts_ids", "facts-ids", "u64h"},
}

const conversationsBody = `SELECT
%[1]s
FROM (
  SELECT
    {conversation} AS conversation,
    min({ts}) AS first_ts,
    max({ts}) AS last_ts,
    groupUniqArray({app}) AS apps,
    uniqExactIf({turn}, {turn} != '') AS turns,
    countIf({kind} = 'llmCall') AS model_calls,
    countIf({kind} = 'llmCall' AND {status} IN ('refused', 'incomplete')) AS calls_not_done,
    sum({tokens-in}) AS tokens_in,
    sum({tokens-out}) AS tokens_out,
    countIf({kind} = 'llmMessage') AS messages,
    countIf({kind} = 'llmMessage' AND {text} != '') AS messages_kept,
    groupUniqArrayIf({task}, {task} != '') AS tasks,
    max({confined}) AS any_confined,
    max({tainted}) AS any_tainted,
    argMinIf({text}, ({ts}, {seq}), {kind} = 'llmMessage' AND {status} = 'user' AND {text} != '') AS first_question,
    argMaxIf({text}, ({ts}, {seq}), {kind} = 'llmMessage' AND {status} = 'assistant' AND {text} != '') AS last_answer,
    groupArray({id}) AS facts_ids
  FROM %[2]s.` + ViewTimeline + `
  WHERE {conversation} != ''
  GROUP BY {conversation}
) AS t
LEFT JOIN (
  SELECT
    {conversation} AS conversation,
    count() AS actions,
    countIf({done}) AS actions_done,
    countIf({waiting}) AS actions_waiting,
    countIf({not-done}) AS actions_not_done,
    groupUniqArray(concat({target-app}, '.', {operation})) AS operations
  FROM %[2]s.` + ViewActionOutcomes + `
  WHERE {conversation} != ''
  GROUP BY {conversation}
) AS a ON a.conversation = t.conversation`

// The tasks aggregate: one row per agent task — the plan the person
// approved, how the grant moved, and what the task did with it.
//
// The model calls and tokens it counts are the delegated ones — calls an
// app made on the task's behalf, stamped with its Delegation. The
// coordinator's own calls that drove the task belong to its conversation,
// not to the task; join through `conversations` to the conversations
// aggregate for those.
var tasksOutputs = []output{
	{"t.task", "task", "s"},
	{"t.first_ts", "first-ts", "z64"},
	{"t.last_ts", "last-ts", "z64"},
	{"dateDiff('second', t.first_ts, t.last_ts)", "span-s", "i64"},
	{"t.conversations", "conversations", "sh"},
	{"t.plan", "plan", "s"},
	{"t.grant_events", "grant-events", "sh"},
	{"t.last_grant_event", "last-grant-event", "s"},
	{"has(t.grant_events, 'ended')", "ended", "b"},
	{"a.actions", "actions", "u64"},
	{"a.actions_done", "actions-done", "u64"},
	{"a.actions_waiting", "actions-waiting", "u64"},
	{"a.actions_not_done", "actions-not-done", "u64"},
	{"a.operations", "operations", "sh"},
	{"a.not_done_reasons", "not-done-reasons", "sh"},
	{"t.captures", "captures", "u64"},
	{"t.delegated_model_calls", "delegated-model-calls", "u64"},
	{"t.delegated_tokens_in", "delegated-tokens-in", "u64"},
	{"t.delegated_tokens_out", "delegated-tokens-out", "u64"},
	{"t.any_confined", "any-confined", "b"},
	{"t.any_tainted", "any-tainted", "b"},
	{"t.facts_ids", "facts-ids", "u64h"},
}

const tasksBody = `SELECT
%[1]s
FROM (
  SELECT
    {task} AS task,
    min({ts}) AS first_ts,
    max({ts}) AS last_ts,
    groupUniqArrayIf({conversation}, {conversation} != '') AS conversations,
    argMinIf({text}, {ts}, {kind} = 'agentGrant' AND {text} != '') AS plan,
    arrayMap(e -> e.2, arraySort(groupArrayIf(({ts}, {status}), {kind} = 'agentGrant'))) AS grant_events,
    argMaxIf({status}, {ts}, {kind} = 'agentGrant') AS last_grant_event,
    countIf({kind} = 'agentCapture') AS captures,
    countIf({kind} = 'llmCall') AS delegated_model_calls,
    sum({tokens-in}) AS delegated_tokens_in,
    sum({tokens-out}) AS delegated_tokens_out,
    max({confined}) AS any_confined,
    max({tainted}) AS any_tainted,
    groupArray({id}) AS facts_ids
  FROM %[2]s.` + ViewTimeline + `
  WHERE {task} != ''
  GROUP BY {task}
) AS t
LEFT JOIN (
  SELECT
    {task} AS task,
    count() AS actions,
    countIf({done}) AS actions_done,
    countIf({waiting}) AS actions_waiting,
    countIf({not-done}) AS actions_not_done,
    groupUniqArray(concat({target-app}, '.', {operation})) AS operations,
    groupUniqArrayIf(concat({operation}, ': ', {outcome}, if(length({reason}) > 0, concat(' (', arrayStringConcat({reason}, ' | '), ')'), '')), {not-done}) AS not_done_reasons
  FROM %[2]s.` + ViewActionOutcomes + `
  WHERE {task} != ''
  GROUP BY {task}
) AS a ON a.task = t.task`
