// Package chstore is the ClickHouse-backed factsstore.FactsStoreI per
// ADR-0026 M2.5c. Writes go through the generated leeway DML builders
// (runtime/factsschema/dml.InEntityFacts) and ship as Arrow IPC through the
// store's record-store executor, which applies an insert transform a
// deployment registered for the facts table (ADR-0402). Reads (the app trail, the log rows, the run events, the
// launch statistics) are hand-composed leeway-shaped SELECTs against the array-encoded
// membership columns — the code class ADR-0105 D5 replaces with generated
// stores kind by kind. App state left this table for its own generated
// store: persist state with ADR-0105 D3a, workingsets and column-width
// overrides with its Update of 2026-08-15; each kind's verbs went with it.
package chstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/db/clickhouse/chversion"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	dmlruntime "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// Config carries the connection coordinates + qualified target table.
//
// RunId is the process's run identity (ADR-0191 §SD3), stamped onto every row
// this store writes that does not carry one of its own. It lives here rather
// than on each row DTO because it is process-wide: eight DTOs repeating one
// constant is eight chances for them to disagree, and three of them
// (GrantRow, LogRow and the bus-request row since retired) had no field for
// it at all — which is why
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
	// RowIds says where the row ids come from (ADR-0397 §SD2). The zero
	// value leases the facts rows' stream through the lease table in
	// Database.
	RowIds RowIds
	// ReadTable is the relation every read of this store goes to (ADR-0402
	// §SD3), database-qualified; empty reads Database.Table. It returns the
	// facts columns under their names and types and may be a view: a view
	// over a deployment's own table, or a Merge over storage tables. Writes,
	// and the statements that change or empty stored rows, name
	// Database.Table whatever it says.
	ReadTable string
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

// ConfigFromEnv layers the CLICKHOUSE_* registry entries (ADR-0009) over
// Defaults: the endpoint from CLICKHOUSE_ENDPOINT, falling back to
// CLICKHOUSE_URL, and the credentials from CLICKHOUSE_USER /
// CLICKHOUSE_PASSWORD. The values come from chclient.ConfigFromEnv, which is
// the one place that decides the endpoint precedence, so this store and every
// other chclient consumer resolve the same server. An entry that is unset or
// empty leaves the Defaults value in place, so the zero-configuration case
// stays exactly what Defaults describes.
//
// Database and Table are deliberately not env-derived. The facts table is a
// fixed location in the schema (factsschema.DatabaseName / TableName, which
// ComposeSetupSQL rewrites against), not the session's default database, so
// CLICKHOUSE_DATABASE does not move it. RunId is the process's, not the
// environment's. ReadTable is the environment's: BOXER_FACTS_READ_TABLE
// (factsschema.ReadTableSetting, ADR-0402 §SD3), validated by New.
//
// Not a live re-read: the registry resolves each entry once and caches it, so
// mutating the environment after the first resolution has no effect.
func ConfigFromEnv() (c Config) {
	c = Defaults()
	cc := chclient.ConfigFromEnv()
	c.URL = cc.URL
	c.User = cc.User
	c.Password = cc.Password
	c.ReadTable = factsschema.ReadTableSetting.Get()
	return
}

// ConfigWithEnvDefaults completes the fields cfg left empty from
// ConfigFromEnv and returns every field cfg did set unchanged — so a caller
// that builds a Config for its own reasons (a RunId, a scratch database) still
// picks up the environment's server and credentials, and one that pins a
// server keeps it.
//
// Password is filled whenever cfg leaves it empty, including when cfg names a
// User: the common case is a config assembled from Defaults (user "default",
// no password) against a server that does require one, which is the whole
// point of consulting the environment. A caller that must send no key at all
// while CLICKHOUSE_PASSWORD is set in its environment builds the Store with
// New, which reads nothing.
func ConfigWithEnvDefaults(cfg Config) (out Config) {
	out = cfg
	e := ConfigFromEnv()
	if out.URL == "" {
		out.URL = e.URL
	}
	if out.User == "" {
		out.User = e.User
	}
	if out.Password == "" {
		out.Password = e.Password
	}
	if out.Database == "" {
		out.Database = e.Database
	}
	if out.Table == "" {
		out.Table = e.Table
	}
	// The read table setting is about the facts table: a config pointed at a
	// scratch table keeps reading the table it writes.
	if out.ReadTable == "" && out.Database+"."+out.Table == factsschema.QualifiedTableName {
		out.ReadTable = e.ReadTable
	}
	return
}

