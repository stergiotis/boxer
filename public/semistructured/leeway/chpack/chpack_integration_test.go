//go:build integration

package chpack_test

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
)

// The installer's Conn is deliberately client-free; this is the one place
// that asserts the intended client satisfies it.
var _ chpack.Conn = (*chclient.Client)(nil)

func liveClient(t *testing.T) (client *chclient.Client, ctx context.Context) {
	t.Helper()
	if clickhouseenv.Endpoint.Get() == "" && clickhouseenv.URL.Get() == "" {
		t.Skip("no ClickHouse endpoint configured (CLICKHOUSE_ENDPOINT / CLICKHOUSE_URL); skipping")
	}
	client = chclient.New(chclient.ConfigFromEnv(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Ping(ctx))
	return
}

func query(t *testing.T, ctx context.Context, client *chclient.Client, sql string) (out string) {
	t.Helper()
	body, err := client.Query(ctx, sql)
	require.NoError(t, err, sql)
	defer func() { _ = body.Close() }()
	b, err := io.ReadAll(body)
	require.NoError(t, err, sql)
	out = strings.TrimSpace(string(b))
	return
}

// TestIntegrationChpack is the ADR-0162 verification-plan lane: install on a
// live server, verify the marker, pin the correctness matrix, re-run the
// plan-identity and guard-pruning probes as regressions, and differential-
// test against a Go oracle on randomized positive co/ragged data.
func TestIntegrationChpack(t *testing.T) {
	client, ctx := liveClient(t)

	require.NoError(t, chpack.Install(ctx, client))

	t.Run("version marker", func(t *testing.T) {
		require.Equal(t, fmt.Sprintf("%d", chpack.Version),
			query(t, ctx, client, "SELECT leewayPackVersion()"))
	})

	t.Run("correctness matrix", func(t *testing.T) {
		// Expectations follow ClickHouse TSV literal formatting. Cases with
		// zero-length runs pin the CH-boundary totality; leeway reads never
		// produce them (positive descriptors).
		cases := []struct{ expr, want string }{
			{"coLookup(['a','b'], [10, 20], 'b')", "20"},
			{"coLookup(['a','b'], [10, 20], 'zz')", "0"},
			{"coLookupNull(['a','b'], [10, 20], 'zz')", "\\N"},
			{"coGather(['a','b','c'], [3, 1])", "['c','a']"},
			{"coArgSort([30, 10, 20])", "[2,3,1]"},
			{"coArgMax(['a','b'], [1, 5])", "b"},
			{"coExistsEq2(['a','w'], 'a', ['y','v'], 'y')", "1"},
			{"coExistsEq2(['a','w'], 'a', ['v','y'], 'y')", "0"},
			{"raggedStarts([2, 0, 3])", "[1,3,3]"},
			{"raggedRanges([2, 0, 3])", "[(1,2),(3,0),(3,3)]"},
			{"raggedParentIds([2, 0, 3])", "[1,1,3,3,3]"},
			{"raggedIota([2, 0, 3])", "[1,2,1,2,3]"},
			{"raggedNest([1, 2, 3, 4, 5], [2, 0, 3])", "[[1,2],[],[3,4,5]]"},
			{"raggedReduce('sum', [1, 2, 3, 4, 5], [2, 0, 3])", "[3,0,12]"},
			{"raggedExists(v -> v > 3, [1, 2, 3, 4, 5], [2, 0, 3])", "[0,0,1]"},
			{"raggedCount(v -> v > 2, [1, 2, 3, 4, 5], [2, 0, 3])", "[0,0,3]"},
			{"raggedElem([1, 2, 3, 4, 5], [2, 0, 3], 3, 2)", "4"},
		}
		for _, c := range cases {
			require.Equalf(t, c.want, query(t, ctx, client, "SELECT "+c.expr), "SELECT %s", c.expr)
		}
	})

	t.Run("plan identity", func(t *testing.T) {
		// A pack call and its handwritten expansion must produce the same
		// actions DAG — the macro claim of ADR-0162, pinned as a regression.
		src := " FROM (SELECT [1, 2, 3, 4, 5] AS vals, [2, 0, 3] AS card)"
		packed := query(t, ctx, client, "EXPLAIN actions = 1 SELECT raggedNest(vals, card)"+src)
		hand := query(t, ctx, client, "EXPLAIN actions = 1 SELECT arrayMap((c, hi) -> arraySlice(vals, hi - c + 1, c), card, arrayCumSum(card))"+src)
		require.Equal(t, hand, packed)
	})

	t.Run("differential against a Go oracle", func(t *testing.T) {
		rng := rand.New(rand.NewSource(1))
		for range 20 {
			n := 1 + rng.Intn(8)
			card := make([]int, n)
			vals := make([]int, 0, n*5)
			for i := range card {
				card[i] = 1 + rng.Intn(5) // positive descriptors, per the model
				for range card[i] {
					vals = append(vals, rng.Intn(201)-100)
				}
			}
			sums := make([]string, n)
			counts := make([]string, n)
			exists := make([]string, n)
			off := 0
			for i, c := range card {
				s, cnt, ex := 0, 0, 0
				for _, v := range vals[off : off+c] {
					s += v
					if v > 0 {
						cnt++
						ex = 1
					}
				}
				off += c
				sums[i] = fmt.Sprintf("%d", s)
				counts[i] = fmt.Sprintf("%d", cnt)
				exists[i] = fmt.Sprintf("%d", ex)
			}
			valsLit := intsLiteral(vals)
			cardLit := intsLiteral(card)
			got := query(t, ctx, client, fmt.Sprintf(
				"SELECT raggedReduce('sum', %s, %s), raggedCount(v -> v > 0, %s, %s), raggedExists(v -> v > 0, %s, %s)",
				valsLit, cardLit, valsLit, cardLit, valsLit, cardLit))
			want := "[" + strings.Join(sums, ",") + "]\t[" + strings.Join(counts, ",") + "]\t[" + strings.Join(exists, ",") + "]"
			require.Equalf(t, want, got, "vals=%s card=%s", valsLit, cardLit)
		}
	})

	t.Run("guard pruning", func(t *testing.T) {
		const db = "zz_chpack_it"
		t.Cleanup(func() {
			cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = client.Exec(cctx, "DROP DATABASE IF EXISTS "+db)
		})
		require.NoError(t, client.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+db))
		require.NoError(t, client.Exec(ctx, "CREATE OR REPLACE TABLE "+db+".t (id UInt64, a Array(String), b Array(String), INDEX bfa a TYPE bloom_filter(0.01) GRANULARITY 1, INDEX bfb b TYPE bloom_filter(0.01) GRANULARITY 1) ENGINE = MergeTree ORDER BY id"))
		require.NoError(t, client.Exec(ctx, "INSERT INTO "+db+".t SELECT number, if(number = 123456, ['needle', concat('t', toString(number % 997))], [concat('t', toString(number % 997)), concat('u', toString(number % 89))]), if(number = 123456, ['needle2', concat('v', toString(number % 991))], [concat('v', toString(number % 991)), concat('w', toString(number % 83))]) FROM numbers(500000)"))

		require.Equal(t, "1", query(t, ctx, client,
			"SELECT count() FROM "+db+".t WHERE coExistsEq2(a, 'needle', b, 'needle2')"))

		// The bundled guards must reach the bloom-filter skip indexes: the
		// final granule selection has to be a strict subset. Without the
		// guards the same predicate scanned every granule (ADR-0162 §SD3).
		plan := query(t, ctx, client,
			"EXPLAIN indexes = 1 SELECT count() FROM "+db+".t WHERE coExistsEq2(a, 'needle', b, 'needle2')")
		require.Contains(t, plan, "Name: bfa")
		// Skip stages chain: each Granules line reports selected/considered
		// where "considered" is what the previous stage left. Pruning means
		// the final selection is a strict subset of the initial total.
		granules := regexp.MustCompile(`Granules: (\d+)/(\d+)`).FindAllStringSubmatch(plan, -1)
		require.NotEmpty(t, granules)
		total := atoi(t, granules[0][2])
		final := atoi(t, granules[len(granules)-1][1])
		require.Lessf(t, final, total, "no pruning in plan:\n%s", plan)
	})
}

func intsLiteral(xs []int) (s string) {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%d", x)
	}
	s = "[" + strings.Join(parts, ", ") + "]"
	return
}

func atoi(t *testing.T, s string) (n int) {
	t.Helper()
	_, err := fmt.Sscanf(s, "%d", &n)
	require.NoError(t, err)
	return
}
