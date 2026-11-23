//go:build integration

package trivialsql

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each statement of columnCases and tableCases is answered by
// clickhouse-local and by the evaluator, and the pair must fall in the
// class its case names. The classes hold for the ClickHouse on the PATH;
// they were first recorded against 26.9.

func clickhouseLocal(t *testing.T, sql string) (out string, failed bool) {
	t.Helper()
	cmd := exec.Command("clickhouse-local", "--query", sql)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if i := strings.Index(msg, ". In scope"); i >= 0 {
			msg = msg[:i]
		}
		return strings.TrimSpace(msg), true
	}
	return stdout.String(), false
}

// parityClass names how the evaluator's answer relates to ClickHouse's.
func parityClass(ch string, chFailed bool, body []byte, err error) string {
	isRefusal := errors.Is(err, ErrNeedsClickHouse)
	switch {
	case err == nil && !chFailed && string(body) == ch:
		return same
	case isRefusal && !chFailed:
		return refused
	case isRefusal && chFailed:
		return refusedCH
	case err != nil && chFailed:
		return bothError
	case err == nil && chFailed:
		return "ANSWERS WHAT CLICKHOUSE REJECTS"
	case err != nil:
		return "ERRORS ON WHAT CLICKHOUSE ANSWERS"
	}
	return "DIVERGES"
}

func withFormat(sql string) string {
	if strings.Contains(sql, " FORMAT ") {
		return sql
	}
	return sql + " FORMAT TabSeparatedWithNames"
}

func TestColumns_AnswerAsClickHouseDoes(t *testing.T) {
	if _, err := exec.LookPath("clickhouse-local"); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	for _, c := range columnCases {
		sql := withFormat(c.sql)
		ch, chFailed := clickhouseLocal(t, sql)
		body, err := Run(context.Background(), reg(t), sql, nil)
		assert.Equal(t, c.want, parityClass(ch, chFailed, body, err), "%s\nclickhouse: %q\nevaluator:  %q %v", c.sql, ch, body, err)
	}
}

// A statement over a bound table is answered as ClickHouse answers it with
// the table before it as a WITH query.
func TestReadTables_AnswerAsClickHouseDoes(t *testing.T) {
	if _, err := exec.LookPath("clickhouse-local"); err != nil {
		t.Skipf("clickhouse-local not installed: %v", err)
	}
	tables := boundTables(t)
	for _, c := range tableCases {
		ch, chFailed := clickhouseLocal(t, withFormat(boundAsWith(c.sql)))
		var body []byte
		ans, err := ReadTables(t.Context(), reg(t), c.sql, nil, tables)
		if err == nil {
			body, err = Encode(ans.Main, FormatTabSeparatedWithNames)
			ans.Release()
			require.NoError(t, err)
		}
		assert.Equal(t, c.want, parityClass(ch, chFailed, body, err), "%s\nclickhouse: %q\nevaluator:  %q %v", c.sql, ch, body, err)
	}
}

// boundAsWith is sql with the bound table t written before it as a WITH
// query: the statement ReadTables claims to answer as ClickHouse would.
func boundAsWith(sql string) string {
	const t = "t AS (SELECT * FROM " + t3 + ")"
	if len(sql) > 5 && sql[:5] == "WITH " {
		return "WITH " + t + ", " + sql[5:]
	}
	return "WITH " + t + " " + sql
}