// Store is the live-CH FactsStoreI. Each Write* call constructs a fresh
// InEntityFacts builder, encodes one row, and ships it as a single-record
// Arrow IPC batch through the store's executor.
type Store struct {
	cfg Config
	cli *chclient.Client
	// exec is cli as the executor the reads and the inserts go through:
	// statements out, Arrow record batches back.
	exec *storeexec.Executor
	// ship is where the writes' batches go: exec, or a capture for the
	// round-trip check, which encodes rows without a server.
	ship      recordstore.ExecutorI
	allocator memory.Allocator
	// ids mints the row ids (ADR-0397 §SD2): leased from the id stream of
	// boxer.facts rows, or process-local while no block can be leased.
	ids rowIds
}

var _ factsstore.FactsStoreI = (*Store)(nil)

// New constructs a Store. Does not connect or create the table — call Ping
// to verify reachability and SetupTable to apply DDL.
func New(cfg Config) (s *Store, err error) {
	if cfg.URL == "" || cfg.Database == "" || cfg.Table == "" {
		err = eh.Errorf("chstore: cfg requires URL + Database + Table")
		return
	}
	if cfg.ReadTable != "" {
		if err = recordstore.CheckTableRef(cfg.ReadTable); err != nil {
			err = eh.Errorf("chstore: ReadTable: %w", err)
			return
		}
	}
	s = &Store{
		cfg: cfg,
		cli: chclient.New(chclient.Config{
			URL: cfg.URL, User: cfg.User, Password: cfg.Password,
		}, nil),
		allocator: memory.NewGoAllocator(),
	}
	s.exec, err = storeexec.New(s.cli, s.allocator)
	if err != nil {
		err = eh.Errorf("chstore: %w", err)
		return
	}
	s.ship = s.exec
	err = s.ids.init(s.exec, s.leaseLayout(), cfg.RowIds)
	if err != nil {
		err = eh.Errorf("chstore: %w", err)
	}
	return
}

// Ping returns nil when the CH server is reachable.
func (inst *Store) Ping(ctx context.Context) (err error) {
	err = inst.cli.Ping(ctx)
	return
}

// ComposeSetupSQL returns the exact table DDL script SetupTable applies for cfg:
// the CREATE DATABASE + CREATE TABLE statements (separated by ';'), composed
// with no database connection and no side effects. An empty engineClause
// selects [EngineClause] — the BOXER_FACTS_ENGINE setting, by default
// factsschema.DefaultEngineClause (ADR-0397). The table carries the insert
// deduplication window and a stamp naming its columns and its layout.
// SetupTable is defined in terms of this function, so a caller that only
// wants the SQL (the `keelsonddl` CLI) emits byte-for-byte what first-run
// initialisation executes for the table, with no risk of drift. The read
// surface and the id lease table are not in it: SetupStatements carries all
// three, and `leeway sqlsurface print` is the surface as a script.
//
// The columns are referenced by their leeway-encoded physical names (e.g.
// "id:id:u64:47::0:") since the table has no logical aliases.
func ComposeSetupSQL(cfg Config, engineClause string) (sql string, err error) {
	t, err := composeFactsTable(cfg, engineClause)
	sql = t.sql
	return
}

