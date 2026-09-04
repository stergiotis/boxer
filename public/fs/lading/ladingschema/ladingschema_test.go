package ladingschema_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rename is what makes a store over the facts shape on its own table
// legal, and the thing it must not change is the columns: a renamed
// descriptor that moved a column would produce a table the shared read access
// decodes wrongly, silently, because the decode is positional.
func TestTableDescRenamesWithoutMovingAnything(t *testing.T) {
	manip, err := factsschema.GetSchemaInManipulator()
	require.NoError(t, err)
	facts, err := manip.BuildTableDesc()
	require.NoError(t, err)

	for _, name := range []string{
		ladingschema.TableNameMeta, ladingschema.TableNameData, ladingschema.TableNameSnap,
	} {
		td, terr := ladingschema.TableDesc(name)
		require.NoError(t, terr)
		assert.EqualValues(t, name, string(td.DictionaryEntry.Name),
			"recordstore/gen refuses a descriptor whose own name disagrees with the table")
		assert.Equal(t, facts.PlainValuesNames, td.PlainValuesNames)
		assert.Equal(t, facts.PlainValuesItemTypes, td.PlainValuesItemTypes)
	}
}

// Raw SQL that names a backbone column resolves it from the descriptor rather
// than spelling it out, so a rename in factsschema moves the clause instead of
// leaving it over a column that is no longer there. The four names are pinned
// because they are also what the ALTERs, the view and every trusted scan
// predicate are written against.
func TestPhysicalPlainNamesResolve(t *testing.T) {
	for plain, want := range map[string]string{
		"id":         `"id:id:u64:47::0:"`,
		"naturalKey": `"id:naturalKey:y:4::0:"`,
		"ts":         `"ts:ts:z64:47::0:"`,
		"expiresAt":  `"lc:expiresAt:z64:4::0:"`,
	} {
		got, err := ladingschema.PhysicalPlainName(plain)
		require.NoErrorf(t, err, "plain %q", plain)
		assert.Equalf(t, want, got, "plain %q", plain)
	}

	_, err := ladingschema.PhysicalPlainName("notAPlain")
	assert.Error(t, err, "a clause naming a column that is not there must fail here, not at CREATE time")
}

// The engine clauses are the store's, not the generator's defaults: expiry-day
// partitioning, (mount, snapshot, path), TTL on the plain the partitioning
// uses, and whole parts dropped. Each is load-bearing and each is cheap to
// lose in an edit.
func TestTableOptionsCarryTheDesign(t *testing.T) {
	expiresAt, err := ladingschema.PhysicalPlainName("expiresAt")
	require.NoError(t, err)

	meta := ladingschema.MetaTableOptions(ladingschema.ProfileCorpus)
	assert.Equal(t, "toYYYYMMDD("+expiresAt+")", meta.PartitionBy,
		"partitioning by expiry day, never by mount: the partition count must not follow the mount count")
	assert.Equal(t, expiresAt, meta.TTL,
		"TTL and PARTITION BY must name the same column, or a partition expires piecemeal")
	require.Len(t, meta.OrderBy, 3)
	assert.EqualValues(t, "id", meta.OrderBy[0].Plain)
	assert.EqualValues(t, "ts", meta.OrderBy[1].Plain)
	assert.EqualValues(t, "naturalKey", meta.OrderBy[2].Plain)
	assert.Contains(t, meta.Settings, "ttl_only_drop_parts = 1")
	assert.Contains(t, meta.Settings, "index_granularity = 1024")

	// One block per mark on the block table is what makes a block read cost
	// exactly one compressed block.
	data := ladingschema.DataTableOptions(ladingschema.ProfileCorpus)
	assert.Contains(t, data.Settings, "index_granularity = 1")

	// The snapshot index is one row per snapshot, so the path is not in it.
	snap := ladingschema.SnapTableOptions(ladingschema.ProfileCorpus)
	require.Len(t, snap.OrderBy, 2)
	assert.EqualValues(t, "ts", snap.OrderBy[1].Plain)
}

