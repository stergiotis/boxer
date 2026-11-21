//go:build integration

// Package factsview is the harness of the facts-read-through-a-view trial
// (../README.md). It loads the same facts into the facts table and into
// carrier tables behind views, runs the facts store's own reads and two SQL
// reads against each, and writes what the server reports about them under
// FRV_RUN_DIR; without FRV_RUN_DIR the tests skip. Every statement goes to
// the ClickHouse server the CLICKHOUSE_* variables name, into databases it
// creates and drops: never point it at a server whose load matters.
package factsview

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore/chstoretest"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

const (
	dbFacts   = "frv_facts"
	dbCarrier = "frv_carrier"
	idColumn  = "id:id:u64:47::0:"
	symbolLr  = "tv:symbol:lr:lr:u64:1247:::0::data"
	tenant    = "boxer"
	months    = 12
)

// arm is one relation the reads go to.
type arm struct {
	// name is the arm's label in every output file.
	name string
	// relation is what is read: the facts table, or a view over a carrier.
	relation string
	// table is the table that stores the rows.
	table string
}

var arms = []arm{
	{"table", dbFacts + ".facts", dbFacts + ".facts"},
	{"view-keyed", dbCarrier + ".keyed_view", dbCarrier + ".keyed"},
	{"view-tenant-key-only", dbCarrier + ".tenantonly_view", dbCarrier + ".tenantonly"},
	{"view-computed-ts", dbCarrier + ".computed_view", dbCarrier + ".computed"},
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func runDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("FRV_RUN_DIR")
	if dir == "" {
		t.Skip("FRV_RUN_DIR is not set; this harness runs through measure.sh")
	}
	return dir
}

func store(t *testing.T, readTable string) *chstore.Store {
	t.Helper()
	cfg := chstore.ConfigWithEnvDefaults(chstore.Config{Database: dbFacts, Table: "facts", RunId: "frv", ReadTable: readTable})
	s, err := chstore.New(cfg)
	require.NoError(t, err)
	require.NoError(t, s.Ping(context.Background()), "no ClickHouse server at %s", cfg.URL)
	return s
}

// server reaches the ClickHouse server the CLICKHOUSE_* variables name.
type server struct {
	t    *testing.T
	cli  *chclient.Client
	exec *storeexec.Executor
}

func connect(t *testing.T) (srv server) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	cli := chclient.New(cfg, nil)
	require.NoError(t, cli.Ping(context.Background()), "no ClickHouse server at %s", cfg.URL)
	exec, err := storeexec.New(cli, nil)
	require.NoError(t, err)
	return server{t, cli, exec}
}

func (inst server) run(sql string) {
	inst.t.Helper()
	require.NoError(inst.t, inst.cli.Exec(context.Background(), sql), sql[:min(len(sql), 160)])
}

func (inst server) query(sql string) string {
	inst.t.Helper()
	body, err := inst.cli.Query(context.Background(), sql+" FORMAT TabSeparated")
	require.NoError(inst.t, err, sql[:min(len(sql), 160)])
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(inst.t, err)
	return strings.TrimRight(string(raw), "\n")
}

// base is the first instant of the months the rows span: the twelve months
// ending with November 2026.
var base = time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