// SetupTable applies the boxer.facts DDL idempotently against the live CH
// connection, provisions the id lease table the store mints row ids through
// (ADR-0397 §SD2; not when the configuration brings an allocator of its
// own) and, when that table holds no row for the store's stream while the
// facts carry ids of it, starts the stream after the largest of them — a
// lease table that was lost must not hand those ids out again —, then
// makes sure the server carries leeway's SQL read
// surface ([Store.EnsureReadSurface]). engineClause supplies the engine,
// partition key, sorting key and TTL (empty selects [EngineClause]). The DDL
// is composed by ComposeSetupSQL, so this path and the `keelsonddl` CLI
// cannot drift.
//
// Idempotent means CREATE TABLE IF NOT EXISTS, which is not the same as
// migrating: an existing table is left exactly as it is, including its column
// names and its layout. Physical column names encode the column's aspect
// bitmask, so a change to the leeway aspect vocabularies renames columns in
// the generated DDL while the deployed table keeps the old ones — after
// which inserts naming the new columns fail against it. There is no
// automatic reconciliation. Recovering means ALTER TABLE ... RENAME COLUMN
// per renamed column, or dropping and re-creating the table if the trail is
// expendable.
//
// Under an insert transform for the table ([WriteTransformed], ADR-0402
// §SD5) the facts table is not created: an empty table of that name beside
// the deployment's storage is one that reads drain from without an error.
// Only its database is, which the lease table and the trail views live in;
// the lease table and the read surface are set up as without one.
//
// Without a transform, a facts table that turns out to be a view fails
// setup with [ErrFactsTableTakesNoInsert]: the binary is on a deployment's
// server without the deployment's transform.
func (inst *Store) SetupTable(ctx context.Context, engineClause string) (err error) {
	var stmts []string
	stmts, err = tableStatements(inst.cfg, engineClause)
	if err != nil {
		return
	}
	for _, stmt := range stmts {
		err = inst.cli.Exec(ctx, stmt)
		if err != nil {
			err = eh.Errorf("chstore: setup: exec: %w", err)
			return
		}
	}
	err = inst.checkWritable(ctx)
	if err != nil {
		return
	}
	if inst.cfg.RowIds.Allocator == nil {
		err = rowalloc.ProvisionIn(ctx, inst.exec, inst.leaseLayout())
		if err != nil {
			err = eh.Errorf("chstore: setup: %w", err)
			return
		}
		// A lease table that holds no row for the stream, beside facts that
		// carry ids of it, was lost: the stream continues after the largest.
		var seededAt uint64
		seededAt, err = seedLease(ctx, inst.exec, inst.leaseLayout(), inst.readTable(), inst.cfg.RowIds.stream().Value())
		if err != nil {
			err = eh.Errorf("chstore: setup: %w", err)
			return
		}
		if seededAt != 0 {
			log.Warn().Str("leaseTable", inst.leaseLayout().Table()).Str("facts", inst.readTable()).Uint64("continuesAt", seededAt).
				Msg("chstore: the id lease table had no row for the facts row stream while the facts hold ids of it; the stream continues after the largest")
		}
	}
	err = inst.EnsureReadSurface(ctx)
	return
}

// ErrFactsTableTakesNoInsert is what setup returns when the name the store
// writes to is a relation that takes no insert — a view — and the process
// carries no insert transform for it (ADR-0402 §SD2): the server stores the
// facts in a deployment's own table, and this binary would fail every
// write. Match with errors.Is.
var ErrFactsTableTakesNoInsert = errors.New("the facts table is a relation that takes no insert, and no insert transform is registered for it")

// checkWritable fails setup when the store's inserts cannot land where it
// sends them: CREATE TABLE IF NOT EXISTS succeeds over a view of the same
// name, so the DDL alone does not tell. Under a transform the inserts go to
// the deployment's table and the name may be anything.
func (inst *Store) checkWritable(ctx context.Context) (err error) {
	if WriteTransformed(inst.cfg) {
		return
	}
	body, err := inst.cli.Query(ctx, "SELECT engine FROM system.tables WHERE database = "+quoteSqlString(inst.cfg.Database)+" AND name = "+quoteSqlString(inst.cfg.Table))
	if err != nil {
		err = eh.Errorf("chstore: setup: read the facts table's engine: %w", err)
		return
	}
	raw, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		err = eh.Errorf("chstore: setup: read the facts table's engine: %w", err)
		return
	}
	if engine := strings.TrimSpace(string(raw)); recordstore.TakesNoInsert(engine) {
		err = eb.Build().Str("table", inst.writeTable()).Str("engine", engine).
			Errorf("chstore: setup: %w; this binary needs the deployment's transform (recordstore.RegisterInsertTransform) to write facts on this server", ErrFactsTableTakesNoInsert)
	}
	return
}