// FinishStatements is what a CREATE TABLE cannot express. It is checked here
// rather than only against a server because most of it is only *wrong* at read
// time — a missing skip index still answers, slowly, and a wrong `ext` still
// groups, wrongly.
func TestFinishStatements(t *testing.T) {
	stmts, err := ladingschema.FinishStatements(ladingschema.ProfileCorpus)
	require.NoError(t, err)
	all := strings.Join(stmts, "\n")

	for _, want := range []string{
		"ADD COLUMN IF NOT EXISTS name",
		"ADD COLUMN IF NOT EXISTS dir",
		"ADD COLUMN IF NOT EXISTS depth",
		"ADD COLUMN IF NOT EXISTS ext",
		"ADD CONSTRAINT IF NOT EXISTS valid_path",
		"ADD INDEX IF NOT EXISTS ix_dir dir TYPE bloom_filter",
		"CREATE TABLE IF NOT EXISTS " + ladingschema.DatabaseName + "." + ladingschema.TableNameSnap,
		"CREATE MATERIALIZED VIEW IF NOT EXISTS",
	} {
		assert.Containsf(t, all, want, "FinishStatements must carry %q", want)
	}

	// Every statement is idempotent, because this runs at every start.
	for _, s := range stmts {
		assert.True(t,
			strings.Contains(s, "IF NOT EXISTS"),
			"not idempotent: %s", firstLine(s))
	}

	// One statement per Exec: the HTTP interface rejects a multi-statement
	// body, so a semicolon here would fail only against a real server.
	for _, s := range stmts {
		assert.NotContains(t, s, ";", "one statement per Exec: %s", firstLine(s))
	}

	// The `ext` expression reads a leading dot as part of the name. The first
	// draft did not, and gave the root row an extension of `.`.
	assert.Contains(t, all, "position(substring(name, 2), '.')",
		"ext must skip a leading dot, or `.gitignore` reads as all extension")

	// The view's predicate is the commit rule.
	naturalKey, err := ladingschema.PhysicalPlainName("naturalKey")
	require.NoError(t, err)
	assert.Contains(t, all, "WHERE "+naturalKey+" = '.'")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// A layout moves every qualified name a store's DDL carries — the database
// prelude, the three tables, the view and its target — and nothing else. The
// default layout must render byte for byte what the unqualified functions
// render, since every existing caller goes through those.
func TestLayoutMovesTheDatabaseAndNothingElse(t *testing.T) {
	moved := ladingschema.Layout{Database: "elsewhere"}
	for _, tc := range []struct {
		name string
		def  func(ladingschema.Profile) ([]string, error)
		in   func(ladingschema.Profile) ([]string, error)
	}{
		{"create", ladingschema.CreateTableStatements, moved.CreateTableStatements},
		{"finish", ladingschema.FinishStatements, moved.FinishStatements},
	} {
		defStmts, err := tc.def(ladingschema.ProfileCorpus)
		require.NoError(t, err)
		zeroStmts, err := ladingschema.Layout{}.CreateTableStatements(ladingschema.ProfileCorpus)
		if tc.name == "finish" {
			zeroStmts, err = ladingschema.Layout{}.FinishStatements(ladingschema.ProfileCorpus)
		}
		require.NoError(t, err)
		assert.Equal(t, defStmts, zeroStmts, "%s: the zero layout is the default", tc.name)

		inStmts, err := tc.in(ladingschema.ProfileCorpus)
		require.NoError(t, err)
		require.Len(t, inStmts, len(defStmts), "%s: a layout adds or drops no statement", tc.name)
		all := strings.Join(inStmts, "\n")
		assert.NotContains(t, all, ladingschema.DatabaseName+".",
			"%s: no table of the moved store may resolve to the default database", tc.name)
		for i := range defStmts {
			assert.Equal(t,
				strings.ReplaceAll(defStmts[i], ladingschema.DatabaseName, "elsewhere"), inStmts[i],
				"%s: statement %d differs by more than the database", tc.name, i)
		}
	}
	assert.Equal(t, "elsewhere.fsmeta", moved.MetaTable())
	assert.Equal(t, "elsewhere.fsdata", moved.DataTable())
	assert.Equal(t, "elsewhere.fssnap", moved.SnapTable())
	assert.Equal(t, ladingschema.DatabaseName, ladingschema.Layout{}.DatabaseName())
}
