package example

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// TestGenerateWithDatabaseQualifiesSQL: a Database qualifies every generated
// table reference as "<db>.<table>" through the single <Store>TableName const
// (which feeds the runtime SELECT/INSERT/DESCRIBE) and the composed CREATE
// TABLE, and the DDL prepends CREATE DATABASE IF NOT EXISTS so EnsureTable
// provisions the database first. The e2e leg runs the emitted DDL against
// clickhouse-local: the table must be addressable as mydb.valcheck and must
// not leak into the default database.
func TestGenerateWithDatabaseQualifiesSQL(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	outDir := t.TempDir()
	td, err := validationManipulator(t, "solo").BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "tmp",
		StoreName:      "Valcheck",
		TableName:      "valcheck",
		Database:       "mydb",
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         outDir,
		ImportPath:     "example.invalid/tmp",
	}.Generate())

	// The const carries the qualified name, so every runtime statement that
	// routes through it (SELECT/INSERT/DESCRIBE) is database-scoped.
	store := readStore(t, outDir)
	require.Contains(t, store, `const ValcheckTableName = "mydb.valcheck"`)

	// The DDL provisions the database, then creates the qualified table.
	ddlBytes, err := os.ReadFile(filepath.Join(outDir, "valcheck_ddl_clickhouse.out.sql"))
	require.NoError(t, err)
	ddl := string(ddlBytes)
	require.True(t, strings.HasPrefix(ddl, "CREATE DATABASE IF NOT EXISTS mydb;"),
		"DDL must lead with CREATE DATABASE, got:\n%s", ddl)
	require.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS mydb.valcheck")

	// e2e: the emitted DDL must execute end to end and land the table in the
	// named database — provisioning is proven by Exec succeeding (a qualified
	// CREATE against a missing database errors), qualification by the table
	// resolving as mydb.valcheck but not as the bare (default-database) name.
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	require.NoError(t, exec.Exec(ctx, ddl))
	require.NoError(t, exec.Exec(ctx, "SELECT count() FROM mydb.valcheck"),
		"the qualified table must resolve in the provisioned database")
	require.Error(t, exec.Exec(ctx, "SELECT count() FROM valcheck"),
		"the table must live in mydb, not the default database")
}

// TestGenerateRejectsBadDatabaseName: the database name is emitted unquoted
// into the qualified reference, so it carries TableName's simple-identifier
// constraint — a non-conforming name must fail at generation time.
func TestGenerateRejectsBadDatabaseName(t *testing.T) {
	td, err := validationManipulator(t, "solo").BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName: "tmp",
		StoreName:   "Valcheck",
		TableName:   "valcheck",
		Database:    "my_db",
		Table:       td,
		RowConfig:   common.TableRowConfigMultiAttributesPerRow,
		OutDir:      t.TempDir(),
		ImportPath:  "example.invalid/tmp",
	}.Generate()
	require.ErrorContains(t, err, "single lowercase word")
}
