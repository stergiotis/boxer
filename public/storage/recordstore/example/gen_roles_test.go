package example

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// The explicit role declaration (gen.Input.Roles; ADR-0100 SD2's deferred
// explicit role configuration): a schema with several same-typed plain
// columns elects its role columns by name, and the columns positional
// binding would have elected ride the pass-through envelope instead.

// rolesManipulator builds a schema where every role has two candidate
// columns, so positional binding and the declaration disagree: ida/idb
// (EntityId u64), tsa/tsb (EntityTimestamp z64), lca/lcb (EntityLifecycle
// u8), plus one section for the component. The names avoid "id"/"ts": a
// role column the declaration demotes rides the envelope as a promoted
// field, and "Ts" would collide with the entity's fixed field there.
func rolesManipulator(t *testing.T) *common.TableManipulator {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("valcheck")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "ida", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "idb", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "tsa", ctabb.Z64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "tsb", ctabb.Z64)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "lca", ctabb.U8)
	manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "lcb", ctabb.U8)
	sec := manip.TaggedValueSection("solo").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardRef)
	sec.TaggedValueColumn("value", ctabb.S)
	return manip
}

func generateRolesInto(t *testing.T, manip *common.TableManipulator, roles gen.Roles, componentPaths ...string) (outDir string, err error) {
	t.Helper()
	td, berr := manip.BuildTableDesc()
	require.NoError(t, berr)
	outDir = t.TempDir()
	err = gen.Input{
		PackageName:    "tmp",
		StoreName:      "Valcheck",
		TableName:      "valcheck",
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: componentPaths,
		OutDir:         outDir,
		ImportPath:     "example.invalid/tmp",
		Roles:          roles,
	}.Generate()
	return
}

// TestGenerateDeclaredRolesElectColumns: the declaration binds the named
// columns, the positional candidates demote to pass-through envelope
// fields, and the derived DDL ORDER BY follows the declared Key and Order.
func TestGenerateDeclaredRolesElectColumns(t *testing.T) {
	out, err := generateRolesInto(t, rolesManipulator(t),
		gen.Roles{Key: "idb", Order: "tsb", Lifecycle: "lcb"})
	require.NoError(t, err)
	store := readStore(t, out)
	require.Contains(t, store, "ColKey")
	require.Contains(t, store, "`\"id:idb:")
	require.Contains(t, store, "`\"ts:tsb:")
	require.Contains(t, store, "`\"lc:lcb:")
	// The positionally-leading candidates ride the envelope now.
	require.Contains(t, store, "type ValcheckEnvelope struct")
	require.Contains(t, store, "Ida uint64")
	require.Contains(t, store, "Tsa time.Time")
	require.Contains(t, store, "Lca uint8")
	ddl, err := os.ReadFile(filepath.Join(out, "valcheck_ddl_clickhouse.out.sql"))
	require.NoError(t, err)
	require.Contains(t, string(ddl), `ORDER BY ("id:idb:`)
	require.Contains(t, string(ddl), `"ts:tsb:`)
}

// TestGenerateRolesUnknownColumn: a declared role must resolve.
func TestGenerateRolesUnknownColumn(t *testing.T) {
	_, err := generateRolesInto(t, rolesManipulator(t), gen.Roles{Key: "nope"})
	require.ErrorContains(t, err, "names no plain column")
}

// TestGenerateRolesKeyWrongItemType: the Key role takes EntityId columns
// only.
func TestGenerateRolesKeyWrongItemType(t *testing.T) {
	_, err := generateRolesInto(t, rolesManipulator(t), gen.Roles{Key: "tsb"})
	require.ErrorContains(t, err, "requires an EntityId plain column")
}

// TestGenerateRolesOrderWrongType: the Order role takes the EntityTimestamp
// lane or a u64-deriving plain column — a u8 lifecycle column is neither.
func TestGenerateRolesOrderWrongType(t *testing.T) {
	_, err := generateRolesInto(t, rolesManipulator(t), gen.Roles{Order: "lca"})
	require.ErrorContains(t, err, "takes the EntityTimestamp z64 lane or a plain column deriving to uint64")
}

// TestGenerateRolesOrderSharedWithKey: one column cannot serve two roles.
func TestGenerateRolesOrderSharedWithKey(t *testing.T) {
	_, err := generateRolesInto(t, rolesManipulator(t), gen.Roles{Key: "idb", Order: "idb"})
	require.ErrorContains(t, err, "another declared role already binds")
}

// TestGenerateRolesU64OrderSignatures: an Order bound to a u64 EntityId
// column (ADR-0100 Update 2026-08-30) generates uint64 verb signatures,
// the Ord entity field, plain integer range SQL in Replay, and the
// ReplayOptsU64 option type; the timestamp helpers stay off the surface.
func TestGenerateRolesU64OrderSignatures(t *testing.T) {
	out, err := generateRolesInto(t, rolesManipulator(t), gen.Roles{Key: "ida", Order: "idb"})
	require.NoError(t, err)
	store := readStore(t, out)
	require.Contains(t, store, "Begin(id uint64, ord uint64")
	require.Regexp(t, `Ord\s+uint64`, store)
	require.Contains(t, store, "fromOrder uint64, opts recordstore.ReplayOptsU64")
	require.Contains(t, store, "strconv.FormatUint(fromOrder, 10)")
	require.Contains(t, store, "MarkStaleIfOlder(key uint64, order uint64)")
	require.Contains(t, store, "Delete(id uint64, ord uint64)")
	require.Contains(t, store, "int64(e.Ord)")
	require.NotContains(t, store, "fromUnixTimestamp64Nano")
	require.NotRegexp(t, `Ts\s+time\.Time`, store)
}

// TestGeneratePositionalU64OrderRefused: the u64 regime is reachable only
// through the explicit declaration — a positionally-bound Order stays on
// the timestamp lane.
func TestGeneratePositionalU64OrderRefused(t *testing.T) {
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("valcheck")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "ida", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "seq", ctabb.U64)
	sec := manip.TaggedValueSection("solo").
		SectionStreamingGroup("data").
		AddSectionMembership(common.MembershipSpecLowCardRef)
	sec.TaggedValueColumn("value", ctabb.S)
	_, err = generateRolesInto(t, manip, gen.Roles{})
	require.ErrorContains(t, err, "bind a uint64 column explicitly via Roles.Order")
}

// TestGenerateRolesLifecycleWrongType: the Lifecycle role takes a u8
// EntityLifecycle column.
func TestGenerateRolesLifecycleWrongType(t *testing.T) {
	_, err := generateRolesInto(t, rolesManipulator(t), gen.Roles{Lifecycle: "idb"})
	require.ErrorContains(t, err, "requires a u8 EntityLifecycle plain column")
}

// TestGeneratePartialRolesKeepPositionalDefaults: declaring only the
// Lifecycle leaves Key and Order on their positional bindings.
func TestGeneratePartialRolesKeepPositionalDefaults(t *testing.T) {
	out, err := generateRolesInto(t, rolesManipulator(t), gen.Roles{Lifecycle: "lcb"})
	require.NoError(t, err)
	store := readStore(t, out)
	require.Contains(t, store, "`\"id:ida:")
	require.Contains(t, store, "`\"ts:tsa:")
	require.Contains(t, store, "`\"lc:lcb:")
	require.Contains(t, store, "Lca uint8")
}
