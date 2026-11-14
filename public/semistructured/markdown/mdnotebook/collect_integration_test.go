//go:build integration

package mdnotebook_test

import (
	"context"
	"os/exec"
	"testing"
	"testing/fstest"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

const localDraft = "Over [?](val:total.n) hours the mean was [?](val:total.mean).\n\n" +
	"```sql\nSELECT toDateTime64(toDateTime('2026-11-02 00:00:00', 'UTC') + number * 3600, 3, 'UTC') AS t,\n" +
	"       if(number % 2 = 0, 'emea', 'apac') AS region, round(300 + 180 * sin(number / 4), 1) AS p99\nFROM numbers(48)\n```\n\n^p99-by-hour\n\n" +
	"```sql\nWITH emea AS (SELECT * FROM p99_by_hour WHERE region = 'emea')\nSELECT count() AS n, round(avg(p99), 1) AS mean FROM emea\n```\n\n^total\n\n" +
	"```sql\nSELECT region, max(p99) AS worst FROM p99_by_hour GROUP BY region ORDER BY worst DESC\n```\n\n^worst\n\n" +
	"```sql chart\nSELECT t AS x, region AS series, p99 FROM p99_by_hour\n```\n"

func clickhouseLocal(settings string) mdnotebook.RunFunc {
	return func(ctx context.Context, statement string) ([]byte, error) {
		return exec.CommandContext(ctx, "clickhouse-local", "-q", statement+settings).Output()
	}
}

// Collection against a local ClickHouse: a record reads the record before
// it, also from inside its own WITH list; a record's ORDER BY orders what
// is stored; and the blobs are the same bytes whatever block size the
// server streamed them in (ADR-0371 §SD8).
func TestCollectAgainstLocalClickHouse(t *testing.T) {
	if _, err := exec.LookPath("clickhouse-local"); err != nil {
		t.Skip("clickhouse-local is not installed")
	}
	nb, err := mdnotebook.Read([]byte(localDraft))
	require.NoError(t, err)
	out, err := mdnotebook.Collect(context.Background(), nb, "b", clickhouseLocal(""))
	require.NoError(t, err)
	small, err := mdnotebook.Collect(context.Background(), nb, "b", clickhouseLocal(" SETTINGS max_block_size = 7"))
	require.NoError(t, err)
	assert.Equal(t, string(out.Text), string(small.Text), "the same rows, the same blobs, the same pin")

	fsys := fstest.MapFS{}
	for p, data := range out.Blobs {
		fsys[p] = &fstest.MapFile{Data: data}
	}
	sealed, err := mdnotebook.Read(out.Text)
	require.NoError(t, err)
	require.Empty(t, sealed.Problems)
	ld := mdnotebook.Load(sealed, fsys)
	defer ld.Release()
	require.Empty(t, ld.Problems)
	assert.Empty(t, ld.Stale)

	hourly := ld.Records["p99_by_hour"]
	require.NotNil(t, hourly)
	assert.EqualValues(t, 48, hourly.NumRows())
	assert.Equal(t, arrow.TIMESTAMP, hourly.Schema().Field(0).Type.ID(), "a DateTime64 is stored as a time")
	assert.Contains(t, string(out.Text), "Over [24](val:total.n) hours")
	worst := ld.Records["worst"]
	require.NotNil(t, worst)
	require.EqualValues(t, 2, worst.NumRows())
	assert.GreaterOrEqual(t, worst.Column(1).ValueStr(0), worst.Column(1).ValueStr(1), "the record's ORDER BY is the stored order")

	for i := range sealed.Cells {
		c := &sealed.Cells[i]
		if c.Kind != mdnotebook.CellKindPresentation {
			continue
		}
		res, eErr := mdnotebook.Evaluate(c.Body, ld.Records)
		require.NoError(t, eErr)
		assert.Equal(t, "x", res.Main.Schema().Field(0).Name)
		res.Release()
	}
}
