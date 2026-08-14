package example

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// generateProvisioning emits the minimal valcheck store with the
// ExternallyProvisioned switch in the given position and returns the store
// source plus its OutDir.
func generateProvisioning(t *testing.T, external bool) (store string, outDir string) {
	t.Helper()
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
	outDir = t.TempDir()
	require.NoError(t, gen.Input{
		PackageName:           "tmp",
		StoreName:             "Valcheck",
		TableName:             "valcheck",
		Table:                 td,
		RowConfig:             common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths:        []string{a},
		OutDir:                outDir,
		ImportPath:            "example.invalid/tmp",
		ExternallyProvisioned: external,
	}.Generate())
	return readStore(t, outDir), outDir
}

// TestGenerateExternallyProvisionedOmitsDDL: a store whose table is created
// by something else must carry no way to run DDL — no EnsureTable, no
// embedded CREATE TABLE string, and no DDLTail config field (which exists
// only as EnsureTable's raw suffix, so keeping it would be config that
// silently does nothing). The schema artefacts stay: the .out.sql file is
// what the external provisioner needs, and VerifySchema is the only drift
// check left.
func TestGenerateExternallyProvisionedOmitsDDL(t *testing.T) {
	store, outDir := generateProvisioning(t, true)

	require.NotContains(t, store, "func (inst *ValcheckStore) EnsureTable(")
	require.NotContains(t, store, "DDLTail")
	require.NotContains(t, store, "valcheckDDLCreate")
	require.NotContains(t, store, "//go:embed")
	// The blank embed import is legal while unused, but it advertises an
	// embedding this store no longer does.
	require.NotContains(t, store, `_ "embed"`)

	// Still a store, and still drift-checkable.
	require.Contains(t, store, "func (inst *ValcheckStore) VerifySchema(")
	require.Contains(t, store, "func (inst *ValcheckStore) Flush(")
	require.Contains(t, store, `const ValcheckTableName = "valcheck"`)

	// The physical schema is still emitted — whoever provisions the table
	// needs the shape the positional decode expects.
	ddl, err := os.ReadFile(filepath.Join(outDir, "valcheck_ddl_clickhouse.out.sql"))
	require.NoError(t, err)
	require.Contains(t, string(ddl), "CREATE TABLE IF NOT EXISTS valcheck")
}

// TestGenerateProvisioningDefaultEmitsDDL pins the default: leaving
// ExternallyProvisioned unset keeps the self-provisioning store whole, so
// the switch cannot change existing output by omission.
func TestGenerateProvisioningDefaultEmitsDDL(t *testing.T) {
	store, _ := generateProvisioning(t, false)

	require.Contains(t, store, "func (inst *ValcheckStore) EnsureTable(")
	require.Contains(t, store, "DDLTail string")
	require.Contains(t, store, "//go:embed valcheck_ddl_clickhouse.out.sql")
	require.Contains(t, store, `_ "embed"`)
}

// TestGenerateProvisioningSwitchIsLocal: the two emissions must differ only
// in the DDL surface. Everything else — the envelope, the verbs, the
// component decode — is emitted from the same schema, so a stray dependency
// on the switch would show up as a difference outside those blocks.
func TestGenerateProvisioningSwitchIsLocal(t *testing.T) {
	external, _ := generateProvisioning(t, true)
	self, _ := generateProvisioning(t, false)

	// Strip the four DDL-bearing regions from the self-provisioning store;
	// what remains must be exactly the ExternallyProvisioned emission apart
	// from VerifySchema's own doc paragraph (which states which regime it is
	// guarding).
	stripped := self
	stripped = cutRegion(t, stripped, "\t_ \"embed\"\n", "")
	stripped = cutRegion(t, stripped, "// The complete CREATE TABLE composed", "// ValcheckTableName is")
	stripped = cutRegion(t, stripped, "\t// DDLTail is a raw suffix", "\t// Stampers are consulted")
	stripped = cutRegion(t, stripped, "// EnsureTable applies", "// VerifySchema compares")

	require.Equal(t, dropVerifySchemaDoc(t, external), dropVerifySchemaDoc(t, stripped))
}

// cutRegion removes src[start:end), both located by their first occurrence.
// An empty end cuts through the end of start's own line. Both anchors must
// be present — an emitter that moved should fail loudly here rather than
// quietly compare less.
func cutRegion(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	require.GreaterOrEqual(t, i, 0, "anchor %q not found — the emitter moved, update this test", start)
	j := i + len(start)
	if end != "" {
		rel := strings.Index(src[j:], end)
		require.GreaterOrEqual(t, rel, 0, "anchor %q not found after %q — the emitter moved, update this test", end, start)
		j += rel
	}
	return src[:i] + src[j:]
}

// dropVerifySchemaDoc removes VerifySchema's leading doc paragraph — the one
// text that legitimately differs between the two regimes.
func dropVerifySchemaDoc(t *testing.T, src string) string {
	t.Helper()
	return cutRegion(t, src, "// VerifySchema compares", "//\n// It checks the COLUMN contract only.")
}