// EnsureReadSurface installs leeway's SQL read surface when the server does
// not carry the revision this build emits against (ADR-0367, update of
// 2026-11-11). The surface is part of setup rather than an optional extra:
// every read of this store expands into its functions, so a store whose
// server lacks them can write and cannot read.
//
// A server already carrying the build's surface — its generation and digest
// — costs one round trip. A server carrying a newer generation is left as it
// is: installs are monotonic, so an older build sharing a server with a newer
// one does not put older bodies back (lwsqlsurface.Decide); it only checks
// that the server still declares every function this build calls. Anything
// else — no stamp, an older generation, other bodies — is a full install,
// on the order of a hundred statements (ADR-0171, update of 2026-11-20).
//
// The same round trip reads the server's release, and a server below the
// ClickHouse floor is refused before anything is installed (chversion). A
// server without the stamp functions fails that query; the release is then
// read on its own, ahead of the install.
func (inst *Store) EnsureReadSurface(ctx context.Context) (err error) {
	stamp := lwsqlsurface.ServerStamp{Generation: -1}
	release := ""
	body, qerr := inst.cli.Query(ctx, "SELECT "+chversion.SelectVersion+", "+lwsqlsurface.StampSelect)
	if qerr == nil {
		raw, rerr := io.ReadAll(body)
		_ = body.Close()
		if rerr == nil {
			fields := strings.Split(strings.TrimSpace(string(raw)), "\t")
			if len(fields) == 3 {
				release = fields[0]
				stamp = lwsqlsurface.ParseStamp(fields[1], fields[2])
			}
		}
	}
	if release == "" {
		release, err = inst.serverRelease(ctx)
		if err != nil {
			return
		}
	}
	err = chversion.Check("server", release)
	if err != nil {
		err = eh.Errorf("chstore: setup: %w", err)
		return
	}
	switch lwsqlsurface.Decide(stamp) {
	case lwsqlsurface.ActionNone:
		return
	case lwsqlsurface.ActionKeepNewer:
		err = lwsqlsurface.VerifyServes(ctx, inst.cli)
		if err != nil {
			err = eh.Errorf("chstore: setup: %w", err)
		}
		return
	}
	err = lwsqlsurface.Install(ctx, inst.cli)
	if err != nil {
		err = eh.Errorf("chstore: setup: install the leeway SQL read surface: %w", err)
	}
	return
}

// serverRelease reads version() alone.
func (inst *Store) serverRelease(ctx context.Context) (release string, err error) {
	body, err := inst.cli.Query(ctx, "SELECT "+chversion.SelectVersion)
	if err != nil {
		err = eh.Errorf("chstore: setup: read the server's version: %w", err)
		return
	}
	raw, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		err = eh.Errorf("chstore: setup: read the server's version: %w", err)
		return
	}
	release = strings.TrimSpace(string(raw))
	return
}

// SetupStatements is the whole of setup as statements, for a caller that
// runs them one by one through its own client (queryrunsvc, which tags every
// query it sends): the table DDL of ComposeSetupSQL, the id lease table's,
// then leeway's SQL read surface — its function families, the revision
// marker, and the views — in the order [lwsqlsurface.AllStatements] gives,
// which is dependency order.
// A server these ran on can be written and read (ADR-0367, update of
// 2026-11-11).
//
// It is what SetupTable does without the checks a connection allows: every
// surface statement is a CREATE OR REPLACE and runs whether or not the
// server already carries the revision, and nothing probes for a name
// collision, drops a retired spelling or creates a dependent view.
// [lwsqlsurface.Install] does those; a host boot runs it.
//
// Under an insert transform for the table the table DDL is left out, as in
// SetupTable (ADR-0402 §SD5).
func SetupStatements(cfg Config, engineClause string) (stmts []string, err error) {
	stmts, err = tableStatements(cfg, engineClause)
	if err != nil {
		return
	}
	if cfg.RowIds.Allocator == nil {
		stmts = append(stmts, rowalloc.ProvisionSQL(rowalloc.Layout{Database: cfg.Database})...)
	}
	stmts = append(stmts, lwsqlsurface.AllStatements("")...)
	return
}

// tableStatements is ComposeSetupSQL split into statements; under an insert
// transform for the table, only the database's (ADR-0402 §SD5).
func tableStatements(cfg Config, engineClause string) (stmts []string, err error) {
	if WriteTransformed(cfg) {
		if err = recordstore.CheckTableRef(cfg.Database); err != nil {
			return
		}
		return []string{"CREATE DATABASE IF NOT EXISTS " + cfg.Database}, nil
	}
	ddl, err := ComposeSetupSQL(cfg, engineClause)
	if err != nil {
		return
	}
	for _, stmt := range splitOnSemicolon(ddl) {
		if stmt = strings.TrimSpace(stmt); stmt != "" {
			stmts = append(stmts, stmt)
		}
	}
	return
}

// writeTable is the table inserts and the statements that change or empty
// stored rows name: Database.Table.
func (inst *Store) writeTable() string {
	return inst.cfg.Database + "." + inst.cfg.Table
}

// readTable is the relation every read goes to (ADR-0402 §SD3): ReadTable,
// or the table written to.
func (inst *Store) readTable() string {
	if inst.cfg.ReadTable != "" {
		return inst.cfg.ReadTable
	}
	return inst.writeTable()
}

