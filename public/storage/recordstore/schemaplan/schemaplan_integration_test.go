//go:build integration

package schemaplan

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// A table an older binary created is brought to the current columns by
// the derived statements, on clickhouse-local: the verdict turns ok, the
// window is set and the stamp is the build's.
func TestApplyBringsAnOlderTableInLine(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	beforeDDL, _ := fixture(t, "persiststate_before.sql")
	stmts, err := recordstore.ProvisioningStatements(beforeDDL, "boxer.persiststate", "")
	require.NoError(t, err)
	for _, s := range stmts {
		require.NoError(t, exec.Exec(ctx, s))
	}
	_, after := fixture(t, "persiststate_after.sql")
	c := contract("boxer.persiststate", after)

	p := PlanTable(ctx, exec, c)
	require.False(t, p.IsRefused(), p.Refused)
	require.NotEmpty(t, p.Statements)
	n, err := Apply(ctx, exec, p)
	require.NoError(t, err)
	require.Equal(t, len(p.Statements), n)

	v := recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	require.Equal(t, recordstore.StampSame, v.Age)
	require.True(t, v.DedupWindow)
	require.Empty(t, PlanTable(ctx, exec, c).Statements, "a second plan has nothing to do")
}

// A column left under an older aspect spelling is renamed and recodec'd.
func TestApplyRenamesAnAspectOnlyVariant(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	v2DDL, v2 := fixture(t, "device_aspects_v2.sql")
	_, v1 := fixture(t, "device_aspects_v1.sql")
	const at = 4
	oldDDL := strings.Replace(v2DDL, `"`+v2[at].Name+`" Array(UInt64) CODEC(Delta,ZSTD(3))`, `"`+v1[at].Name+`" Array(UInt64) CODEC(ZSTD(3))`, 1)
	require.NotEqual(t, v2DDL, oldDDL)
	require.NoError(t, exec.Exec(ctx, oldDDL))
	c := contract("device", v2)
	p := PlanTable(ctx, exec, c)
	require.False(t, p.IsRefused(), p.Refused)
	_, err = Apply(ctx, exec, p)
	require.NoError(t, err)
	v := recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	require.Empty(t, v.Extra)
}
