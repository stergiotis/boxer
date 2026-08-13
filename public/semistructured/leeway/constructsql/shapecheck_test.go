package constructsql

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stretchr/testify/require"
)

func mintFixture(t *testing.T) (v string, memb string, card string) {
	t.Helper()
	c, err := lwsql.NewComposer(lwsql.DefaultTableSegments())
	require.NoError(t, err)
	v, err = c.TaggedValueColumn("sym", "v", "s", nil)
	require.NoError(t, err)
	memb, err = c.MembershipColumn("sym", "low-card-ref")
	require.NoError(t, err)
	card, err = c.SupportColumn("sym", "lrcard")
	require.NoError(t, err)
	return
}

func shapeOK(t *testing.T, sql string) {
	t.Helper()
	out, err := ShapeCheckPass.Run(sql)
	require.NoError(t, err)
	require.Equal(t, sql, out, "analytical pass must not rewrite")
}

func shapeErr(t *testing.T, sql string, msg string) {
	t.Helper()
	_, err := ShapeCheckPass.Run(sql)
	require.Error(t, err)
	require.ErrorContains(t, err, msg)
}

func TestShapeCheck_CoherentSetPasses(t *testing.T) {
	v, memb, card := mintFixture(t)
	shapeOK(t, `SELECT "id:mycol:u64:::0:", "`+v+`", "`+memb+`", "`+card+`" FROM t`)
	// Non-repeating membership (no card lane) is a legal shape — the
	// fast-path licence's dual.
	shapeOK(t, `SELECT "`+v+`", "`+memb+`" FROM t`)
	// A single plain column is a valid leeway table.
	shapeOK(t, `SELECT "id:mycol:u64:::0:" FROM t`)
	// Aliased expressions minting physical names count via their alias.
	shapeOK(t, `SELECT sum(x) AS "id:mycol:u64:::0:" FROM t`)
}

// TestShapeCheck_AfterConstructors is the intended composition: expand the
// constructor calls, then shape-check the expanded projection.
func TestShapeCheck_AfterConstructors(t *testing.T) {
	expanded, err := ExpandPass.Run("SELECT LW_TV(v, 'sym', 'v', 's'), LW_TV_MEMB(m, 'sym', 'low-card-ref'), LW_TV_SUPPORT(k, 'sym', 'lrcard') FROM src")
	require.NoError(t, err)
	shapeOK(t, expanded)
}

func TestShapeCheck_Rejections(t *testing.T) {
	v, memb, card := mintFixture(t)

	shapeErr(t, "SELECT sum(x) FROM t", "closure rule")
	shapeErr(t, "SELECT * FROM t", "cannot be shape-checked")
	shapeErr(t, "SELECT nope FROM t", "does not parse")
	shapeErr(t, `SELECT "`+v+`" FROM t`, "no membership lane")
	shapeErr(t, `SELECT "`+card+`" FROM t`, "dangling membership cardinality lane")
	_ = memb

	// Array value lane without its len support.
	c, err := lwsql.NewComposer(lwsql.DefaultTableSegments())
	require.NoError(t, err)
	va, err := c.TaggedValueColumn("sym", "va", "u64h", nil)
	require.NoError(t, err)
	shapeErr(t, `SELECT "`+va+`", "`+memb+`" FROM t`, "without their `len` support")
}

func TestShapeCheck_CoSectionGroupWholeness(t *testing.T) {
	seg := lwsql.DefaultTableSegments()
	seg.CoSectionGroup = "g"
	c, err := lwsql.NewComposer(seg)
	require.NoError(t, err)
	aVal, err := c.TaggedValueColumn("a", "v", "s", nil)
	require.NoError(t, err)
	aMemb, err := c.MembershipColumn("a", "low-card-ref")
	require.NoError(t, err)
	bMemb, err := c.MembershipColumn("b", "low-card-ref")
	require.NoError(t, err)

	// One half of the group alone: rejected.
	shapeErr(t, `SELECT "`+aVal+`", "`+aMemb+`" FROM t`, "dangling co-section-group half")
	// Both halves together: whole.
	shapeOK(t, `SELECT "`+aVal+`", "`+aMemb+`", "`+bMemb+`" FROM t`)
}

// TestShapeCheck_DuplicateOutputNames: a result carrying one physical name
// twice is not a leeway table, whichever way the duplicate arises.
func TestShapeCheck_DuplicateOutputNames(t *testing.T) {
	shapeErr(t, `SELECT "id:mycol:u64:::0:", "id:mycol:u64:::0:" FROM t`, "duplicate output column name")
	shapeErr(t, `SELECT a AS "id:mycol:u64:::0:", "id:mycol:u64:::0:" FROM t`, "duplicate output column name")
}

func TestShapeCheck_UnionMembersCheckedIndependently(t *testing.T) {
	v, memb, _ := mintFixture(t)
	good := `SELECT "` + v + `", "` + memb + `" FROM t`
	shapeOK(t, good+" UNION ALL "+good)
	shapeErr(t, good+" UNION ALL SELECT sum(x) FROM t", "closure rule")
}

func TestShapeCheck_AssertProperties(t *testing.T) {
	v, memb, card := mintFixture(t)
	corpus := []string{
		`SELECT "id:mycol:u64:::0:" FROM t`,
		`SELECT "` + v + `", "` + memb + `", "` + card + `" FROM t`,
		`SELECT "` + v + `", "` + memb + `" FROM t`,
	}
	nanopass.AssertProperties(t, ShapeCheckPass, corpus)
}
