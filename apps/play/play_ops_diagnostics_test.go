package play

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// get_diagnostics reads the pane's sections: the statement's status, the
// security class with what raised it, the tables returned as stored, a
// measured rewrite's failures, the query graph and the last run.
func TestGetDiagnosticsReadsThePanesSections(t *testing.T) {
	p := &PlayApp{diag: NewDiagnosticsDriver(nil)}
	require.Equal(t, "empty", snapshotDiagnostics(p).Statement.Status)

	p.sql = "SELECT * FROM db.t"
	require.Equal(t, "settling", snapshotDiagnostics(p).Statement.Status)

	p.formattedFor = p.sql
	p.diag.noteParse(p.sql, nil)
	d := snapshotDiagnostics(p)
	require.Equal(t, "parses", d.Statement.Status)
	require.Equal(t, "read", d.Security.Class)
	require.Equal(t, []string{"db.t"}, d.Security.Passthrough)
	require.False(t, d.Rewrites.Measured, "nothing measured this buffer; get_diagnostics starts no measurement")
	require.Equal(t, "no run yet", d.QueryGraph)

	p.sql = "SELECT * FROM url('http://example.invalid/x.csv', CSV)"
	p.formattedFor = p.sql
	p.diag.noteParse(p.sql, nil)
	d = snapshotDiagnostics(p)
	require.Equal(t, "read-egress", d.Security.Class)
	require.NotEmpty(t, d.Security.Witnesses)
	require.Contains(t, d.Security.Witnesses[0], "url")

	p.sql = "SELEC 1"
	p.formattedFor, p.formattedErr = p.sql, errors.New("line 1: no viable alternative")
	p.diag.noteParse(p.sql, p.formattedErr)
	d = snapshotDiagnostics(p)
	require.Equal(t, "no-endpoint", d.Statement.Status, "no endpoint to ask")
	require.Contains(t, d.Statement.Parser, "no viable alternative")
	require.Equal(t, "unclassified", d.Security.Class)

	p.rewriteTrace.valid, p.rewriteTrace.forRaw = true, p.sql
	p.rewriteTrace.obs = []passreg.ApplyObservation{{Name: "lw_get", Outcome: passreg.ApplyOutcomeSkipped, Err: errors.New("no such section")}}
	d = snapshotDiagnostics(p)
	require.True(t, d.Rewrites.Measured)
	require.Len(t, d.Rewrites.Failed, 1)
	require.Equal(t, "lw_get", d.Rewrites.Failed[0].Pass)
	require.Equal(t, "no such section", d.Rewrites.Failed[0].Error)

	p.lastSentSql, p.splitErr = "SELECT 1", errors.New("two sinks")
	require.Contains(t, snapshotDiagnostics(p).QueryGraph, "two sinks")
}

func TestGetDiagnosticsCatalogEntry(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opGetDiagnostics)
	require.True(t, ok)
	require.Equal(t, 2, int(spec.Version))
	require.True(t, spec.Untrusted)
	require.Equal(t, app.OperationEffectNone, spec.Effect)
	require.Equal(t, []string{opsResSql, opsResResult, opsResSignals, opsResPanes}, spec.Reads)
	require.Empty(t, spec.Writes)
}

// A gloss directive that does not compile is a diagnostic of the buffer, as
// the Table pane notes it under its pager, whether or not the Table drew.
func TestGetDiagnosticsReportsGlossDirectivesThatDoNotCompile(t *testing.T) {
	p := &PlayApp{diag: NewDiagnosticsDriver(nil)}
	p.sql = "-- play: gloss no/such-type ^x$\nSELECT 1 AS x"
	d := snapshotDiagnostics(p)
	require.Len(t, d.GlossNotes, 1)
	require.Contains(t, d.GlossNotes[0], "line 1")

	p.sql = "SELECT 1 AS x"
	require.Empty(t, snapshotDiagnostics(p).GlossNotes)
}

// LastRun is of the result the panels draw: with the main result landed and
// nothing observed, its summary; a failed run gives its full error.
func TestGetDiagnosticsLastRunIsOfTheActiveFrame(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner

	rec := int64Rec("n", 1, 2, 3)
	p.graph.mainLane.finish("SELECT n", nil, time.Now(), rec, rec.Schema(), 3, Summary{}, nil, runstream.Terminal{})
	d := snapshotDiagnostics(p)
	require.False(t, d.LastRunError)
	require.NotEmpty(t, d.LastRun)

	p.graph.mainLane.finish("SELECT n", nil, time.Now(), nil, nil, 0, Summary{}, errors.New("Code: 60. Unknown table"), runstream.Terminal{})
	d = snapshotDiagnostics(p)
	require.True(t, d.LastRunError)
	require.Contains(t, d.LastRun, "Unknown table")
}
