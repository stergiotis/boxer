package jackstay

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	jk "github.com/stergiotis/boxer/public/db/clickhouse/jackstay"
	"github.com/stergiotis/boxer/public/gov/datacatalog"
)

func TestParseMap(t *testing.T) {
	m, err := parseMap(nil)
	require.NoError(t, err)
	assert.Nil(t, m)
	m, err = parseMap([]string{"a=b", "c=d"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"a": "b", "c": "d"}, m)
	for _, bad := range []string{"a", "=b", "a="} {
		_, err = parseMap([]string{bad})
		assert.Error(t, err, bad)
	}
}

func TestParseFilters(t *testing.T) {
	m, err := parseFilters([]string{"db.t = x = 1 AND y >= 2"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"db.t": "x = 1 AND y >= 2"}, m, "the first '=' separates")
	for _, bad := range [][]string{{"t=x"}, {"db.t"}, {"db.t="}, {"db.t=a", "db.t=b"}} {
		_, err = parseFilters(bad)
		assert.Error(t, err, bad)
	}
}

// A --table names a table as database.name; anything else is refused rather
// than matching nothing.
func TestParseRefs(t *testing.T) {
	refs, err := parseRefs([]string{"db.t", "db.a.b"})
	require.NoError(t, err)
	assert.Equal(t, []datacatalog.TableRef{{Database: "db", Name: "t"}, {Database: "db", Name: "a.b"}}, refs)
	for _, bad := range []string{"t", ".t", "db.", ""} {
		_, err = parseRefs([]string{bad})
		assert.Error(t, err, bad)
	}
}

// syncRequest runs the sync command's flag parsing on args.
func syncRequest(t *testing.T, args ...string) (req jk.SyncRequest, compression string, err error) {
	t.Helper()
	cmd := newSyncCommand()
	cmd.Action = func(c *cli.Context) error {
		req, compression, err = parseSyncRequest(c)
		return nil
	}
	a := &cli.App{Commands: []*cli.Command{cmd}, ExitErrHandler: func(*cli.Context, error) {}}
	require.NoError(t, a.Run(append([]string{"boxer", "sync", "--plan", "plan.json"}, args...)))
	return
}

func TestParseSyncRequest(t *testing.T) {
	req, compression, err := syncRequest(t, "--mode", "sample", "--sample", "1/10", "--existing", "append",
		"--compression", "none", "--restart", "--table", "db.t")
	require.NoError(t, err)
	assert.Equal(t, jk.SyncModeSample, req.Mode)
	assert.Equal(t, jk.ExistingPolicyAppend, req.Existing)
	assert.Equal(t, uint32(1), req.SampleNum)
	assert.Equal(t, uint32(10), req.SampleDen)
	assert.True(t, req.Restart)
	assert.Equal(t, []datacatalog.TableRef{{Database: "db", Name: "t"}}, req.Only)
	assert.Empty(t, compression, "none is no compression")
	assert.InDelta(t, 1.5, req.Headroom, 1e-9)

	req, compression, err = syncRequest(t, "--mode", "full")
	require.NoError(t, err)
	assert.Equal(t, jk.SyncModeFull, req.Mode)
	assert.Equal(t, jk.ExistingPolicyRefuse, req.Existing)
	assert.Equal(t, "zstd", compression)

	for _, bad := range [][]string{
		{"--mode", "mirror"},
		{"--mode", "full", "--existing", "merge"},
		{"--mode", "sample", "--sample", "2/1"},
		{"--mode", "full", "--compression", "lz4"},
		{"--mode", "full", "--table", "t"},
	} {
		_, _, err = syncRequest(t, bad...)
		assert.Error(t, err, bad)
	}
}
