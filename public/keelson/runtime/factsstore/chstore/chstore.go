// Package chstore is the ClickHouse-backed factsstore.FactsStoreI per
// ADR-0026 M2.5c. Writes go through the generated leeway DML builders
// (runtime/factsschema/dml.InEntityFacts) and ship as Arrow IPC via
// chclient.InsertArrow. Reads (RecentLogs, the run-session views, the
// workingset and column-width latest-wins lists) are hand-composed
// leeway-shaped SELECTs against the array-encoded membership columns —
// the code class ADR-0105 D5 replaces with generated stores kind by kind.
// App persist state left this table for its own generated store
// (ADR-0105 D3a); the state verbs went with it.
package chstore

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	factsddl "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	dmlruntime "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
)

// Config carries the connection coordinates + qualified target table.
//
// RunId is the process's run identity (ADR-0191 §SD3), stamped onto every row
// this store writes that does not carry one of its own. It lives here rather
// than on each row DTO because it is process-wide: eight DTOs repeating one
// constant is eight chances for them to disagree, and three of them
// (GrantRow, AuditRow, LogRow) had no field for it at all — which is why
// "limit to this run" was a timestamp range for those kinds, and silently
// admitted a concurrent boxer process. Empty leaves rows unstamped, which is
// what a store built before its host knew the run id gets.
type Config struct {
	URL      string
	User     string
	Password string
	Database string
	Table    string
	RunId    string
}

// Defaults targets the project's localhost CH at boxer.facts per the
// user-confirmed defaults (memory: reference_clickhouse_localhost_defaults).
func Defaults() (c Config) {
	c = Config{
		URL:      "http://localhost:8123/",
		User:     "default",
		Database: factsschema.DatabaseName,
		Table:    factsschema.TableName,
	}
	return
}

// Store is the live-CH FactsStoreI. Each Write* call constructs a fresh
// InEntityFacts builder, encodes one row, and ships it as a single-record
// Arrow IPC batch through chclient.InsertArrow.
type Store struct {
	cfg       Config
	cli       *chclient.Client
	allocator memory.Allocator
	nextId    atomic.Uint64
}

var _ factsstore.FactsStoreI = (*Store)(nil)

// New constructs a Store. Does not connect or create the table — call Ping
// to verify reachability and SetupTable to apply DDL.
func New(cfg Config) (s *Store, err error) {
	if cfg.URL == "" || cfg.Database == "" || cfg.Table == "" {
		err = eh.Errorf("chstore: cfg requires URL + Database + Table")
		return
	}
	s = &Store{
		cfg: cfg,
		cli: chclient.New(chclient.Config{
			URL: cfg.URL, User: cfg.User, Password: cfg.Password,
		}, nil),
		allocator: memory.NewGoAllocator(),
	}
	return
}

// Ping returns nil when the CH server is reachable.
func (inst *Store) Ping(ctx context.Context) (err error) {
	err = inst.cli.Ping(ctx)
	return
}

// defaultEngineClause is the MergeTree clause ComposeSetupSQL applies when
// the caller passes an empty engineClause — the shape every first-run
// initialisation (SetupTable(ctx, "")) uses.
//
// Time-ordered by default: every audit read (RecentLogs, LifecyclesByRun,
// the latest-wins lists) is ORDER BY ts, so a sorted primary key turns those into a
// sparse-index range read instead of the full scan that ORDER BY tuple()
// forced. Retention — TTL / partitioning to bound a forever-growing
// heartbeat table — is intentionally left to the operator's own engine
// clause rather than imposed here, since it deletes data.
const defaultEngineClause = "MergeTree() ORDER BY `ts:ts:z64:47::0:`"