// WriteGrant lands one boxer.facts row tagged KindGrant.
func (inst *Store) WriteGrant(ctx context.Context, row factsstore.GrantRow) (id uint64, err error) {
	id, err = inst.mintId(ctx)
	if err != nil {
		return
	}
	ts := defaultTs(row.Ts)
	nk := naturalKeyFor("grant", row.AppId, []byte(row.Pattern), []byte(row.Direction.String()))
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)
	if !row.ExpiresAt.IsZero() {
		ent.SetLifecycle(row.ExpiresAt)
	}
	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("grant").AddMembershipLowCardRef(vocab.MembKindGrant.GetId().Value()).EndAttribute()
	stampApp(sym, row.AppId)
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
	err = inst.commitAndShip(ctx, ent)
	return
}

// WriteLog lands one boxer.facts row tagged KindLog. Envelope fields
// (level, caller, service) go on the symbol section as low-card-refs;
// message/error on the string section; stack on the text section. Each
// user-supplied LogField is fanned out by its Kind into the typed section
// matching the value, carrying MembLogField as a MixedLowCardRef whose
// high-card parameter is the field NAME so readers can recover (name,
// value) pairs without parsing.
func (inst *Store) WriteLog(ctx context.Context, row factsstore.LogRow) (id uint64, err error) {
	id, err = inst.mintId(ctx)
	if err != nil {
		return
	}
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	inst.encodeLogEntity(ent, id, row)
	err = inst.commitAndShip(ctx, ent)
	return
}

