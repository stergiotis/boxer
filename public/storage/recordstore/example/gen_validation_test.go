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
	_, err := generateIntoDir(t, manip, componentPaths...)
	return err
}

// generateIntoDir is generateInto but returns the OutDir so a test can read
// the emitted files back (e.g. to assert the pass-through envelope shape).
func generateIntoDir(t *testing.T, manip *common.TableManipulator, componentPaths ...string) (outDir string, err error) {
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
	}.Generate()
	return
}

// readStore returns the generated store glue file from an OutDir.
func readStore(t *testing.T, outDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(outDir, "valcheck_store.out.go"))
	require.NoError(t, err)
	return string(b)
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
// TestGenerateSecondIdIsPassThrough: a second EntityId column is not a
// duplicate Key — the leading EntityId binds the Key role and the rest ride
// as pass-through envelope fields (ADR-0100 Update 2026-07-09).
func TestGenerateSecondIdIsPassThrough(t *testing.T) {
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
	out, err := generateIntoDir(t, manip, a)
	require.NoError(t, err)
	store := readStore(t, out)
	require.Contains(t, store, "type ValcheckEnvelope struct")
	require.Contains(t, store, "Id2 uint64")
	// The Key still binds the leading EntityId; the DML sets both id columns
	// (via the private-control driver — ADR-0100 SD6 — in the default layout).
	require.Contains(t, store, "SetId(inst.dml, id, env.Id2)")
}