// ComposeSetupSQL returns the exact DDL script SetupTable applies for cfg:
// the CREATE DATABASE + CREATE TABLE statements (separated by ';'), composed
// with no database connection and no side effects. An empty engineClause
// selects defaultEngineClause — the first-run default. SetupTable is defined
// in terms of this function, so a caller that only wants the SQL (the
// `keelsonddl` CLI) emits byte-for-byte what first-run initialisation
// executes, with no risk of drift.
//
// The columns are referenced by their leeway-encoded physical names (e.g.
// "id:id:u64:47::0:") since the table has no logical aliases.
func ComposeSetupSQL(cfg Config, engineClause string) (sql string, err error) {
	if cfg.Database == "" || cfg.Table == "" {
		err = eh.Errorf("chstore: compose setup sql: cfg requires Database + Table")
		return
	}
	if engineClause == "" {
		engineClause = defaultEngineClause
	}
	sql, err = factsddl.ComposeCreateTableSql(engineClause)
	if err != nil {
		err = eh.Errorf("chstore: compose setup sql: %w", err)
		return
	}
	sql = strings.ReplaceAll(sql,
		factsschema.DatabaseName+"."+factsschema.TableName,
		cfg.Database+"."+cfg.Table)
	if cfg.Database != factsschema.DatabaseName {
		sql = strings.ReplaceAll(sql,
			"CREATE DATABASE IF NOT EXISTS "+factsschema.DatabaseName+";",
			"CREATE DATABASE IF NOT EXISTS "+cfg.Database+";")
	}
	return
}

// SetupTable applies the boxer.facts DDL idempotently against the live CH
// connection. engineClause supplies the MergeTree partition / order / TTL
// settings (empty selects defaultEngineClause). The SQL is composed by
// ComposeSetupSQL, so this path and the `keelsonddl` CLI cannot drift.
//
// Idempotent means CREATE TABLE IF NOT EXISTS, which is not the same as
// migrating: an existing table is left exactly as it is, including its column
// names. Physical column names encode the column's aspect bitmask, so a change
// to the leeway aspect vocabularies renames columns in the generated DDL while
// the deployed table keeps the old ones — after which inserts naming the new
// columns fail against it. There is no automatic reconciliation. Recovering
// means ALTER TABLE ... RENAME COLUMN per renamed column, or dropping and
// re-creating the table if the trail is expendable.
func (inst *Store) SetupTable(ctx context.Context, engineClause string) (err error) {
	var ddl string
	ddl, err = ComposeSetupSQL(inst.cfg, engineClause)
	if err != nil {
		return
	}
	for _, stmt := range splitOnSemicolon(ddl) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		err = inst.cli.Exec(ctx, stmt)
		if err != nil {
			err = eh.Errorf("chstore: setup: exec: %w", err)
			return
		}
	}
	return
}

func (inst *Store) qualifiedTable() string {
	return inst.cfg.Database + "." + inst.cfg.Table
}

