package fairlead

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
)

func TestReportFoldsJournalsPerSiteAndClassification(t *testing.T) {
	first := strings.Join([]string{
		`{"site":"a.post","exit":"server","sql":"SELECT * FROM t"}`,
		`{"site":"a.post","exit":"server","sql":"SELECT * FROM u LIMIT 3"}`,
		`{"site":"a.post","exit":"server","sql":"SELECT count() FROM t"}`,
		`{"site":"a.post","exit":"server","sql":"CREATE TABLE t (a UInt8) ENGINE = Memory"}`,
		`{"site":"b.local","exit":"local","sql":"SELECT a FROM t WHERE a > 1"}`,
		`{"site":"b.local","exit":"local","sql":"CREATE TEMPORARY TABLE s AS SELECT 1 AS a; SELECT * FROM s"}`,
		`{"site":"c.meta","exit":"server","sql":"DESCRIBE (SELECT a FROM t WHERE a > 1) FORMAT ArrowStream"}`,
		`{"site":"c.meta","exit":"server","sql":"EXISTS TABLE db.t"}`,
	}, "\n") + "\n"
	// A second process saw one statement again, one new one, and was killed
	// mid-line.
	second := `{"site":"a.post","exit":"server","sql":"SELECT * FROM t"}` + "\n" +
		`{"site":"b.local","exit":"local","sql":"SELECT b FROM t"}` + "\n" +
		`{"site":"b.local","exit":"local","sql":"SELECT a FROM t WHERE a > 22"}` + "\n" +
		`{"site":"b.local","exit":"lo`

	rep := NewReport()
	rep.Add(strings.NewReader(first))
	rep.Add(strings.NewReader(second))
	require.EqualValues(t, 1, rep.Torn())

	require.Equal(t, []ReportRow{
		{"a.post", "server", false, analysis.KindMutating, analysis.QuerySecurityMutating, analysis.QueryStratumUnplaced, 1, 1, false, false},
		{"a.post", "server", true, analysis.KindReadOnly, analysis.QuerySecurityRead, analysis.QueryStratumDerive, 1, 1, false, false},
		{"a.post", "server", true, analysis.KindReadOnly, analysis.QuerySecurityRead, analysis.QueryStratumVerbatim, 2, 2, false, false},
		{"b.local", "local", true, analysis.KindReadOnly, analysis.QuerySecurityRead, analysis.QueryStratumRestrict, 3, 2, false, false},
		{"b.local", "local", true, analysis.KindReadOnly, analysis.QuerySecurityRead, analysis.QueryStratumVerbatim, 1, 1, true, false},
		{"c.meta", "server", false, analysis.KindReadOnly, analysis.QuerySecurityRead, analysis.QueryStratumUnplaced, 1, 1, false, true},
		{"c.meta", "server", true, analysis.KindReadOnly, analysis.QuerySecurityRead, analysis.QueryStratumRestrict, 1, 1, false, true},
	}, rep.Rows())

	var out bytes.Buffer
	require.NoError(t, writeRows(&out, rep.Rows()))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Len(t, lines, 8)
	require.Equal(t, []string{"site", "exit", "parsed", "kind", "security", "stratum", "script", "introspects", "statements", "shapes"}, strings.Fields(lines[0]))
	require.Equal(t, []string{"a.post", "server", "true", "read-only", "read", "verbatim", "false", "false", "2", "2"}, strings.Fields(lines[3]))
}
