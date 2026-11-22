package recordstore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckStoredTables(t *testing.T) {
	ok := StoredTable{Owner: "cell", Contract: TableContract{Kind: "k", Table: "db.t"}}
	require.NoError(t, CheckStoredTables([]StoredTable{ok, {Owner: "cell", Contract: TableContract{Kind: "other", Table: "db.t"}}}),
		"two kinds may share a table")
	for name, tables := range map[string][]StoredTable{
		"no owner":         {{Contract: TableContract{Kind: "k", Table: "db.t"}}},
		"an owner in caps": {{Owner: "Cell", Contract: TableContract{Kind: "k", Table: "db.t"}}},
		"no table":         {{Owner: "cell", Contract: TableContract{Kind: "k"}}},
		"declared twice":   {ok, {Owner: "audit", Contract: TableContract{Kind: "k", Table: "db.t"}}},
	} {
		assert.Error(t, CheckStoredTables(tables), name)
	}
}

func TestDeclaredStoredTablesAreSortedByOwnerThenTable(t *testing.T) {
	DeclareStoredTables(func() ([]StoredTable, error) {
		return []StoredTable{
			{Owner: "zz-test", Contract: TableContract{Kind: "k", Table: "zz_test.b"}},
			{Owner: "zz-test", Contract: TableContract{Kind: "k", Table: "zz_test.a"}},
		}, nil
	})
	tables, err := DeclaredStoredTables()
	require.NoError(t, err)
	var mine []string
	for _, st := range tables {
		if st.Owner == "zz-test" {
			mine = append(mine, st.Contract.Table)
		}
	}
	assert.Equal(t, []string{"zz_test.a", "zz_test.b"}, mine)
}

func TestProvisioningSQLListsWhatProvisionWouldRun(t *testing.T) {
	stmts, err := ProvisioningSQL(context.Background(), func(ctx context.Context, exec ExecutorI) error {
		if err := exec.Exec(ctx, "CREATE DATABASE IF NOT EXISTS db"); err != nil {
			return err
		}
		return exec.Exec(ctx, "CREATE TABLE IF NOT EXISTS db.t (a UInt8) ENGINE = MergeTree ORDER BY a")
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"CREATE DATABASE IF NOT EXISTS db", "CREATE TABLE IF NOT EXISTS db.t (a UInt8) ENGINE = MergeTree ORDER BY a"}, stmts)

	_, err = ProvisioningSQL(context.Background(), func(ctx context.Context, exec ExecutorI) error {
		for _, rerr := range exec.QueryArrow(ctx, "SELECT 1") {
			return rerr
		}
		return nil
	})
	assert.ErrorContains(t, err, "cannot be listed", "a provision that reads the server has no fixed statements")
}
