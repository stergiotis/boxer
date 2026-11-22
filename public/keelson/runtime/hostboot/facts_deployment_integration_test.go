//go:build integration

package hostboot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore/chstoretest"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// These tests boot a host against a server on which the facts are stored in
// a deployment's own table (ADR-0402): the reference carrier of chstoretest,
// its transform registered for boxer.facts, and its view named boxer.facts.
//
// They use the runtime's own names, drop and create the boxer database, and
// so run only on a throwaway server: one that carries the marker database
// scripts/ci/clickhouse_server.sh creates. On any other server they skip.
//
// The registry of insert transforms and the environment are a process's, so
// each host is a child of this test binary.

const (
	deployChildKey  = "BOXER_HOSTBOOT_IT_DEPLOYMENT"
	deployMarker    = "DEPLOYMENT\t"
	throwawayMarker = "boxer_throwaway_server"
	carrierDatabase = "boxer_it_carrier"
	carrierTable    = carrierDatabase + ".rows"
	carrierTenant   = "boxer"
	deployMessage   = "written by the deployment test"
)

// deploySummary is what a child host reports about itself.
type deploySummary struct {
	Mode         string
	BootErr      string
	IsChStore    bool
	FactsBackend string
	TrailDurable bool
	TrailWhy     string
	LogId        uint64
	WriteErr     string
	ReadBack     int
	ReadErr      string
	DitchErr     string
	DitchRefused bool
	TrailWritten uint64
	TrailDropped uint64
	Schemas      []recordstore.SchemaVerdict
}

func throwawayClient(t *testing.T) (cli *chclient.Client) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	cli = chclient.New(cfg, nil)
	ctx := context.Background()
	if err := cli.Ping(ctx); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	if serverScalar(t, cli, "SELECT count() FROM system.databases WHERE name = '"+throwawayMarker+"'") != "1" {
		t.Skipf("the server at %s is not a throwaway one (no %s database); start one with scripts/ci/clickhouse_server.sh and point CLICKHOUSE_URL at it", cfg.URL, throwawayMarker)
	}
	return
}

func serverScalar(t *testing.T, cli *chclient.Client, sql string) string {
	t.Helper()
	body, err := cli.Query(context.Background(), sql+" FORMAT TabSeparated")
	require.NoError(t, err, sql)
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	return strings.TrimSpace(string(raw))
}

func referenceCarrier(t *testing.T) *chstoretest.Carrier {
	t.Helper()
	c, err := chstore.SchemaContract(chstore.Defaults())
	require.NoError(t, err)
	return chstoretest.NewCarrier(c.Columns, carrierTable, factsschema.QualifiedTableName, carrierTenant)
}

// TestFactsDeploymentChild is one host process of the deployment tests. In
// mode "carrier" it is a deployment's binary: it registers the transform
// before it boots. In mode "stock" it is a binary that does not carry the
// transform. It is skipped in a normal run.
func TestFactsDeploymentChild(t *testing.T) {
	mode := os.Getenv(deployChildKey)
	if mode == "" {
		t.Skip("child-process helper for the deployment tests")
	}
	ctx := context.Background()
	sum := deploySummary{Mode: mode}
	defer func() {
		raw, err := json.Marshal(sum)
		require.NoError(t, err)
		fmt.Printf("\n%s%s\n", deployMarker, raw)
	}()
	if mode == "carrier" {
		recordstore.RegisterInsertTransform(factsschema.QualifiedTableName, referenceCarrier(t).Transform())
	}

	logger := zerolog.New(os.Stderr).Level(zerolog.WarnLevel)
	rt, err := Boot(ctx, Options{
		Log:           logger,
		Registry:      app.NewRegistry(),
		NoWindowHost:  true,
		KeepCoreDumps: true,
		Services:      Services{Persist: true},
	})
	if err != nil {
		sum.BootErr = err.Error()
		return
	}
	defer rt.Close(ctx)
	sum.IsChStore = rt.IsChStore
	sum.FactsBackend = rt.Status.FactsBackend
	sum.TrailDurable = rt.Trail.Durable()
	sum.TrailWhy = rt.Trail.NotDurable()
	sum.Schemas = rt.Schemas.Verdicts()

	id, werr := rt.Facts.WriteLog(ctx, factsstore.LogRow{AppId: "deployment.test", Level: "info", Message: deployMessage, Ts: time.Now().UTC()})
	sum.LogId = id
	if werr != nil {
		sum.WriteErr = werr.Error()
	}
	if st, ok := rt.Facts.(*chstore.Store); ok {
		rows, rerr := st.LogRows(ctx, factsstore.LogRowsFilter{MessageContains: deployMessage})
		sum.ReadBack = len(rows)
		if rerr != nil {
			sum.ReadErr = rerr.Error()
		}
	}
	// The removal of kept text: refused under a transform, and the refusal
	// recorded on the trail as a failed ditch, through the transform.
	if derr := rt.Trail.DitchBodies(ctx, "deployment.test"); derr != nil {
		sum.DitchErr = derr.Error()
		sum.DitchRefused = strings.Contains(derr.Error(), recordstore.ErrTransformedTable.Error())
	}
	_ = rt.Trail.Flush(ctx)
	counts := rt.Trail.Counts()
	sum.TrailWritten, sum.TrailDropped = counts.Written, counts.Dropped
}

