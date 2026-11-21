package contracts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cli "github.com/urfave/cli/v3"
)

func baseSnapshot() (s Snapshot) {
	s = make(Snapshot)
	s.put(KindDDL, "public/x/t_ddl_clickhouse.out.sql", "CREATE TABLE t (\n  a UInt64,\n  b String\n)\n")
	s.put(KindMembership, "public/x/testdata/membership-assignments.golden", "m1 1\nm2 2\n")
	s.put(KindLwSurface, "generation", "3")
	s.put(KindLwSurface, "digest", "aaa")
	s.put(KindLwSurface, "function/LW_A", "")
	s.put(KindEnv, "BOXER_A", "")
	s.put(KindEnv, "SPINNAKER_PLAY_SQL", "")
	s.put(KindIDL, "fingerprint", "0x1")
	s.put(KindVersion, "public/x.wireVersion", "1")
	s.put(KindCLI, "boxer gov", "")
	s.put(KindCLI, "boxer gov --root", "")
	return
}

func clone(s Snapshot) (c Snapshot) {
	c = make(Snapshot)
	for k, m := range s {
		for i, v := range m {
			c.put(k, i, v)
		}
	}
	return
}

func find(t *testing.T, changes []Change, key string) (c Change) {
	t.Helper()
	for _, c = range changes {
		if c.Key() == key {
			return
		}
	}
	require.Failf(t, "no change", "%s in %+v", key, changes)
	return
}

func TestDiffUnchangedIsEmpty(t *testing.T) {
	require.Empty(t, Diff(baseSnapshot(), baseSnapshot()))
}

func TestDiffAdditive(t *testing.T) {
	cur := clone(baseSnapshot())
	cur.put(KindDDL, "public/x/t_ddl_clickhouse.out.sql", "CREATE TABLE t (\n  a UInt64,\n  b String,\n  c Bool\n)\n")
	cur.put(KindMembership, "public/x/testdata/membership-assignments.golden", "m1 1\nm2 2\nm3 3\n")
	cur.put(KindLwSurface, "generation", "4")
	cur.put(KindLwSurface, "digest", "bbb")
	cur.put(KindLwSurface, "function/LW_B", "")
	cur.put(KindEnv, "BOXER_B", "")
	delete(cur[KindEnv], "SPINNAKER_PLAY_SQL")
	cur.put(KindVersion, "public/y.wireVersion", "1")
	cur.put(KindCLI, "boxer gov --out", "")
	changes := Diff(baseSnapshot(), cur)
	for _, c := range changes {
		assert.Truef(t, c.Additive, "%s %s: %s", c.Key(), c.What, c.Why)
	}
	assert.Equal(t, "removed", find(t, changes, "env/SPINNAKER_PLAY_SQL").What, "a tombstoned removal")
}

func TestDiffNotAdditive(t *testing.T) {
	cur := clone(baseSnapshot())
	cur.put(KindDDL, "public/x/t_ddl_clickhouse.out.sql", "CREATE TABLE t (\n  a UInt64,\n  b2 String\n)\n")
	cur.put(KindMembership, "public/x/testdata/membership-assignments.golden", "m1 1\nm2 7\n")
	cur.put(KindLwSurface, "digest", "bbb")
	delete(cur[KindLwSurface], "function/LW_A")
	delete(cur[KindEnv], "BOXER_A")
	cur.put(KindIDL, "fingerprint", "0x2")
	cur.put(KindVersion, "public/x.wireVersion", "2")
	delete(cur[KindCLI], "boxer gov --root")
	changes := Diff(baseSnapshot(), cur)
	for _, key := range []string{
		"ddl/public/x/t_ddl_clickhouse.out.sql", "membership/public/x/testdata/membership-assignments.golden",
		"lwsurface/digest", "lwsurface/function/LW_A", "env/BOXER_A", "idl/fingerprint",
		"version/public/x.wireVersion", "cli/boxer gov --root",
	} {
		c := find(t, changes, key)
		assert.Falsef(t, c.Additive, "%s: %s", key, c.Why)
	}
	assert.Equal(t, len(changes), Unacknowledged(changes))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "2026-11-x.md"), []byte("# x\n\n- `contract: idl/fingerprint`\n"), 0o644))
	acks, err := Acks(dir)
	require.NoError(t, err)
	Acknowledge(changes, acks)
	assert.Equal(t, "2026-11-x.md", find(t, changes, "idl/fingerprint").AckedBy)
	assert.Equal(t, len(changes)-1, Unacknowledged(changes))

	none, err := Acks(filepath.Join(dir, "absent"))
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestWriteLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snap")
	require.NoError(t, baseSnapshot().Write(dir))
	got, err := Load(dir)
	require.NoError(t, err)
	require.Empty(t, Diff(baseSnapshot(), got))
}

func TestCollectReadsTheTree(t *testing.T) {
	root := &cli.Command{Name: "x", Flags: []cli.Flag{&cli.BoolFlag{Name: "v"}},
		Commands: []*cli.Command{{Name: "gov", Commands: []*cli.Command{{Name: "contracts"}}}}}
	snap, err := Collect(filepath.Join("..", "..", ".."), root)
	require.NoError(t, err)
	assert.Len(t, snap[KindMembership], 9)
	assert.Contains(t, snap[KindDDL], "public/keelson/runtime/trail/facts_ddl_clickhouse.out.sql")
	for item := range snap[KindDDL] {
		assert.True(t, productionDDL(item), item)
	}
	assert.Equal(t, "1", snap[KindVersion]["public/db/clickhouse/jackstay.PlanFormatVersion"])
	assert.Contains(t, snap[KindVersion], "public/semistructured/leeway/canonwire/runtime.Version")
	assert.NotEmpty(t, snap[KindEnv])
	assert.Contains(t, snap[KindLwSurface], "function/LW_SURFACE_VERSION")
	assert.Contains(t, snap[KindCLI], "boxer --v")
	assert.Contains(t, snap[KindCLI], "boxer gov contracts")
}

func TestProductionDDL(t *testing.T) {
	assert.True(t, productionDDL("public/keelson/runtime/trail/facts_ddl_clickhouse.out.sql"))
	assert.False(t, productionDDL("public/storage/recordstore/internal/example/device_ddl_clickhouse.out.sql"))
	assert.False(t, productionDDL("public/keelson/runtime/factsschema/meshdemo/facts_ddl_clickhouse.out.sql"))
	assert.False(t, productionDDL("public/semistructured/leeway/anchor/card_anchor_ddl_clickhouse.out.sql"))
}
