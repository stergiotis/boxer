package example

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// The generator validation gates (ADR-0100 SD2/SD6): schema/DTO
// combinations that would emit silently-corrupting or non-compiling
// stores must fail at generation time instead.

func writeDTO(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))
	return path
}

// validationManipulator builds a minimal schema: plain id (u64 by
// default) + ts + the given single-value string sections.
func validationManipulator(t *testing.T, sections ...string) *common.TableManipulator {
	t.Helper()
	manip, err := common.NewTableManipulator()
	require.NoError(t, err)
	manip.SetTableName("valcheck")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
	for _, s := range sections {
		sec := manip.TaggedValueSection(naming.MustBeValidStylableName(s)).
			SectionStreamingGroup("data").
			AddSectionMembership(common.MembershipSpecLowCardRef)
		sec.TaggedValueColumn("value", ctabb.S)
	}
	return manip
}

func generateInto(t *testing.T, manip *common.TableManipulator, componentPaths ...string) error {
	t.Helper()
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	return gen.Input{
		PackageName:    "tmp",
		StoreName:      "Valcheck",
		TableName:      "valcheck",
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: componentPaths,
		OutDir:         t.TempDir(),
	}.Generate()
}

// TestGenerateRejectsSharedSection: membership ids are assigned per kind,
// so two kinds binding one section would silently cross-decode — the
// generator must refuse.
func TestGenerateRejectsSharedSection(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,shared\"`"+`
}
`)
	b := writeDTO(t, dir, "kind_b.go", `package tmp

type KindB struct {
	_  struct{} `+"`kind:\"kindB\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	B  string   `+"`lw:\"fieldB,shared\"`"+`
}
`)
	err := generateInto(t, validationManipulator(t, "shared"), a, b)
	require.ErrorContains(t, err, "disjoint sections")
}

// TestGenerateRejectsDuplicateRoleColumns: a second EntityId plain column
// must be a schema error, not a silent last-wins.
func TestGenerateRejectsDuplicateRoleColumns(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	manip := validationManipulator(t, "solo")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id2", ctabb.U64)
	err := generateInto(t, manip, a)
	require.ErrorContains(t, err, "both carry the Key role")
}

// TestGenerateRejectsNonRolePlainColumn: a plain column outside the three
// roles would be silently zero-written by every Begin and dropped by the
// decode — the generator must refuse until pass-through envelope fields
// exist (ADR-0100 Update 2026-07-04).
func TestGenerateRejectsNonRolePlainColumn(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	manip := validationManipulator(t, "solo")
	manip.PlainValueColumn(common.PlainItemTypeEntityRouting, "route", ctabb.U64)
	err := generateInto(t, manip, a)
	require.ErrorContains(t, err, "only the role-bearing")
}

// TestGenerateRejectsIngestIdTypeMismatch: an id field whose Go type
// disagrees with the Key column would emit non-compiling Ingest code —
// the generator must refuse instead.
func TestGenerateRejectsIngestIdTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID string   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	err := generateInto(t, validationManipulator(t, "solo"), a)
	require.ErrorContains(t, err, "cannot be emitted")
}

// TestGenerateIngestUsesIdFieldName: the DTO's id field need not be
// named ID — Ingest must reference the actual Go field bound to the
// plain id column (the old emitter hard-coded rows[i].ID and produced
// non-compiling output for any other name).
func TestGenerateIngestUsesIdFieldName(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_    struct{} `+"`kind:\"kindA\"`"+`
	Node uint64   `+"`lw:\",id\"`"+`
	A    string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	outDir := t.TempDir()
	td, err := validationManipulator(t, "solo").BuildTableDesc()
	require.NoError(t, err)
	require.NoError(t, gen.Input{
		PackageName:    "tmp",
		StoreName:      "Valcheck",
		TableName:      "valcheck",
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         outDir,
	}.Generate())
	store, err := os.ReadFile(filepath.Join(outDir, "valcheck_store.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(store), "inst.Begin(rows[i].Node, ts)")
}