// runDeployment boots one child host in mode and returns what it reported
// and everything it printed.
func runDeployment(t *testing.T, mode string, extraEnv ...string) (sum deploySummary, output string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFactsDeploymentChild$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), deployChildKey+"="+mode)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	output = string(out)
	require.NoError(t, err, output)
	for l := range strings.SplitSeq(output, "\n") {
		if after, ok := strings.CutPrefix(l, deployMarker); ok {
			require.NoError(t, json.Unmarshal([]byte(strings.TrimRight(after, "\r")), &sum), after)
			return
		}
	}
	require.FailNow(t, "the child host reported nothing", output)
	return
}

func verdictFor(sum deploySummary, table string) (v recordstore.SchemaVerdict, ok bool) {
	for _, s := range sum.Schemas {
		if s.Table == table {
			return s, true
		}
	}
	return
}

// resetDeployment leaves the throwaway server with the deployment's storage
// and nothing of boxer's: the carrier, and the view named boxer.facts.
func resetDeployment(t *testing.T, cli *chclient.Client) (carrier *chstoretest.Carrier) {
	t.Helper()
	ctx := context.Background()
	for _, db := range []string{factsschema.DatabaseName, carrierDatabase} {
		require.NoError(t, cli.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" SYNC"))
	}
	t.Cleanup(func() {
		for _, db := range []string{factsschema.DatabaseName, carrierDatabase} {
			_ = cli.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" SYNC")
		}
	})
	carrier = referenceCarrier(t)
	for _, stmt := range append(carrier.SetupStatements(), carrier.ViewStatement(nil, nil)) {
		require.NoError(t, cli.Exec(ctx, stmt), stmt[:min(len(stmt), 80)])
	}
	return
}

// A deployment's host boots on storage of its own: nothing of boxer's is
// created under the name boxer.facts, every fact it writes is a carrier row
// under a leased id, its readers read them back through the view, the
// schema check judges the view by its columns, and what assumes a table
// boxer owns is refused.
func TestFactsDeployment_AHostOverACarrier(t *testing.T) {
	cli := throwawayClient(t)
	resetDeployment(t, cli)

	sum, output := runDeployment(t, "carrier")
	require.Empty(t, sum.BootErr, output)
	require.True(t, sum.IsChStore, "the host reached the server and kept the ClickHouse store\n%s", output)
	assert.True(t, sum.TrailDurable, "the trail is durable: %s", sum.TrailWhy)
	require.Empty(t, sum.WriteErr)
	require.Empty(t, sum.ReadErr)
	assert.Equal(t, 1, sum.ReadBack, "the store's reader finds the row through the view")
	assert.True(t, sum.DitchRefused, "the removal of kept text is refused under a transform: %s", sum.DitchErr)
	assert.Zero(t, sum.TrailDropped)
	assert.NotZero(t, sum.TrailWritten, "trail rows, the failed ditch among them, landed through the transform")
	assert.Equal(t, vocab.IdStreamFactsRow.Value(), identifier.TaggedId(sum.LogId).GetTag().GetValue(), "leased from boxer.idlease, which setup still provisions")

	v, ok := verdictFor(sum, factsschema.QualifiedTableName)
	require.True(t, ok, "the read relation is on the schema board: %+v", sum.Schemas)
	assert.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.False(t, v.LayoutDiffers, "a view is judged by its columns only: %s", v.Summary())

	// On the server: the name is still the deployment's view, and the rows
	// are in the carrier.
	assert.Equal(t, "View", serverScalar(t, cli, "SELECT engine FROM system.tables WHERE database = 'boxer' AND name = 'facts'"))
	n := serverScalar(t, cli, "SELECT count() FROM "+carrierTable+" WHERE tenant = '"+carrierTenant+"'")
	assert.NotEqual(t, "0", n)
	assert.Equal(t, n, serverScalar(t, cli, "SELECT count() FROM boxer.facts"), "SQL typed against boxer.facts returns the facts")
	assert.Equal(t, "1", serverScalar(t, cli, "SELECT count() FROM system.tables WHERE database = 'boxer' AND name = 'idlease'"))
	assert.Equal(t, "0", serverScalar(t, cli, "SELECT count() FROM system.tables WHERE database = 'boxer' AND engine = 'MaterializedView'"),
		"a host installs no query-run view; queryrunsd does")
	assert.Equal(t, "1", serverScalar(t, cli, "SELECT count() FROM boxer.facts WHERE `id:id:u64:47::0:` = "+fmt.Sprint(sum.LogId)))

	// A second host of the same deployment starts on what the first left.
	again, output := runDeployment(t, "carrier")
	require.Empty(t, again.BootErr, output)
	require.Empty(t, again.WriteErr)
	assert.Equal(t, 2, again.ReadBack, "both hosts' rows, from the one carrier")
	assert.NotEqual(t, sum.LogId, again.LogId)
}

