//go:build llm_generated_opus47

// Package chstore is the ClickHouse-backed factsstore.FactsStoreI per
// ADR-0026 M2.5c. Writes go through the generated leeway DML builders
// (runtime/factsschema/dml.InEntityFacts) and ship as Arrow IPC via
// chclient.InsertArrow. Read operations (LatestState / DeleteState) are
// stubbed in M2.5c — the leeway-shaped SELECT against array-encoded
// membership columns is non-trivial and lands in a later sub-phase.
package chstore

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	factsddl "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ddl"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// Config carries the connection coordinates + qualified target table.
type Config struct {
	URL      string
	User     string
	Password string
	Database string
	Table    string
}

// Defaults targets the project's localhost CH at runtime.facts per the
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

// SetupTable applies the runtime.facts DDL idempotently. engineClause
// supplies the MergeTree partition / order / TTL settings — note the
// columns must be referenced by their leeway-encoded physical names
// (e.g. "id:id:u64:2k:0:0:") since the table has no logical aliases.
func (inst *Store) SetupTable(ctx context.Context, engineClause string) (err error) {
	if engineClause == "" {
		engineClause = "MergeTree() ORDER BY tuple()"
	}
	var ddl string
	ddl, err = factsddl.ComposeCreateTableSql(engineClause)
	if err != nil {
		err = eh.Errorf("chstore: setup: compose ddl: %w", err)
		return
	}
	ddl = strings.ReplaceAll(ddl, factsschema.DatabaseName+"."+factsschema.TableName, inst.qualifiedTable())
	if inst.cfg.Database != factsschema.DatabaseName {
		ddl = strings.ReplaceAll(ddl,
			"CREATE DATABASE IF NOT EXISTS "+factsschema.DatabaseName+";",
			"CREATE DATABASE IF NOT EXISTS "+inst.cfg.Database+";")
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

// WriteGrant lands one runtime.facts row tagged KindGrant.
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
	sym.EndSection()
	if row.Reason != "" {
		str := ent.GetSectionStringArray()
		str.BeginAttributeSingle(row.Reason).AddMembershipLowCardRef(vocab.MembGrantReason.GetId().Value()).EndAttribute()
		str.EndSection()
	}
	bsec := ent.GetSectionBool()
	bsec.BeginAttribute(row.Sticky).AddMembershipLowCardRef(vocab.MembGrantSticky.GetId().Value()).EndAttribute()
	bsec.EndSection()
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteAudit lands one runtime.facts row tagged KindAudit.
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
	sym.EndSection()
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

// WriteLog lands one runtime.facts row tagged KindLog. Envelope fields
// (level, caller, service) go on the symbol section as low-card-refs;
// message/error on the string section; stack on the text section. Each
// user-supplied LogField is fanned out by its Kind into the typed section
// matching the value, carrying MembLogField as a MixedLowCardRef whose
// high-card parameter is the field NAME so readers can recover (name,
// value) pairs without parsing.
func (inst *Store) WriteLog(row factsstore.LogRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyForLog(row, ts)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
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

	writeLogTypedFields(ent, row.Fields, logFieldMembId)

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// writeLogTypedFields fans the non-string LogFields out to their matching
// canonical-type sections. Each section is opened on first use and closed
// once at the end so the call structure follows the dml builder contract
// (every BeginAttribute must be balanced inside a single Begin/EndSection
// pair).
func writeLogTypedFields(ent *dml.InEntityFacts, fields []factsstore.LogField, logFieldMembId uint64) {
	var (
		i64Sec  *dml.InEntityFactsSectionI64Array
		u64Sec  *dml.InEntityFactsSectionU64Array
		f64Sec  *dml.InEntityFactsSectionF64Array
		boolSec *dml.InEntityFactsSectionBool
		blobSec *dml.InEntityFactsSectionBlobArray
		timeSec *dml.InEntityFactsSectionTimeArray
	)
	for _, f := range fields {
		switch f.Kind {
		case factsstore.LogFieldKindInt:
			if i64Sec == nil {
				i64Sec = ent.GetSectionI64Array()
			}
			i64Sec.BeginAttributeSingle(f.Int).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
		case factsstore.LogFieldKindUint:
			if u64Sec == nil {
				u64Sec = ent.GetSectionU64Array()
			}
			u64Sec.BeginAttributeSingle(f.Uint).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
		case factsstore.LogFieldKindFloat:
			if f64Sec == nil {
				f64Sec = ent.GetSectionF64Array()
			}
			f64Sec.BeginAttributeSingle(f.Float).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
		case factsstore.LogFieldKindBool:
			if boolSec == nil {
				boolSec = ent.GetSectionBool()
			}
			boolSec.BeginAttribute(f.Bool).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
		case factsstore.LogFieldKindBytes:
			if blobSec == nil {
				blobSec = ent.GetSectionBlobArray()
			}
			blobSec.BeginAttributeSingle(f.Bytes).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
		case factsstore.LogFieldKindTime:
			if timeSec == nil {
				timeSec = ent.GetSectionTimeArray()
			}
			timeSec.BeginAttributeSingle(f.Time).AddMembershipMixedLowCardRef(logFieldMembId, []byte(f.Name)).EndAttribute()
		}
	}
	if i64Sec != nil {
		i64Sec.EndSection()
	}
	if u64Sec != nil {
		u64Sec.EndSection()
	}
	if f64Sec != nil {
		f64Sec.EndSection()
	}
	if boolSec != nil {
		boolSec.EndSection()
	}
	if blobSec != nil {
		blobSec.EndSection()
	}
	if timeSec != nil {
		timeSec.EndSection()
	}
}

// WriteRuntimeStart lands one runtime.facts row tagged KindRuntimeRun.
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
	sym.BeginAttribute(row.RunId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeRun.GetId().Value(), []byte(row.RunId)).EndAttribute()
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

// WriteRuntimeHeartbeat lands one runtime.facts row tagged
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
	sym.BeginAttribute(row.RunId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeRun.GetId().Value(), []byte(row.RunId)).EndAttribute()
	sym.EndSection()

	err = inst.commitAndShip(context.Background(), ent)
	return
}

// WriteAppLifecycle lands one runtime.facts row tagged KindAppLifecycle.
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
	sym.BeginAttribute(row.RunId).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeRun.GetId().Value(), []byte(row.RunId)).EndAttribute()
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

// WriteState lands one runtime.facts row tagged KindState; the value bytes
// go in the blob section under the PersistKey membership.
func (inst *Store) WriteState(row factsstore.StateRow) (id uint64, err error) {
	id = inst.nextId.Add(1)
	ts := defaultTs(row.Ts)
	nk := naturalKeyFor("state", row.AppId, []byte(row.Key), nil)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)
	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("state").AddMembershipLowCardRef(vocab.MembKindState.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(row.AppId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(row.AppId)).EndAttribute()
	sym.BeginAttribute(row.Key).AddMembershipLowCardRef(vocab.MembPersistKey.GetId().Value()).EndAttribute()
	sym.EndSection()
	blob := ent.GetSectionBlobArray()
	blob.BeginAttributeSingle(row.Value).AddMembershipLowCardRef(vocab.MembPersistKey.GetId().Value()).EndAttribute()
	blob.EndSection()
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// LatestState returns the most recent state value for (appId, key). Relies
// on the positional invariant of WriteState — symbol.value[1] = "state",
// symbol.value[2] = appId string, symbol.value[3] = key. The filter uses
// has(symbol.lr, MembKindState.id) as a sanity check plus the positional
// matches; the blob value is hex-encoded over the wire for binary safety.
// Tombstone rows (the most recent write for (appId, key) is a DeleteState)
// return found=false.
func (inst *Store) LatestState(appId app.AppIdT, key string) (value []byte, found bool, err error) {
	ctx := context.Background()
	sql := fmt.Sprintf(`
SELECT
  hex(arrayElement(`+"`tv:blobArray:value:val:yh:g:0:0:0::data`"+`, 1)) AS v_hex,
  has(`+"`tv:bool:lr:lr:u64:2q:0:0:0::data`"+`, %d) AS is_tombstone
FROM %s
WHERE
  has(`+"`tv:symbol:lr:lr:u64:2q:0:0:0::data`"+`, %d)
  AND arrayElement(`+"`tv:symbol:value:val:s:m:0:24:0::data`"+`, 2) = %s
  AND arrayElement(`+"`tv:symbol:value:val:s:m:0:24:0::data`"+`, 3) = %s
ORDER BY `+"`ts:ts:z64:2k:0:0:`"+` DESC
LIMIT 1
FORMAT TabSeparated`,
		vocab.MembPersistTombstone.GetId().Value(),
		inst.qualifiedTable(),
		vocab.MembKindState.GetId().Value(),
		quoteSqlString(string(appId)),
		quoteSqlString(key))
	body, err := inst.cli.Query(ctx, sql)
	if err != nil {
		err = eh.Errorf("chstore: latest state query: %w", err)
		return
	}
	defer body.Close()
	buf := make([]byte, 65536)
	n, _ := body.Read(buf)
	raw := strings.TrimRight(string(buf[:n]), "\n")
	if raw == "" {
		return
	}
	parts := strings.Split(raw, "\t")
	if len(parts) != 2 {
		err = eh.Errorf("chstore: latest state: unexpected row shape: %q", raw)
		return
	}
	if parts[1] == "1" {
		return
	}
	value, err = hex.DecodeString(parts[0])
	if err != nil {
		err = eh.Errorf("chstore: latest state: hex decode: %w", err)
		return
	}
	found = true
	return
}

// DeleteState writes a tombstone row for (appId, key): same shape as a
// state row plus a bool-section attribute marked with MembPersistTombstone.
// LatestState treats the most-recent tombstone as found=false.
func (inst *Store) DeleteState(appId app.AppIdT, key string) (err error) {
	id := inst.nextId.Add(1)
	ts := time.Now().UTC()
	nk := naturalKeyFor("state-tomb", appId, []byte(key), nil)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)
	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("state").AddMembershipLowCardRef(vocab.MembKindState.GetId().Value()).EndAttribute()
	sym.BeginAttribute(string(appId)).AddMembershipMixedLowCardRef(
		vocab.MembRuntimeApp.GetId().Value(), []byte(appId)).EndAttribute()
	sym.BeginAttribute(key).AddMembershipLowCardRef(vocab.MembPersistKey.GetId().Value()).EndAttribute()
	sym.EndSection()
	b := ent.GetSectionBool()
	b.BeginAttribute(true).AddMembershipLowCardRef(vocab.MembPersistTombstone.GetId().Value()).EndAttribute()
	b.EndSection()
	err = inst.commitAndShip(context.Background(), ent)
	return
}

// quoteSqlString single-quotes s for inline SQL, escaping single quotes by
// doubling. Used for the positional ARRAY-element equality predicates in
// LatestState — those values come from the caller-controlled appId/key and
// are not amenable to FORMAT-time parameter binding.
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

// commitAndShip CommitEntities, TransferRecords, InsertArrows, and releases
// the records. Returns the first error encountered.
func (inst *Store) commitAndShip(ctx context.Context, ent *dml.InEntityFacts) (err error) {
	err = ent.CommitEntity()
	if err != nil {
		err = eh.Errorf("chstore: commit entity: %w", err)
		return
	}
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
	for _, s := range strings.Split(sql, ";") {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return
}