// TestGenerateInternalLayout: the default layout places the DML/RA
// scaffolding in internal/lowlevel and the store references it
// qualified, so the generated package's public surface stays the store
// family (ADR-0100 Update 2026-07-04).
func TestGenerateInternalLayout(t *testing.T) {
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
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         outDir,
		ImportPath:     "example.invalid/tmp",
	}.Generate())
	for _, f := range []string{"valcheck_dml.out.go", "valcheck_ra.out.go"} {
		_, serr := os.Stat(filepath.Join(outDir, "internal", "lowlevel", f))
		require.NoError(t, serr, "%s must land in internal/lowlevel", f)
	}
	store, err := os.ReadFile(filepath.Join(outDir, "valcheck_store.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(store), `"example.invalid/tmp/internal/lowlevel"`)
	require.Contains(t, string(store), "lowlevel.NewInEntityValcheckTable")
	scaffold, err := os.ReadFile(filepath.Join(outDir, "internal", "lowlevel", "valcheck_dml.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(scaffold), "package lowlevel")
}

// TestGenerateFlatLayout: Flat keeps everything in one package (the
// pre-Update layout); ImportPath is then not needed.
func TestGenerateFlatLayout(t *testing.T) {
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
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         outDir,
		Flat:           true,
	}.Generate())
	_, serr := os.Stat(filepath.Join(outDir, "valcheck_dml.out.go"))
	require.NoError(t, serr, "Flat must keep the scaffolding beside the store")
	store, err := os.ReadFile(filepath.Join(outDir, "valcheck_store.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(store), "dml: NewInEntityValcheckTable(alloc, 64)")
	require.NotContains(t, string(store), "lowlevel.")
	// Flat keeps the control surface exported (private control is off under
	// Flat and FullCodecs — ADR-0100 SD6), so the store drives the frame by
	// plain method calls, not the type-prefixed driver functions.
	require.Contains(t, string(store), ".BeginEntity()")
	require.NotContains(t, string(store), "InEntityValcheckTableBeginEntity(")
}

// TestGenerateRequiresImportPath: the internal layout cannot write the
// store's scaffolding import without the package's own import path.
func TestGenerateRequiresImportPath(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	td, err := validationManipulator(t, "solo").BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName:    "tmp",
		StoreName:      "Valcheck",
		TableName:      "valcheck",
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         t.TempDir(),
	}.Generate()
	require.ErrorContains(t, err, "ImportPath is required")
}

// TestGenerateRejectsMultiWordTableName: the store emitter derives the
// DML/RA class names by capitalizing TableName's first letter, so a
// multi-word name would emit non-compiling references — refuse at
// generation time.
func TestGenerateRejectsMultiWordTableName(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	td, err := validationManipulator(t, "solo").BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName:    "tmp",
		StoreName:      "Valcheck",
		TableName:      "val_check",
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         t.TempDir(),
		ImportPath:     "example.invalid/tmp",
	}.Generate()
	require.ErrorContains(t, err, "single lowercase word")
}

// TestGenerateRejectsTableNameMismatch: Input.TableName and the
// TableDesc's own name must agree — the emitted DDL and SQL use
// TableName while the desc name is what schema tooling shows.
func TestGenerateRejectsTableNameMismatch(t *testing.T) {
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	td, err := validationManipulator(t, "solo").BuildTableDesc()
	require.NoError(t, err)
	err = gen.Input{
		PackageName:    "tmp",
		StoreName:      "Valcheck",
		TableName:      "valchk",
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         t.TempDir(),
		ImportPath:     "example.invalid/tmp",
	}.Generate()
	require.ErrorContains(t, err, "disagree")
}

// TestGenerateRejectsNonRolePlainColumn: a plain column outside the three
// roles would be silently zero-written by every Begin and dropped by the
// decode — the generator must refuse until pass-through envelope fields
// exist (ADR-0100 Update 2026-07-04).
// TestGenerateRoutingColumnIsPassThrough: a non-role plain column (here an
// EntityRouting lane) is carried through the envelope — written on Begin and
// read back onto the entity — rather than rejected (ADR-0100 Update
// 2026-07-09).
func TestGenerateRoutingColumnIsPassThrough(t *testing.T) {
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
	out, err := generateIntoDir(t, manip, a)
	require.NoError(t, err)
	store := readStore(t, out)
	require.Contains(t, store, "type ValcheckEnvelope struct")
	require.Contains(t, store, "Route uint64")
	require.Contains(t, store, "SetRouting(inst.dml, env.Route)")
	require.Contains(t, store, "env ValcheckEnvelope")
}

// TestGenerateRejectsOpaquePlainColumn: Transaction/Opaque plain columns
// carry semantics the store glue does not model — they stay deferred.
func TestGenerateRejectsOpaquePlainColumn(t *testing.T) {
	manip := validationManipulator(t, "solo")
	manip.PlainValueColumn(common.PlainItemTypeOpaque, "blob", ctabb.S)
	err := generateInto(t, manip)
	require.ErrorContains(t, err, "deferred")
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
		ImportPath:     "example.invalid/tmp",
	}.Generate())
	store, err := os.ReadFile(filepath.Join(outDir, "valcheck_store.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(store), "inst.Begin(rows[i].Node, ts)")
}

// TestGenerateTrimmedCodecs: the default emission is the store-support
// codec — no exported per-kind machinery (no Columns/BuildEntities/
// FillFromArrow), AddSections/ReadRow unexported (ADR-0100 Update
// 2026-07-04).
func TestGenerateTrimmedCodecs(t *testing.T) {
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
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         outDir,
		ImportPath:     "example.invalid/tmp",
	}.Generate())
	dto, err := os.ReadFile(filepath.Join(outDir, "kind_a.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(dto), "func kindAAddSections[")
	require.Contains(t, string(dto), "func kindAReadRow[")
	require.NotContains(t, string(dto), "KindAColumns")
	require.NotContains(t, string(dto), "KindABuildEntities")
	require.NotContains(t, string(dto), "KindAFillFromArrow")
	store, err := os.ReadFile(filepath.Join(outDir, "valcheck_store.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(store), "kindAAddSections(inst.store.dml, row)")
}

// TestGenerateFullCodecs: the opt-out restores the complete exported
// codec for a package that genuinely needs the SoA batch path.
func TestGenerateFullCodecs(t *testing.T) {
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
		Table:          td,
		RowConfig:      common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths: []string{a},
		OutDir:         outDir,
		ImportPath:     "example.invalid/tmp",
		FullCodecs:     true,
	}.Generate())
	dto, err := os.ReadFile(filepath.Join(outDir, "kind_a.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(dto), "type KindAColumns struct")
	require.Contains(t, string(dto), "func KindAAddSections[")
	store, err := os.ReadFile(filepath.Join(outDir, "valcheck_store.out.go"))
	require.NoError(t, err)
	require.Contains(t, string(store), "KindAAddSections(inst.store.dml, row)")
	// FullCodecs keeps the control surface exported (private control is off,
	// so <Kind>BuildEntities can drive the frame — ADR-0100 SD6); the store
	// drives it by plain method calls, not the type-prefixed drivers.
	require.Contains(t, string(store), ".BeginEntity()")
	require.NotContains(t, string(store), "InEntityValcheckTableBeginEntity(")
}
