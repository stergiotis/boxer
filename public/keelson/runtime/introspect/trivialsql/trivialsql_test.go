package trivialsql

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// seqProvider is a table defined by its argument: n rows, v = 1..n, and a
// word per row.
type seqProvider struct{}

func (seqProvider) Name() string                         { return "seq" }
func (seqProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (seqProvider) Schema() *arrow.Schema                { return seqTable().Schema() }
func (seqProvider) Snapshot(introspect.Projection) (arrow.RecordBatch, error) {
	return nil, assert.AnError
}
func (seqProvider) Args() []introspect.ArgSpec {
	return []introspect.ArgSpec{{Name: "n", Type: introspect.ArgUInt64, Required: true}}
}
func (seqProvider) SnapshotArgs(proj introspect.Projection, args introspect.Args) (arrow.RecordBatch, error) {
	return seqTable().Build(proj, int(args.UInt64("n"))), nil
}

func seqTable() *introspect.Table {
	words := []string{"a", "b\tc", "d", "e", "f"}
	return introspect.NewTable().
		Uint64("v", func(i int) uint64 { return uint64(i + 1) }).
		String("w", func(i int) string { return words[i%len(words)] })
}

// plainProvider takes no arguments.
type plainProvider struct{}

func (plainProvider) Name() string                         { return "plain" }
func (plainProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (plainProvider) Schema() *arrow.Schema                { return seqTable().Schema() }
func (plainProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	return seqTable().Build(proj, 2), nil
}

func reg(t *testing.T) *introspect.Registry {
	t.Helper()
	r := introspect.NewRegistry()
	require.NoError(t, r.Register(seqProvider{}))
	require.NoError(t, r.Register(plainProvider{}))
	return r
}

func run(t *testing.T, sql string, params map[string]string) string {
	t.Helper()
	body, err := Run(context.Background(), reg(t), sql, params)
	require.NoError(t, err, sql)
	return string(body)
}

func TestRun_Shapes(t *testing.T) {
	cases := []struct {
		sql    string
		params map[string]string
		want   string
	}{
		{"SELECT * FROM keelson('plain')", nil, "1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson(plain) AS p FORMAT TabSeparated", nil, "1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson('seq', n = 3)", nil, "1\ta\n2\tb\\tc\n3\td\n"},
		{"SELECT * FROM keelson('seq', n = {k:UInt64}) LIMIT 1", map[string]string{"k": "4"}, "1\ta\n"},
		{"SET param_k = 4; SELECT * FROM keelson('seq', n = {k:UInt64}) LIMIT 1 OFFSET 3", nil, "4\te\n"},
		{"SELECT * FROM keelson('seq', n = 5) LIMIT 1, 2", nil, "2\tb\\tc\n3\td\n"},
		{"WITH vf AS (SELECT * FROM keelson('seq', n = 2)) SELECT * FROM vf", nil, "1\ta\n2\tb\\tc\n"},
		{"SELECT * FROM keelson('seq', n = 9) LIMIT 0", nil, ""},
		{"SELECT * FROM keelson('plain') SETTINGS max_threads = 1 FORMAT JSONEachRow", nil, "{\"v\":1,\"w\":\"a\"}\n{\"v\":2,\"w\":\"b\\tc\"}\n"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, run(t, c.sql, c.params), c.sql)
	}
}

func TestRun_ArrowStream(t *testing.T) {
	batch, format, err := Read(reg(t), "SELECT * FROM keelson('seq', n = 4) FORMAT ArrowStream", nil)
	require.NoError(t, err)
	defer batch.Release()
	assert.Equal(t, FormatArrowStream, format)
	assert.Equal(t, int64(4), batch.NumRows())
	body, err := Encode(batch, format)
	require.NoError(t, err)
	assert.NotEmpty(t, body)
}

func TestRun_RefusesWhatNeedsClickHouse(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1",
		"SELECT v FROM keelson('plain')",
		"SELECT * FROM keelson('plain') WHERE v = 1",
		"SELECT * FROM keelson('plain') ORDER BY v",
		"SELECT count() FROM keelson('plain')",
		"SELECT * FROM keelson('plain') JOIN keelson('plain') USING v",
		"SELECT * FROM keelson('plain') UNION ALL SELECT * FROM keelson('plain')",
		"SELECT * FROM (SELECT * FROM keelson('plain'))",
		"SELECT * FROM system.tables",
		"SELECT * FROM url('http://x', 'ArrowStream')",
		"WITH x AS (SELECT v FROM keelson('plain')) SELECT * FROM x",
		"WITH 1 AS one SELECT * FROM keelson('plain')",
		"SET max_threads = 1; SELECT * FROM keelson('plain')",
		"SELECT * FROM keelson('plain') FORMAT Pretty",
		"INSERT INTO t VALUES (1)",
	} {
		_, err := Run(context.Background(), reg(t), sql, nil)
		assert.True(t, errors.Is(err, ErrNeedsClickHouse), "%s: %v", sql, err)
	}
}

func TestRun_CallErrorsAreNotRefusals(t *testing.T) {
	for _, sql := range []string{
		"SELECT * FROM keelson('nope')",
		"SELECT * FROM keelson('seq')",
		"SELECT * FROM keelson('seq', n = {k:UInt64})",
		"SELECT * FROM keelson('plain', n = 1)",
	} {
		_, err := Run(context.Background(), reg(t), sql, nil)
		require.Error(t, err, sql)
		assert.False(t, errors.Is(err, ErrNeedsClickHouse), "%s is a bad call, not a statement for a server: %v", sql, err)
	}
}