// TestLoad builds the four arms. The facts table is written through the
// facts store, one month per insert block; the carriers are copies of it,
// each holding the same rows twice — once for the tenant the view selects,
// once for another.
func TestLoad(t *testing.T) {
	_ = runDir(t)
	srv := connect(t)
	ctx := context.Background()
	rows := envInt("FRV_ROWS", 2_000_000)
	const batch = 10_000

	srv.run("DROP DATABASE IF EXISTS " + dbFacts + " SYNC")
	srv.run("DROP DATABASE IF EXISTS " + dbCarrier + " SYNC")
	s := store(t, "")
	require.NoError(t, s.SetupTable(ctx, ""))

	perMonth := rows / months
	levels := []string{"info", "info", "info", "debug", "warn", "error"}
	buf := make([]factsstore.LogRow, 0, batch)
	for m := range months {
		from := base.AddDate(0, m, 0)
		span := from.AddDate(0, 1, 0).Sub(from)
		for i := range perMonth {
			at := from.Add(time.Duration(int64(span) / int64(perMonth) * int64(i)))
			buf = append(buf, factsstore.LogRow{
				AppId: app.AppIdT("app" + strconv.Itoa(i%7)), Level: levels[i%len(levels)], Service: "svc" + strconv.Itoa(i%3),
				Message: "message " + strconv.Itoa(i) + " of month " + strconv.Itoa(m), Ts: at, InstanceKey: uint64(i % 5),
			})
			if len(buf) == batch || i == perMonth-1 {
				_, err := s.WriteLogs(ctx, buf)
				require.NoError(t, err)
				buf = buf[:0]
			}
		}
	}
	// A second, rare kind: one heartbeat per day.
	for d := range 365 {
		_, err := s.WriteRuntimeHeartbeat(ctx, factsstore.HeartbeatRow{RunId: "frv", Ts: base.AddDate(0, 0, d).Add(12 * time.Hour)})
		require.NoError(t, err)
	}

	contract, err := chstore.SchemaContract(chstore.Config{Database: dbFacts, Table: "facts"})
	require.NoError(t, err)
	carrier := func(name string, engine func(c *chstoretest.Carrier) string) *chstoretest.Carrier {
		c := chstoretest.NewCarrier(contract.Columns, dbCarrier+"."+name, dbCarrier+"."+name+"_view", tenant)
		c.Engine = engine(c)
		return c
	}
	ts := func(c *chstoretest.Carrier) string { return c.FactsColumn(factsschema.TsColumn) }
	keyed := carrier("keyed", func(c *chstoretest.Carrier) string {
		return "MergeTree PARTITION BY toYYYYMM(" + ts(c) + ") ORDER BY (tenant, " + ts(c) + ")"
	})
	tenantOnly := carrier("tenantonly", func(*chstoretest.Carrier) string { return chstoretest.DefaultEngine })
	computed := carrier("computed", func(c *chstoretest.Carrier) string {
		return "MergeTree PARTITION BY toYYYYMM(fromUnixTimestamp64Nano(" + ts(c) + ", 'UTC')) ORDER BY (tenant, " + ts(c) + ")"
	})
	const copySettings = " SETTINGS max_partitions_per_insert_block = 0, insert_deduplicate = 0"
	for _, c := range []*chstoretest.Carrier{keyed, tenantOnly} {
		for _, stmt := range append(c.SetupStatements(), c.ViewStatement(nil, nil)) {
			srv.run(stmt)
		}
		for _, who := range []string{tenant, "another"} {
			srv.run("INSERT INTO " + c.Table + " SELECT '" + who + "', * FROM " + dbFacts + ".facts" + copySettings)
		}
	}
	// The carrier that keeps the timestamp as nanoseconds in an Int64, with
	// a view that computes the facts timestamp back.
	tsCol := ts(computed)
	tsType := ""
	for _, col := range contract.Columns {
		if col.Name == factsschema.TsColumn {
			tsType = col.Type
		}
	}
	stmts := computed.SetupStatements()
	create := stmts[len(stmts)-1]
	require.Contains(t, create, tsCol+" "+tsType+",")
	create = strings.Replace(create, tsCol+" "+tsType+",", tsCol+" Int64,", 1)
	for _, stmt := range stmts[:len(stmts)-1] {
		srv.run(stmt)
	}
	srv.run(create)
	view := computed.ViewStatement(nil, nil)
	asTs := tsCol + " AS `" + factsschema.TsColumn + "`"
	require.Contains(t, view, asTs)
	srv.run(strings.Replace(view, asTs, "fromUnixTimestamp64Nano("+tsCol+", 'UTC') AS `"+factsschema.TsColumn+"`", 1))
	for _, who := range []string{tenant, "another"} {
		srv.run("INSERT INTO " + computed.Table + " SELECT '" + who + "', * REPLACE (toUnixTimestamp64Nano(`" + factsschema.TsColumn + "`) AS `" + factsschema.TsColumn + "`) FROM " + dbFacts + ".facts" + copySettings)
	}
	for _, a := range arms {
		srv.run("OPTIMIZE TABLE " + a.table + " FINAL")
	}
}

// TestTables records what each arm's table holds.
func TestTables(t *testing.T) {
	dir := runDir(t)
	srv := connect(t)
	var b strings.Builder
	b.WriteString("arm\trelation\ttable\trows\tactive_parts\tpartitions\tbytes_on_disk\tsorting_key\tpartition_key\n")
	for _, a := range arms {
		db, name, _ := strings.Cut(a.table, ".")
		where := " WHERE database = '" + db + "' AND table = '" + name + "' AND active"
		b.WriteString(strings.Join([]string{a.name, a.relation, a.table,
			srv.query("SELECT sum(rows) FROM system.parts" + where),
			srv.query("SELECT count() FROM system.parts" + where),
			srv.query("SELECT uniqExact(partition_id) FROM system.parts" + where),
			srv.query("SELECT sum(bytes_on_disk) FROM system.parts" + where),
			srv.query("SELECT sorting_key FROM system.tables WHERE database = '" + db + "' AND name = '" + name + "'"),
			srv.query("SELECT partition_key FROM system.tables WHERE database = '" + db + "' AND name = '" + name + "'"),
		}, "\t") + "\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tables.tsv"), []byte(b.String()), 0o644))
}