// A binary that does not carry the deployment's transform cannot write
// facts there, and its host says so instead of looking healthy: setup finds
// a view under the name it writes to, the facts store falls back to memory
// as it does for any failed setup, and the trail reports that it is not
// durable.
func TestFactsDeployment_AStockBinaryFailsLoudly(t *testing.T) {
	cli := throwawayClient(t)
	resetDeployment(t, cli)
	before := serverScalar(t, cli, "SELECT count() FROM "+carrierTable)

	sum, output := runDeployment(t, "stock")
	require.Empty(t, sum.BootErr, output)
	assert.False(t, sum.IsChStore, "the host must not report a ClickHouse-backed facts store\n%s", output)
	assert.Equal(t, "mem", sum.FactsBackend)
	assert.False(t, sum.TrailDurable)
	assert.NotEmpty(t, sum.TrailWhy)
	assert.Contains(t, output, chstore.ErrFactsTableTakesNoInsert.Error(), "the log names what the binary lacks")
	assert.Equal(t, before, serverScalar(t, cli, "SELECT count() FROM "+carrierTable), "nothing of it reached the carrier")
	assert.Equal(t, "View", serverScalar(t, cli, "SELECT engine FROM system.tables WHERE database = 'boxer' AND name = 'facts'"), "it did not replace the view")
}

// A deployment that leaves boxer.facts alone and names its view with
// BOXER_FACTS_READ_TABLE: the host reads through the setting.
func TestFactsDeployment_AReadTableSetting(t *testing.T) {
	cli := throwawayClient(t)
	carrier := resetDeployment(t, cli)
	ctx := context.Background()
	// The view under another name; nothing is named boxer.facts.
	require.NoError(t, cli.Exec(ctx, "DROP VIEW boxer.facts SYNC"))
	carrier.View = carrierDatabase + ".facts_view"
	require.NoError(t, cli.Exec(ctx, carrier.ViewStatement(nil, nil)))

	sum, output := runDeployment(t, "carrier", "BOXER_FACTS_READ_TABLE="+carrier.View)
	require.Empty(t, sum.BootErr, output)
	require.True(t, sum.IsChStore, output)
	require.Empty(t, sum.WriteErr)
	require.Empty(t, sum.ReadErr)
	assert.Equal(t, 1, sum.ReadBack)
	assert.Equal(t, "0", serverScalar(t, cli, "SELECT count() FROM system.tables WHERE database = 'boxer' AND name = 'facts'"),
		"setup created no table of that name beside the deployment's storage")
	v, ok := verdictFor(sum, carrier.View)
	require.True(t, ok, "%+v", sum.Schemas)
	assert.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())

	// A view that lacks a facts column is reported by the schema check.
	require.NoError(t, cli.Exec(ctx, carrier.ViewStatement([]string{factsschema.TsColumn}, nil)))
	broken, output := runDeployment(t, "carrier", "BOXER_FACTS_READ_TABLE="+carrier.View)
	bv, ok := verdictFor(broken, carrier.View)
	require.True(t, ok, output)
	assert.NotEqual(t, recordstore.SchemaVerdictOk, bv.Verdict, bv.Summary())
	assert.Contains(t, bv.Missing, factsschema.TsColumn)

	// So is a view that returns a facts column under another type: here the
	// timestamp in seconds.
	require.NoError(t, cli.Exec(ctx, carrier.ViewStatement(nil, map[string]string{factsschema.TsColumn: "DateTime('UTC')"})))
	retyped, output := runDeployment(t, "carrier", "BOXER_FACTS_READ_TABLE="+carrier.View)
	rv, ok := verdictFor(retyped, carrier.View)
	require.True(t, ok, output)
	assert.Equal(t, recordstore.SchemaVerdictIncompatible, rv.Verdict, rv.Summary())
	require.NoError(t, cli.Exec(ctx, carrier.ViewStatement([]string{factsschema.TsColumn}, nil)))

	// Under BOXER_SCHEMA_STRICT that verdict fails the boot.
	strict, output := runDeployment(t, "carrier", "BOXER_FACTS_READ_TABLE="+carrier.View, "BOXER_SCHEMA_STRICT=1")
	assert.NotEmpty(t, strict.BootErr, output)

	// A malformed setting fails the boot whatever else is right.
	malformed, _ := runDeployment(t, "carrier", "BOXER_FACTS_READ_TABLE=facts_view")
	assert.Contains(t, malformed.BootErr, "BOXER_FACTS_READ_TABLE")
}
