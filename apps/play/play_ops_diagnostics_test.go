package play

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
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