// WriteGrant lands one boxer.facts row tagged KindGrant.
func (inst *Store) WriteGrant(row factsstore.GrantRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyFor("grant", row.AppId, []byte(row.Pattern), []byte(row.Direction.String()))
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)
	if !row.ExpiresAt.IsZero() {
		ent.SetLifecycle(row.ExpiresAt)
	}
	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("grant").AddMembershipLowCardRef(vocab.MembKindGrant.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(row.AppId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(row.AppId)).EndAttribute()
	sym.BeginAttribute(row.Pattern).AddMembershipLowCardRef(vocab.MembGrantSubjectPattern.GetId().Value()).EndAttribute()
	sym.BeginAttribute(row.Direction.String()).AddMembershipLowCardRef(vocab.MembGrantDirection.GetId().Value()).EndAttribute()
	grantedVia := row.GrantedVia
	if grantedVia == "" {
		grantedVia = "policy"
	}
	sym.BeginAttribute(grantedVia).AddMembershipLowCardRef(vocab.MembGrantedVia.GetId().Value()).EndAttribute()
	inst.stampRun(sym, "")
	sym.EndSection()
	if row.Reason != "" {
		str := ent.GetSectionStringArray()
		str.BeginAttributeSingle(row.Reason).AddMembershipLowCardRef(vocab.MembGrantReason.GetId().Value()).EndAttribute()
		str.EndSection()
	}
	u64 := ent.GetSectionU64Array()
	stampInstance(u64, row.InstanceKey)
	u64.EndSection()
	bsec := ent.GetSectionBool()
	bsec.BeginAttribute(row.Sticky).AddMembershipLowCardRef(vocab.MembGrantSticky.GetId().Value()).EndAttribute()
	bsec.EndSection()
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteAudit lands one boxer.facts row tagged KindAudit.
func (inst *Store) WriteAudit(row factsstore.AuditRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyFor("audit", row.AppId, []byte(row.Subject), []byte(row.Result))
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)
	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("audit").AddMembershipLowCardRef(vocab.MembKindAudit.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(row.AppId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(row.AppId)).EndAttribute()
	sym.BeginAttribute(row.Subject).AddMembershipLowCardRef(vocab.MembAuditRequestSubject.GetId().Value()).EndAttribute()
	if row.Result != "" {
		sym.BeginAttribute(row.Result).AddMembershipLowCardRef(vocab.MembAuditResult.GetId().Value()).EndAttribute()
	}
	inst.stampRun(sym, "")
	sym.EndSection()
	u64 := ent.GetSectionU64Array()
	stampInstance(u64, row.InstanceKey)
	u64.EndSection()
	u32 := ent.GetSectionU32Array()
	if row.LatencyMs > 0 {
		u32.BeginAttributeSingle(row.LatencyMs).AddMembershipLowCardRef(vocab.MembAuditLatencyMs.GetId().Value()).EndAttribute()
	}
	if row.RequestSizeB > 0 {
		u32.BeginAttributeSingle(row.RequestSizeB).AddMembershipLowCardRef(vocab.MembAuditRequestSizeB.GetId().Value()).EndAttribute()
	}
	if row.ResponseSizeB > 0 {
		u32.BeginAttributeSingle(row.ResponseSizeB).AddMembershipLowCardRef(vocab.MembAuditResponseSizeB.GetId().Value()).EndAttribute()
	}
	u32.EndSection()
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteLog lands one boxer.facts row tagged KindLog. Envelope fields
// (level, caller, service) go on the symbol section as low-card-refs;
// message/error on the string section; stack on the text section. Each
// user-supplied LogField is fanned out by its Kind into the typed section
// matching the value, carrying MembLogField as a MixedLowCardRef whose
// high-card parameter is the field NAME so readers can recover (name,
// value) pairs without parsing.
func (inst *Store) WriteLog(row factsstore.LogRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	inst.encodeLogEntity(ent, id, row)
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteLogs lands a whole batch of KindLog rows as a single multi-row Arrow
// IPC insert (one HTTP round-trip), instead of one POST per row. This is the
// shape logbridge.drain feeds — its in-memory ring already batches up to
// FlushN rows off the producer's hot path, and writing them one INSERT at a
// time created one ClickHouse part per row (heavy merge pressure) and
// defeated the batching. ids[i] corresponds to rows[i]. An empty batch is a
// no-op.
func (inst *Store) WriteLogs(rows []factsstore.LogRow) (ids []uint64, err error) {
	if len(rows) == 0 {
		return
	}
	ent := dml.NewInEntityFacts(inst.allocator, len(rows))
	ids = make([]uint64, len(rows))
	for i := range rows {
		id := inst.nextId.Add(1)
		ids[i] = id
		inst.encodeLogEntity(ent, id, rows[i])
		if cErr := ent.CommitEntity(); cErr != nil {
			err = eh.Errorf("chstore: write logs: commit entity %d: %w", i, cErr)
			return
		}
	}
	err = inst.shipRecords(context.Background(), ent)
	return
}

// encodeLogEntity encodes one KindLog row into ent (BeginEntity through the
// last section, no CommitEntity — the caller commits). Shared by the
// single-row WriteLog and the batched WriteLogs so the two paths cannot
// drift in how a log row maps onto the boxer.facts sections.
func (inst *Store) encodeLogEntity(ent *dml.InEntityFacts, id uint64, row factsstore.LogRow) {
	ts := defaultTs(row.Ts)
	nk := naturalKeyForLog(row, ts)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	logFieldMembId := vocab.MembLogField.GetId().Value()

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("log").AddMembershipLowCardRef(vocab.MembKindLog.GetId().Value()).EndAttribute()
	if row.AppId != "" {
		sym.BeginAttribute(string(row.AppId)).AddMembershipMixedLowCardRef(
			vocab.MembRuntimeApp.GetId().Value(), []byte(row.AppId)).EndAttribute()
	}
	if row.Level != "" {
		sym.BeginAttribute(row.Level).AddMembershipLowCardRef(vocab.MembLogLevel.GetId().Value()).EndAttribute()
	}
	if row.Caller != "" {
		sym.BeginAttribute(row.Caller).AddMembershipLowCardRef(vocab.MembLogCaller.GetId().Value()).EndAttribute()
	}
	if row.Service != "" {
		sym.BeginAttribute(row.Service).AddMembershipLowCardRef(vocab.MembLogService.GetId().Value()).EndAttribute()
	}
	inst.stampRun(sym, "")
	sym.EndSection()

	str := ent.GetSectionStringArray()
	if row.Message != "" {
		str.BeginAttributeSingle(row.Message).AddMembershipLowCardRef(vocab.MembLogMessage.GetId().Value()).EndAttribute()
	}
	if row.Error != "" {
		str.BeginAttributeSingle(row.Error).AddMembershipLowCardRef(vocab.MembLogError.GetId().Value()).EndAttribute()
	}
	for _, f := range row.Fields {
		if f.Kind != factsstore.LogFieldKindString && f.Kind != factsstore.LogFieldKindUnknown {
			continue
		}
		str.BeginAttributeSingle(f.Str).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
	}
	str.EndSection()

	if row.Stack != "" {
		txt := ent.GetSectionTextArray()
		txt.BeginAttributeSingle(row.Stack).AddMembershipLowCardRef(vocab.MembLogStack.GetId().Value()).EndAttribute()
		txt.EndSection()
	}

	writeLogTypedFields(ent, row.Fields, logFieldMembId, row.InstanceKey)
}

// writeLogTypedFields fans the non-string LogFields out to their matching
// canonical-type sections.
//
// A field's kind decides its section, so one row's fields arrive interleaved
// across up to six of them — and a section frame closes for good, which is why
// the writes cannot simply follow the field order. The deferred buffer holds
// them: each field enqueues under its section, and Flush writes each section's
// fields together and closes it once (ADR-0183 D4). This function used to do
// that with six nullable section handles and a six-branch close block, which
// is the same mechanism written out by hand.
//
// Per-section field order is preserved, which is the only order the wire
// records — sections are separate column families, so interleaving across them
// says nothing.
func writeLogTypedFields(ent *dml.InEntityFacts, fields []factsstore.LogField, logFieldMembId uint64, instanceKey uint64) {
	var buf dmlruntime.DeferredSectionBuffer
	// The window this line came from (ADR-0191 §SD4) rides the same section as
	// the uint fields and so must go through the same buffer: a section frame
	// closes for good, and a second opener would be refused. It is enqueued
	// first so it reads before the fields in the section's attribute order.
	if instanceKey != 0 {
		buf.Enqueue("u64Array", "instanceKey", func() error {
			stampInstance(ent.GetSectionU64Array(), instanceKey)
			return nil
		})
	}
	for _, f := range fields {
		switch f.Kind {
		case factsstore.LogFieldKindInt:
			buf.Enqueue("i64Array", "logField", func() error {
				ent.GetSectionI64Array().BeginAttributeSingle(f.Int).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
				return nil
			})
		case factsstore.LogFieldKindUint:
			buf.Enqueue("u64Array", "logField", func() error {
				ent.GetSectionU64Array().BeginAttributeSingle(f.Uint).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
				return nil
			})
		case factsstore.LogFieldKindFloat:
			buf.Enqueue("f64Array", "logField", func() error {
				ent.GetSectionF64Array().BeginAttributeSingle(f.Float).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
				return nil
			})
		case factsstore.LogFieldKindBool:
			buf.Enqueue("bool", "logField", func() error {
				ent.GetSectionBool().BeginAttribute(f.Bool).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
				return nil
			})
		case factsstore.LogFieldKindBytes:
			buf.Enqueue("blobArray", "logField", func() error {
				ent.GetSectionBlobArray().BeginAttributeSingle(f.Bytes).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
				return nil
			})
		case factsstore.LogFieldKindTime:
			buf.Enqueue("timeArray", "logField", func() error {
				ent.GetSectionTimeArray().BeginAttributeSingle(f.Time).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
				return nil
			})
		}
	}
	// The contributions cannot fail — each is one DML call whose errors the
	// DML accumulates for the entity commit — so the flush error is nil by
	// construction and the sections are what needs closing.
	_ = buf.Flush(func(section string) error {
		switch section {
		case "i64Array":
			ent.GetSectionI64Array().EndSection()
		case "u64Array":
			ent.GetSectionU64Array().EndSection()
		case "f64Array":
			ent.GetSectionF64Array().EndSection()
		case "bool":
			ent.GetSectionBool().EndSection()
		case "blobArray":
			ent.GetSectionBlobArray().EndSection()
		case "timeArray":
			ent.GetSectionTimeArray().EndSection()
		}
		return nil
	})
}

// stampRun writes the MembRuntimeRun attribute onto an already-open symbol
// section (ADR-0191 §SD3). It is the one place the run reaches a row, so
// every kind carries it the same way and a reader has one membership to
// filter on.
//
// rowRunId wins when the DTO carries one: for a live write it equals the
// store's, and for a backfill or a test it is the authoritative value. The
// store's configured run is the fallback, and both empty writes nothing —
// which reads back as an unattributed row rather than as a row belonging to
// run "".
//
// The attribute VALUE carries the run id as well as the high-cardinality
// parameter. That is how WriteRuntimeStart has always written it, and it is
// what lets a reader gather the run through the value lane
// (LW_CO_GATHER(`symbol:value`, LW_SEL_ATTRS(…))) — the parameter lane holds
// the same bytes, but LW_GET refuses a mixed channel without a param: token,
// and here the parameter is the thing being read.
func (inst *Store) stampRun(sym *dml.InEntityFactsSectionSymbol, rowRunId string) {
	runId := rowRunId
	if runId == "" {
		runId = inst.cfg.RunId
	}
	if runId == "" {
		return
	}
	sym.BeginAttribute(runId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeRun.GetId().Value(), []byte(runId)).EndAttribute()
}

// stampInstance writes the window key onto an already-open u64 section
// (ADR-0191 §SD3). Zero is unattributed — a host-written row, a CLI
// bootstrap, or a producer that has no window — and writes nothing, so a
// reader tells "no window" from "window 0" by absence rather than by value.
//
// The membership is spelled MembLifecycleTileKey for the reason §SD1 gives:
// the launch and workingset kinds already reuse it, one column to join on
// beats a per-kind spelling, and renaming it would renumber an id that rows
// on disk carry.
func stampInstance(u64 *dml.InEntityFactsSectionU64Array, instanceKey uint64) {
	if instanceKey == 0 {
		return
	}
	u64.BeginAttributeSingle(instanceKey).
		AddMembershipLowCardRef(vocab.MembLifecycleTileKey.GetId().Value()).EndAttribute()
}

// WriteRuntimeStart lands one boxer.facts row tagged KindRuntimeRun.
// The run_id is the natural key (entity-id) and rides as the high-card
// parameter of MembRuntimeRun so child app-lifecycle rows can join by
// equality on a single symbol membership.
func (inst *Store) WriteRuntimeStart(row factsstore.RuntimeStartRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyFor("runtime-run", app.AppIdT(row.RunId), []byte(row.Hostname), nil)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("runtime-run").AddMembershipLowCardRef(vocab.MembKindRuntimeRun.GetId().Value()).EndAttribute()
	inst.stampRun(sym, row.RunId)
	sym.BeginAttribute(row.Hostname).AddMembershipLowCardRef(vocab.MembRunHostname.GetId().Value()).EndAttribute()
	if row.GoVersion != "" {
		sym.BeginAttribute(row.GoVersion).AddMembershipLowCardRef(vocab.MembRunGoVersion.GetId().Value()).EndAttribute()
	}
	if row.VcsRevision != "" {
		sym.BeginAttribute(row.VcsRevision).AddMembershipLowCardRef(vocab.MembRunVcsRevision.GetId().Value()).EndAttribute()
	}
	if row.ModulePath != "" {
		sym.BeginAttribute(row.ModulePath).AddMembershipLowCardRef(vocab.MembRunModulePath.GetId().Value()).EndAttribute()
	}
	sym.EndSection()

	if row.VcsBuildInfo != "" {
		str := ent.GetSectionStringArray()
		str.BeginAttributeSingle(row.VcsBuildInfo).AddMembershipLowCardRef(vocab.MembRunVcsBuildInfo.GetId().Value()).EndAttribute()
		str.EndSection()
	}

	u64 := ent.GetSectionU64Array()
	u64.BeginAttributeSingle(uint64(row.Pid)).AddMembershipLowCardRef(vocab.MembRunPid.GetId().Value()).EndAttribute()
	u64.EndSection()

	bsec := ent.GetSectionBool()
	bsec.BeginAttribute(row.VcsModified).AddMembershipLowCardRef(vocab.MembRunVcsModified.GetId().Value()).EndAttribute()
	bsec.EndSection()

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteRuntimeHeartbeat lands one boxer.facts row tagged
// KindRuntimeHeartbeat. The row carries only the kind tag and the
// MembRuntimeRun mixed-LCR(run_id) so the heartbeat joins back to its
// runtime-start parent by the same predicate the lifecycle queries
// use. RunId is required; the natural key includes the timestamp
// nanoseconds so a hot heartbeat cadence doesn't dedup at insert.
func (inst *Store) WriteRuntimeHeartbeat(row factsstore.HeartbeatRow) (id uint64, err error) {
	if row.RunId == "" {
		err = eh.Errorf("chstore: WriteRuntimeHeartbeat requires a non-empty RunId")
		return
	}
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyForHeartbeat(row.RunId, ts)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("runtime-heartbeat").AddMembershipLowCardRef(vocab.MembKindRuntimeHeartbeat.GetId().Value()).EndAttribute()
	inst.stampRun(sym, row.RunId)
	sym.EndSection()

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteAppLifecycle lands one boxer.facts row tagged KindAppLifecycle.
// Symbol-section attributes carry the kind tag, the app reference, the
// run reference (so the row joins back to its runtime-start parent),
// and the phase ("started" or "stopped"). The optional StopReason rides
// on the string section. The tile key rides on the u64 section so two
// concurrent tiles for the same AppId are distinguishable.
func (inst *Store) WriteAppLifecycle(row factsstore.AppLifecycleRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	phase := row.Phase.String()
	nk := naturalKeyForLifecycle(row.RunId, row.AppId, row.TileKey, phase)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("app-lifecycle").AddMembershipLowCardRef(vocab.MembKindAppLifecycle.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(row.AppId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(row.AppId)).EndAttribute()
	inst.stampRun(sym, row.RunId)
	sym.BeginAttribute(phase).AddMembershipLowCardRef(vocab.MembLifecyclePhase.GetId().Value()).EndAttribute()
	sym.EndSection()

	if row.StopReason != "" {
		str := ent.GetSectionStringArray()
		str.BeginAttributeSingle(row.StopReason).AddMembershipLowCardRef(vocab.MembLifecycleStopReason.GetId().Value()).EndAttribute()
		str.EndSection()
	}

	u64 := ent.GetSectionU64Array()
	u64.BeginAttributeSingle(row.TileKey).AddMembershipLowCardRef(vocab.MembLifecycleTileKey.GetId().Value()).EndAttribute()
	u64.EndSection()

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteLaunch lands one boxer.facts row tagged KindLaunch (ADR-0135
// §SD6): the accepted `windowhost.open` request beside its app-lifecycle
// "started" row. Target app / run reuse the MembRuntimeApp /
// MembRuntimeRun identities; the caller rides MembLaunchCaller as a
// mixed-low-card-ref; the tile key reuses MembLifecycleTileKey so the
// row joins the lifecycle row on one column; the raw config bytes land
// on the blob section (nil for a plain open — no blob attribute).
func (inst *Store) WriteLaunch(row factsstore.LaunchRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	var tk [8]byte
	for i := 0; i < 8; i++ {
		tk[i] = byte(row.TileKey >> (8 * (7 - i)))
	}
	nk := naturalKeyFor("launch", row.TargetAppId, []byte(row.RunId), tk[:])
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("launch").AddMembershipLowCardRef(vocab.MembKindLaunch.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(row.TargetAppId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(row.TargetAppId)).EndAttribute()
	sym.BeginAttribute(string(row.CallerAppId)).AddMembershipMixedLowCardRef(
		vocab.MembLaunchCaller.GetId().Value(), []byte(row.CallerAppId)).EndAttribute()
	inst.stampRun(sym, row.RunId)
	if row.ConfigKind != "" {
		sym.BeginAttribute(row.ConfigKind).AddMembershipLowCardRef(vocab.MembLaunchConfigKind.GetId().Value()).EndAttribute()
	}
	sym.EndSection()

	u64 := ent.GetSectionU64Array()
	u64.BeginAttributeSingle(row.TileKey).AddMembershipLowCardRef(vocab.MembLifecycleTileKey.GetId().Value()).EndAttribute()
	u64.EndSection()

	if len(row.Config) > 0 {
		blob := ent.GetSectionBlobArray()
		blob.BeginAttributeSingle(row.Config).AddMembershipLowCardRef(vocab.MembLaunchConfig.GetId().Value()).EndAttribute()
		blob.EndSection()
	}

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// quoteSqlString single-quotes s for inline SQL, escaping single quotes by
// doubling. Used for the membership-keyed equality predicates that compare
// caller-controlled appId/key values; those are not amenable to FORMAT-time
// parameter binding over the HTTP interface.
func quoteSqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Count is a test helper — total rows in the qualified table.
func (inst *Store) Count(ctx context.Context) (n uint64, err error) {
	body, err := inst.cli.Query(ctx, "SELECT count() FROM "+inst.qualifiedTable()+" FORMAT TabSeparated")
	if err != nil {
		return
	}
	defer body.Close()
	var raw [32]byte
	read, _ := body.Read(raw[:])
	_, err = fmt.Sscanf(strings.TrimSpace(string(raw[:read])), "%d", &n)
	if err != nil {
		err = eh.Errorf("chstore: parse count: %w", err)
		return
	}
	return
}

// Truncate is a test helper — removes all rows.
func (inst *Store) Truncate(ctx context.Context) (err error) {
	err = inst.cli.Exec(ctx, "TRUNCATE TABLE IF EXISTS "+inst.qualifiedTable())
	return
}

// DropTable is a test helper — removes the table entirely so a
// subsequent SetupTable recreates it from the current DDL. Truncate
// only clears rows, which leaves the column schema in place; tests
// that span schema migrations need a real drop to pick up new
// columns.
func (inst *Store) DropTable(ctx context.Context) (err error) {
	err = inst.cli.Exec(ctx, "DROP TABLE IF EXISTS "+inst.qualifiedTable())
	return
}

// commitAndShip commits the single buffered entity then ships it. Used by
// the one-row Write* methods that build exactly one entity.
func (inst *Store) commitAndShip(ctx context.Context, ent *dml.InEntityFacts) (err error) {
	err = ent.CommitEntity()
	if err != nil {
		err = eh.Errorf("chstore: commit entity: %w", err)
		return
	}
	err = inst.shipRecords(ctx, ent)
	return
}

// shipRecords TransferRecords, InsertArrows, and releases the records.
// Entities must already be committed (commitAndShip commits the single-row
// case; WriteLogs commits each row before calling this). Returns the first
// error encountered.
func (inst *Store) shipRecords(ctx context.Context, ent *dml.InEntityFacts) (err error) {
	var records []arrow.RecordBatch
	records, err = ent.TransferRecords(nil)
	if err != nil {
		err = eh.Errorf("chstore: transfer records: %w", err)
		return
	}
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	err = inst.cli.InsertArrow(ctx, inst.qualifiedTable(), records)
	if err != nil {
		err = eh.Errorf("chstore: insert arrow: %w", err)
		return
	}
	return
}

func defaultTs(t time.Time) (out time.Time) {
	if t.IsZero() {
		out = time.Now().UTC()
		return
	}
	out = t.UTC()
	return
}

func naturalKeyFor(kind string, appId app.AppIdT, a, b []byte) (out []byte) {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(appId))
	_, _ = h.Write([]byte{0})
	if a != nil {
		_, _ = h.Write(a)
	}
	_, _ = h.Write([]byte{0})
	if b != nil {
		_, _ = h.Write(b)
	}
	out = h.Sum(nil)
	return
}

// naturalKeyForLifecycle seeds a stable per-event identifier for an
// app-lifecycle row. Distinct on (run_id, app_id, tile_key, phase) —
// each tile open and each tile close becomes one row with its own
// natural key.
func naturalKeyForLifecycle(runId string, appId app.AppIdT, tileKey uint64, phase string) (out []byte) {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte("app-lifecycle"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(runId))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(appId))
	_, _ = h.Write([]byte{0})
	var tk [8]byte
	for i := 0; i < 8; i++ {
		tk[i] = byte(tileKey >> (8 * (7 - i)))
	}
	_, _ = h.Write(tk[:])
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(phase))
	out = h.Sum(nil)
	return
}

// naturalKeyForHeartbeat seeds a stable per-tick identifier so two
// heartbeats for the same run at different timestamps occupy distinct
// rows (no upsert collapse). Ts nanoseconds + run_id is unique enough
// in practice; an idempotent re-ingest path can recognise duplicates
// if it ships later.
func naturalKeyForHeartbeat(runId string, ts time.Time) (out []byte) {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte("runtime-heartbeat"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(runId))
	_, _ = h.Write([]byte{0})
	var tsBuf [8]byte
	nanos := uint64(ts.UnixNano())
	for i := 0; i < 8; i++ {
		tsBuf[i] = byte(nanos >> (8 * (7 - i)))
	}
	_, _ = h.Write(tsBuf[:])
	out = h.Sum(nil)
	return
}

// naturalKeyForLog seeds a stable per-event identifier. Log rows are not
// deduped today (every event is a new row), but the natural key still has
// to be unique within (appId, level, message, ts-nanoseconds) so an
// idempotent re-ingest path can recognise duplicates if it ships later.
func naturalKeyForLog(row factsstore.LogRow, ts time.Time) (out []byte) {
	h := blake3.New(16, nil)
	_, _ = h.Write([]byte("log"))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(row.AppId))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(row.Level))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(row.Message))
	_, _ = h.Write([]byte{0})
	var tsBuf [8]byte
	nanos := uint64(ts.UnixNano())
	for i := 0; i < 8; i++ {
		tsBuf[i] = byte(nanos >> (8 * (7 - i)))
	}
	_, _ = h.Write(tsBuf[:])
	out = h.Sum(nil)
	return
}

func splitOnSemicolon(sql string) (out []string) {
	for s := range strings.SplitSeq(sql, ";") {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return
}