// WriteLogs lands a whole batch of KindLog rows as a single multi-row Arrow
// IPC insert (one HTTP round-trip), instead of one POST per row. This is the
// shape logbridge.drain feeds — its in-memory ring already batches up to
// FlushN rows off the producer's hot path, and writing them one INSERT at a
// time created one ClickHouse part per row (heavy merge pressure) and
// defeated the batching. ids[i] corresponds to rows[i]. An empty batch is a
// no-op.
func (inst *Store) WriteLogs(ctx context.Context, rows []factsstore.LogRow) (ids []uint64, err error) {
	if len(rows) == 0 {
		return
	}
	ent := dml.NewInEntityFacts(inst.allocator, len(rows))
	ids = make([]uint64, len(rows))
	for i := range rows {
		id, mErr := inst.mintId(ctx)
		if mErr != nil {
			err = mErr
			return
		}
		ids[i] = id
		inst.encodeLogEntity(ent, id, rows[i])
		if cErr := ent.CommitEntity(); cErr != nil {
			err = eb.Build().Int("entity", i).Errorf("chstore: write logs: commit entity: %w", cErr)
			return
		}
	}
	err = inst.shipRecords(ctx, ent)
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
	stampApp(sym, row.AppId)
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
	// first so it reads before the fields in the section's attribute order,
	// and always, zero included, as the origin's instance slot.
	buf.Enqueue("u64Array", "instanceKey", func() error {
		stampInstance(ent.GetSectionU64Array(), instanceKey)
		return nil
	})
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

// Every kind this store writes carries the three slots of the shared
// origin component (ADR-0330 §SD3, ADR-0277 §SD1) — run, app and instance,
// once each, on the memberships audit.Origin spells them with — so a row
// of any kind reads as that component by its slots (ADR-0146 §D5) and a
// reader selects every writer's rows by one predicate. stampRun, stampApp
// and stampInstance are the three; each writes its slot even when the
// value is unattributed, "" or 0 (ADR-0191 §SD3), as the generated stores'
// origin does. A reader treats those values as unknown, never as a run ""
// or a window 0.

// stampRun writes the MembRuntimeRun attribute onto an already-open symbol
// section (ADR-0191 §SD3). It is the one place the run reaches a row, so
// every kind carries it the same way and a reader has one membership to
// filter on.
//
// rowRunId wins when the DTO carries one: for a live write it equals the
// store's, and for a backfill or a test it is the authoritative value. The
// store's configured run is the fallback; with neither the slot holds "",
// unattributed.
//
// The run rides the plain channel as the attribute's value, as the app id
// does (ADR-0277 §SD8): the mixed channel carried the same bytes a second
// time as the membership's parameter, which added nothing and kept these
// rows from reading as the origin component.
func (inst *Store) stampRun(sym *dml.InEntityFactsSectionSymbol, rowRunId string) {
	runId := rowRunId
	if runId == "" {
		runId = inst.cfg.RunId
	}
	sym.BeginAttribute(runId).AddMembershipLowCardRef(vocab.MembRuntimeRun.GetId().Value()).EndAttribute()
}

// stampApp writes the MembRuntimeApp attribute onto an already-open symbol
// section: the app a row is about, "" for the host's own work.
func stampApp(sym *dml.InEntityFactsSectionSymbol, appId app.AppIdT) {
	sym.BeginAttribute(string(appId)).AddMembershipLowCardRef(vocab.MembRuntimeApp.GetId().Value()).EndAttribute()
}

// stampInstance writes the window key onto an already-open u64 section
// (ADR-0191 §SD3). Zero is unattributed — a host-written row, a CLI
// bootstrap, or a producer that has no window.
//
// The membership is spelled MembLifecycleTileKey for the reason §SD1 gives:
// the launch and workingset kinds already reuse it, one column to join on
// beats a per-kind spelling, and renaming it would renumber an id that rows
// on disk carry.
func stampInstance(u64 *dml.InEntityFactsSectionU64Array, instanceKey uint64) {
	u64.BeginAttributeSingle(instanceKey).
		AddMembershipLowCardRef(vocab.MembLifecycleTileKey.GetId().Value()).EndAttribute()
}

// WriteRuntimeStart lands one boxer.facts row tagged KindRuntimeRun.
// The run_id is the natural key (entity-id) and rides as the value of
// MembRuntimeRun so child app-lifecycle rows can join by equality on a
// single symbol membership. The run is the host's own work: its origin
// names no app and no window.
func (inst *Store) WriteRuntimeStart(ctx context.Context, row factsstore.RuntimeStartRow) (id uint64, err error) {
	id, err = inst.mintId(ctx)
	if err != nil {
		return
	}
	ts := defaultTs(row.Ts)
	nk := naturalKeyFor("runtime-run", app.AppIdT(row.RunId), []byte(row.Hostname), nil)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("runtime-run").AddMembershipLowCardRef(vocab.MembKindRuntimeRun.GetId().Value()).EndAttribute()
	inst.stampRun(sym, row.RunId)
	stampApp(sym, "")
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
	if row.BuildId != "" {
		sym.BeginAttribute(row.BuildId).AddMembershipLowCardRef(vocab.MembRunBuildId.GetId().Value()).EndAttribute()
	}
	// The driver stamp (ADR-0344 §SD5).
	if row.Driver != "" {
		sym.BeginAttribute(row.Driver).AddMembershipLowCardRef(vocab.MembRunDriver.GetId().Value()).EndAttribute()
	}
	if row.DriverJob != "" {
		sym.BeginAttribute(row.DriverJob).AddMembershipLowCardRef(vocab.MembRunDriverJob.GetId().Value()).EndAttribute()
	}
	if row.DriverJobKind != "" {
		sym.BeginAttribute(row.DriverJobKind).AddMembershipLowCardRef(vocab.MembRunDriverJobKind.GetId().Value()).EndAttribute()
	}
	sym.EndSection()

	if row.VcsBuildInfo != "" || row.Task != "" || row.Call != "" {
		str := ent.GetSectionStringArray()
		if row.VcsBuildInfo != "" {
			str.BeginAttributeSingle(row.VcsBuildInfo).AddMembershipLowCardRef(vocab.MembRunVcsBuildInfo.GetId().Value()).EndAttribute()
		}
		// The delegation, on the trail's memberships and sections, so the
		// trail's context columns read it (ADR-0344 §SD5).
		if row.Task != "" {
			str.BeginAttributeSingle(row.Task).AddMembershipLowCardRef(vocab.MembTrailTask.GetId().Value()).EndAttribute()
		}
		if row.Call != "" {
			str.BeginAttributeSingle(row.Call).AddMembershipLowCardRef(vocab.MembTrailCall.GetId().Value()).EndAttribute()
		}
		str.EndSection()
	}

	u64 := ent.GetSectionU64Array()
	u64.BeginAttributeSingle(uint64(row.Pid)).AddMembershipLowCardRef(vocab.MembRunPid.GetId().Value()).EndAttribute()
	if row.Driver != "" {
		u64.BeginAttributeSingle(uint64(row.DriverAttempt)).AddMembershipLowCardRef(vocab.MembRunDriverAttempt.GetId().Value()).EndAttribute()
	}
	if row.Task != "" {
		u64.BeginAttributeSingle(row.TaskEpoch).AddMembershipLowCardRef(vocab.MembTrailTaskEpoch.GetId().Value()).EndAttribute()
	}
	stampInstance(u64, 0)
	u64.EndSection()

	bsec := ent.GetSectionBool()
	bsec.BeginAttribute(row.VcsModified).AddMembershipLowCardRef(vocab.MembRunVcsModified.GetId().Value()).EndAttribute()
	bsec.EndSection()

	err = inst.commitAndShip(ctx, ent)
	return
}

// WriteRuntimeHeartbeat lands one boxer.facts row tagged
// KindRuntimeHeartbeat. The row carries the kind tag and the origin's
// slots — the run on MembRuntimeRun, so the heartbeat joins back to its
// runtime-start parent by the same predicate the lifecycle queries use,
// and no app or window. RunId is required; the natural key includes the
// timestamp nanoseconds so a hot heartbeat cadence doesn't dedup at
// insert.
func (inst *Store) WriteRuntimeHeartbeat(ctx context.Context, row factsstore.HeartbeatRow) (id uint64, err error) {
	if row.RunId == "" {
		err = eh.Errorf("chstore: WriteRuntimeHeartbeat requires a non-empty RunId")
		return
	}
	id, err = inst.mintId(ctx)
	if err != nil {
		return
	}
	ts := defaultTs(row.Ts)
	nk := naturalKeyForHeartbeat(row.RunId, ts)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("runtime-heartbeat").AddMembershipLowCardRef(vocab.MembKindRuntimeHeartbeat.GetId().Value()).EndAttribute()
	inst.stampRun(sym, row.RunId)
	stampApp(sym, "")
	sym.EndSection()

	u64 := ent.GetSectionU64Array()
	stampInstance(u64, 0)
	u64.EndSection()

	err = inst.commitAndShip(ctx, ent)
	return
}

// WriteAppLifecycle lands one boxer.facts row tagged KindAppLifecycle.
// Symbol-section attributes carry the kind tag, the app reference, the
// run reference (so the row joins back to its runtime-start parent),
// and the phase ("started" or "stopped"). The optional StopReason rides
// on the string section. The tile key rides on the u64 section so two
// concurrent tiles for the same AppId are distinguishable.
func (inst *Store) WriteAppLifecycle(ctx context.Context, row factsstore.AppLifecycleRow) (id uint64, err error) {
	id, err = inst.mintId(ctx)
	if err != nil {
		return
	}
	ts := defaultTs(row.Ts)
	phase := row.Phase.String()
	nk := naturalKeyForLifecycle(row.RunId, row.AppId, row.TileKey, phase)
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("app-lifecycle").AddMembershipLowCardRef(vocab.MembKindAppLifecycle.GetId().Value()).EndAttribute()
	stampApp(sym, row.AppId)
	inst.stampRun(sym, row.RunId)
	sym.BeginAttribute(phase).AddMembershipLowCardRef(vocab.MembLifecyclePhase.GetId().Value()).EndAttribute()
	sym.EndSection()

	if row.StopReason != "" {
		str := ent.GetSectionStringArray()
		str.BeginAttributeSingle(row.StopReason).AddMembershipLowCardRef(vocab.MembLifecycleStopReason.GetId().Value()).EndAttribute()
		str.EndSection()
	}

	u64 := ent.GetSectionU64Array()
	stampInstance(u64, row.TileKey)
	u64.EndSection()

	err = inst.commitAndShip(ctx, ent)
	return
}

// WriteLaunch lands one boxer.facts row tagged KindLaunch (ADR-0135
// §SD6): the accepted `runtime.windowhost.open` request beside its app-lifecycle
// "started" row. Target app / run reuse the MembRuntimeApp /
// MembRuntimeRun identities; the caller rides MembLaunchCaller as a
// mixed-low-card-ref; the tile key reuses MembLifecycleTileKey so the
// row joins the lifecycle row on one column; the raw config bytes land
// on the blob section (nil for a plain open — no blob attribute); a row
// the host resolved from a kind (ADR-0361 §SD2) carries
// MembLaunchResolvedFromKind on the bool section.
func (inst *Store) WriteLaunch(ctx context.Context, row factsstore.LaunchRow) (id uint64, err error) {
	id, err = inst.mintId(ctx)
	if err != nil {
		return
	}
	ts := defaultTs(row.Ts)
	var tk [8]byte
	for i := range 8 {
		tk[i] = byte(row.TileKey >> (8 * (7 - i)))
	}
	nk := naturalKeyFor("launch", row.TargetAppId, []byte(row.RunId), tk[:])
	ent := dml.NewInEntityFacts(inst.allocator, 1)
	ent.BeginEntity().SetId(id, nk).SetTimestamp(ts)

	sym := ent.GetSectionSymbol()
	sym.BeginAttribute("launch").AddMembershipLowCardRef(vocab.MembKindLaunch.GetId().Value()).EndAttribute()
	stampApp(sym, row.TargetAppId)
	sym.BeginAttribute(string(row.CallerAppId)).AddMembershipLowCardRef(vocab.MembLaunchCaller.GetId().Value()).EndAttribute()
	inst.stampRun(sym, row.RunId)
	if row.ConfigKind != "" {
		sym.BeginAttribute(row.ConfigKind).AddMembershipLowCardRef(vocab.MembLaunchConfigKind.GetId().Value()).EndAttribute()
	}
	sym.EndSection()

	u64 := ent.GetSectionU64Array()
	stampInstance(u64, row.TileKey)
	u64.EndSection()

	if len(row.Config) > 0 {
		blob := ent.GetSectionBlobArray()
		blob.BeginAttributeSingle(row.Config).AddMembershipLowCardRef(vocab.MembLaunchConfig.GetId().Value()).EndAttribute()
		blob.EndSection()
	}

	if row.ResolvedFromKind {
		bsec := ent.GetSectionBool()
		bsec.BeginAttribute(true).AddMembershipLowCardRef(vocab.MembLaunchResolvedFromKind.GetId().Value()).EndAttribute()
		bsec.EndSection()
	}

	err = inst.commitAndShip(ctx, ent)
	return
}

// quoteSqlString single-quotes s for inline SQL, escaping single quotes by
// doubling. Used for the membership-keyed equality predicates that compare
// caller-controlled appId/key values; those are not amenable to FORMAT-time
// parameter binding over the HTTP interface.
func quoteSqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Count is a test helper — total rows in the read relation.
func (inst *Store) Count(ctx context.Context) (n uint64, err error) {
	body, err := inst.cli.Query(ctx, "SELECT count() FROM "+inst.readTable()+" FORMAT TabSeparated")
	if err != nil {
		return
	}
	defer func() { _ = body.Close() }()
	var raw [32]byte
	read, _ := body.Read(raw[:])
	_, err = fmt.Sscanf(strings.TrimSpace(string(raw[:read])), "%d", &n)
	if err != nil {
		err = eh.Errorf("chstore: parse count: %w", err)
		return
	}
	return
}

// Truncate is a test helper — removes all rows. Under an insert transform
// for the table it is refused with [recordstore.ErrTransformedTable]: the
// storage is the deployment's (ADR-0402 §SD5).
func (inst *Store) Truncate(ctx context.Context) (err error) {
	if err = refuseTransformed(inst.cfg, "TRUNCATE"); err != nil {
		return
	}
	err = inst.cli.Exec(ctx, "TRUNCATE TABLE IF EXISTS "+inst.writeTable())
	return
}

// DropTable is a test helper — removes the table entirely so a
// subsequent SetupTable recreates it from the current DDL. Truncate
// only clears rows, which leaves the column schema in place; tests
// that span schema migrations need a real drop to pick up new
// columns. Under an insert transform for the table it is refused, as
// Truncate is.
func (inst *Store) DropTable(ctx context.Context) (err error) {
	if err = refuseTransformed(inst.cfg, "DROP TABLE"); err != nil {
		return
	}
	err = inst.cli.Exec(ctx, "DROP TABLE IF EXISTS "+inst.writeTable())
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

// WriteTimeout bounds one insert whose ctx carries no deadline. The writes
// run during the host's shutdown drain as well — the heartbeat's last tick,
// the log bridge's final drain — and there the drain's ctx bounds them; a
// caller that hands a ctx without a deadline still gets a bounded wait, so a
// server that stopped answering cannot hold a write forever. A write cut
// short fails as any failed insert does.
const WriteTimeout = 3 * time.Second

// boundWrite is ctx, with WriteTimeout as its deadline when it has none.
func boundWrite(ctx context.Context) (bounded context.Context, cancel context.CancelFunc) {
	if _, has := ctx.Deadline(); has {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, WriteTimeout)
}

// shipRecords TransferRecords, InsertArrows, and releases the records.
// Entities must already be committed (commitAndShip commits the single-row
// case; WriteLogs commits each row before calling this). Returns the first
// error encountered. The insert runs under ctx, bounded by boundWrite.
func (inst *Store) shipRecords(ctx context.Context, ent *dml.InEntityFacts) (err error) {
	ctx, cancel := boundWrite(ctx)
	defer cancel()
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
	// Through the executor, which applies a transform registered for the
	// facts table (ADR-0402 §SD1): the generated stores' inserts take the
	// same path.
	err = inst.ship.InsertArrow(ctx, inst.writeTable(), records)
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
	for i := range 8 {
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
	for i := range 8 {
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
	for i := range 8 {
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
