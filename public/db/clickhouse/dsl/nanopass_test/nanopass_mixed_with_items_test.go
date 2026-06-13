package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/require"
)

// TestMixedWithItems exercises the grammar after the withItem refactor — a
// single WITH may contain both scalar/column expression aliases and named
// subquery (CTE) definitions in any order, matching ClickHouse semantics.
func TestMixedWithItems(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"alias-only", "WITH `a` AS x SELECT 1"},
		{"alias-only-multi", "WITH `a` AS x, `b` AS y SELECT 1"},
		{"cte-only", "WITH r AS (SELECT 1) SELECT * FROM r"},
		{"cte-only-multi", "WITH r1 AS (SELECT 1), r2 AS (SELECT 2) SELECT * FROM r1"},
		{"alias-then-cte", "WITH `a` AS x, r AS (SELECT 1) SELECT * FROM r"},
		{"cte-then-alias", "WITH r AS (SELECT 1), `a` AS x SELECT * FROM r"},
		{"interleaved", "WITH `a` AS x, r1 AS (SELECT 1), `b` AS y, r2 AS (SELECT 2) SELECT * FROM r1"},
		{"user-full", `WITH
      ` + "`tv_string_semantic_val_s_k_0_qC7BL0mtTc_0__`" + ` AS ssem,
      ` + "`tv_string_value_val_s_g_0_gw_0__`" + `            AS sval,
      ` + "`tv_time_semantic_val_s_k_0_qC7BL0mtTc_0__`" + `   AS tsem,
      ` + "`tv_time_value_val_z32_48_0_gw_0__`" + `           AS tval,
      recordings AS (
          SELECT
              ssem[indexOf(sval,'source_file')]                                  AS source_file,
              toFloat64OrNull(ssem[indexOf(sval,'duration_ms')])                 AS duration_ms,
              min(tval[indexOf(tsem,'signal_start_time')])                       AS start_t,
              maxIf(tval[indexOf(tsem,'signal_end_time')],
                    indexOf(tsem,'signal_end_time') > 0
                    AND tval[indexOf(tsem,'signal_end_time')] > toDateTime('2020-01-01','UTC')) AS end_t,
              count() AS fact_count
          FROM t
          GROUP BY source_file, duration_ms
          HAVING end_t > start_t
      )
  SELECT
      toDateTime64(start_t, 3, 'UTC')                                    AS _tl_time,
      toDateTime64(end_t,   3, 'UTC')                                    AS _tl_time_end,
      extract(source_file, '\d{2}h\d{2}m\d{2}s|raw')                     AS _tl_lane,
      toFloat32(fact_count) / max(toFloat32(fact_count)) OVER ()         AS _tl_intensity,
      source_file
  FROM recordings
  ORDER BY _tl_time`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := nanopass.Parse(c.sql)
			require.NoError(t, err)
		})
	}
}
