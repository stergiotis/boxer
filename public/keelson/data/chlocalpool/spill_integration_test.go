//go:build integration

package chlocalpool

// How a worker behaves when a statement outgrows MaxMemoryPerWorker. 26.4–26.9
// made joins and DISTINCT spill to disk by default, and 26.6 raised local
// temporary storage from 1 GiB to 1 TiB. The pool relies on a capped worker
// failing fast; these tests pin that it still does, because the ratio-based
// spill thresholds are taken against server or user limits, not the
// query-level --max_memory_usage the worker is started with.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const spillWorkerCap = 128 << 20

// runCapped returns the worker's exit error and its stderr, where the
// ClickHouse error code is.
func runCapped(t *testing.T, sql string) (stderr string, err error) {
	t.Helper()
	cfg := m0Config(t)
	cfg.MaxMemoryPerWorker = spillWorkerCap
	w, err := newWorker(context.Background(), cfg, nil)
	require.NoError(t, err)
	defer func() { _ = w.Close() }()
	_, _, err = submitTimed(w, m0Query{sql: sql, format: "Null"})
	stderr = string(w.StderrTail())
	return
}

func TestWorker_MemoryCapFailsFastInsteadOfSpilling(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{name: "group by", sql: "SELECT k, count() FROM (SELECT number % 4000000 AS k FROM numbers(20000000)) GROUP BY k"},
		{name: "hash join", sql: "SELECT count() FROM numbers(3000000) AS a JOIN (SELECT number AS n, toString(number) AS s FROM numbers(3000000)) AS b ON a.number = b.n"},
		// 26.9's external DISTINCT engages under the cap and still fails.
		{name: "distinct", sql: "SELECT count() FROM (SELECT DISTINCT toString(number) FROM numbers(6000000))"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stderr, err := runCapped(t, c.sql)
			require.Error(t, err)
			assert.Contains(t, stderr, "MEMORY_LIMIT_EXCEEDED")
		})
	}
}

// When a statement opts into spilling, the per-query temporary-data cap
// bounds what it may write under the worker's --path.
func TestWorker_PerQueryTemporaryDataCap(t *testing.T) {
	stderr, err := runCapped(t, "SELECT k, count() FROM (SELECT number % 4000000 AS k FROM numbers(20000000)) GROUP BY k "+
		"SETTINGS max_bytes_before_external_group_by = 33554432, max_temporary_data_on_disk_size_for_query = 1000000")
	require.Error(t, err)
	assert.Contains(t, stderr, "TOO_MANY_ROWS_OR_BYTES")
}