// TestReadCost runs each read against each arm and records, from
// system.query_log, what the server read to answer it.
func TestReadCost(t *testing.T) {
	dir := runDir(t)
	reps := envInt("FRV_REPS", 5)
	srv := connect(t)
	ctx := context.Background()

	// The reads' parameters, taken from the data so that every arm answers
	// the same question.
	hourFrom := base.AddDate(0, months-1, 14).Add(9 * time.Hour)
	dayFrom := base.AddDate(0, months-1, 14)
	pointId := srv.query("SELECT `" + idColumn + "` FROM " + dbFacts + ".facts WHERE `" + factsschema.TsColumn + "` >= toDateTime64('" + hourFrom.Format("2006-01-02 15:04:05") + "', 9, 'UTC') ORDER BY `" + factsschema.TsColumn + "` LIMIT 1")
	require.NotEmpty(t, pointId)
	kind := strconv.FormatUint(vocab.MembKindRuntimeHeartbeat.GetId().Value(), 10)
	dayLit := func(d time.Time) string { return "toDateTime64('" + d.Format("2006-01-02 15:04:05") + "', 9, 'UTC')" }

	type read struct {
		name string
		// run performs the read under ctx and returns how many rows it gave.
		run func(ctx context.Context, a arm) (rows int, err error)
	}
	sqlCount := func(where string) func(ctx context.Context, a arm) (int, error) {
		return func(ctx context.Context, a arm) (int, error) {
			n := 0
			for rec, err := range srv.exec.QueryArrow(ctx, "SELECT count() AS n FROM "+a.relation+" WHERE "+where) {
				if err != nil {
					return 0, err
				}
				n = int(rec.Column(0).(*array.Uint64).Value(0))
				rec.Release()
			}
			return n, nil
		}
	}
	logRows := func(from time.Time, d time.Duration) func(ctx context.Context, a arm) (int, error) {
		return func(ctx context.Context, a arm) (int, error) {
			rows, err := store(t, a.relation).LogRows(ctx, factsstore.LogRowsFilter{Since: from, Until: from.Add(d), Limit: 1_000_000})
			return len(rows), err
		}
	}
	ts := "`" + factsschema.TsColumn + "`"
	reads := []read{
		{"logrows-one-hour", logRows(hourFrom, time.Hour)},
		{"logrows-one-day", logRows(dayFrom, 24*time.Hour)},
		{"point-by-id", sqlCount("`" + idColumn + "` = " + pointId)},
		{"point-by-id-within-its-day", sqlCount("`" + idColumn + "` = " + pointId + " AND " + ts + " >= " + dayLit(dayFrom) + " AND " + ts + " < " + dayLit(dayFrom.AddDate(0, 0, 1)))},
		{"kind-all-time", sqlCount("has(`" + symbolLr + "`, " + kind + ")")},
		{"kind-one-month", sqlCount("has(`" + symbolLr + "`, " + kind + ") AND " + ts + " >= " + dayLit(base.AddDate(0, months-1, 0)) + " AND " + ts + " < " + dayLit(base.AddDate(0, months, 0)))},
	}

	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	type key struct{ arm, read string }
	answers := map[key]int{}
	for _, r := range reads {
		for _, a := range arms {
			for rep := range reps {
				// The query condition cache remembers which granules a
				// predicate matched; with it a repeated read is cheaper than
				// the keys alone make it. The trial asks about the keys.
				srv.run("SYSTEM DROP QUERY CONDITION CACHE")
				id := recordstore.BatchIdT(fmt.Sprintf("frv-%s-%s-%s-%d", stamp, a.name, r.name, rep))
				n, err := r.run(recordstore.WithBatchId(ctx, id), a)
				require.NoError(t, err, "%s on %s", r.name, a.name)
				answers[key{a.name, r.name}] = n
			}
		}
	}
	for _, r := range reads {
		want := answers[key{arms[0].name, r.name}]
		for _, a := range arms[1:] {
			require.Equal(t, want, answers[key{a.name, r.name}], "%s: arm %s answers differently from the table", r.name, a.name)
		}
	}

	srv.run("SYSTEM FLUSH LOGS")
	var b strings.Builder
	b.WriteString("read\tarm\trep\tresult_rows\tread_rows\tread_bytes\tselected_parts\tselected_marks\tduration_ms\n")
	for _, r := range reads {
		for _, a := range arms {
			for rep := range reps {
				id := fmt.Sprintf("frv-%s-%s-%s-%d", stamp, a.name, r.name, rep)
				line := srv.query("SELECT read_rows, read_bytes, ProfileEvents['SelectedParts'], ProfileEvents['SelectedMarks'], query_duration_ms FROM system.query_log" +
					" WHERE type = 'QueryFinish' AND event_date >= yesterday() AND position(log_comment, '" + id + "') > 0 ORDER BY event_time_microseconds DESC LIMIT 1")
				require.NotEmpty(t, line, "no query_log row for %s", id)
				b.WriteString(r.name + "\t" + a.name + "\t" + strconv.Itoa(rep) + "\t" + strconv.Itoa(answers[key{a.name, r.name}]) + "\t" + line + "\n")
			}
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cost.tsv"), []byte(b.String()), 0o644))

	// The plans, as evidence of which key each read used.
	var plans strings.Builder
	for _, a := range arms {
		for _, r := range []struct{ name, where string }{
			{"one-day", ts + " >= " + dayLit(dayFrom) + " AND " + ts + " < " + dayLit(dayFrom.AddDate(0, 0, 1))},
			{"point-by-id", "`" + idColumn + "` = " + pointId},
		} {
			plans.WriteString("## " + a.name + " — " + r.name + "\n\n")
			plans.WriteString(srv.query("EXPLAIN indexes = 1 SELECT count() FROM "+a.relation+" WHERE "+r.where) + "\n\n")
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plans.txt"), []byte(plans.String()), 0o644))
}
