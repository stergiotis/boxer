package example

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// generateReadTableDefault emits the minimal valcheck store with the given
// ReadTableDefault and returns the store source.
func generateReadTableDefault(t *testing.T, ref *gen.FuncRef) (store string, err error) {
	t.Helper()
	dir := t.TempDir()
	a := writeDTO(t, dir, "kind_a.go", `package tmp

type KindA struct {
	_  struct{} `+"`kind:\"kindA\"`"+`
	ID uint64   `+"`lw:\",id\"`"+`
	A  string   `+"`lw:\"fieldA,solo\"`"+`
}
`)
	td, terr := validationManipulator(t, "solo").BuildTableDesc()
	require.NoError(t, terr)
	outDir := t.TempDir()
	err = gen.Input{
		PackageName:      "tmp",
		StoreName:        "Valcheck",
		TableName:        "valcheck",
		Table:            td,
		RowConfig:        common.TableRowConfigMultiAttributesPerRow,
		ComponentPaths:   []string{a},
		OutDir:           outDir,
		ImportPath:       "example.invalid/tmp",
		ReadTableDefault: ref,
	}.Generate()
	if err != nil {
		return
	}
	store = readStore(t, outDir)
	return
}

// A store generated with a read-table default asks the named function,
// with the table it writes to, when its configuration names no read table —
// and only then.
func TestGenerateReadTableDefaultAsksTheNamedFunction(t *testing.T) {
	store, err := generateReadTableDefault(t, &gen.FuncRef{ImportPath: "example.invalid/settings", Package: "settings", Name: "ReadTableFor"})
	require.NoError(t, err)
	require.Contains(t, store, `"example.invalid/settings"`)
	require.Contains(t, store, "if cfg.ReadTable == \"\" {")
	require.Contains(t, store, "written = ValcheckTableName")
	require.Contains(t, store, "settings.ReadTableFor(written)")
	require.Contains(t, store, "asks settings.ReadTableFor for the relation to read")
}

// Without one the store is what it was: no call, no import, and the read
// table an empty configuration leaves is the table written to. A store that
// owns its table must not start following a setting meant for another.
func TestGenerateWithoutReadTableDefaultReadsTheWrittenTable(t *testing.T) {
	store, err := generateReadTableDefault(t, nil)
	require.NoError(t, err)
	require.NotContains(t, store, "ReadTableFor")
	require.NotContains(t, store, "if cfg.ReadTable == \"\" {")
	require.Contains(t, store, "empty (the default) reads the table written to")
}

func TestGenerateReadTableDefaultRefusesAnUnusableReference(t *testing.T) {
	for name, ref := range map[string]*gen.FuncRef{
		"no import path": {Package: "settings", Name: "ReadTableFor"},
		"no package":     {ImportPath: "example.invalid/settings", Name: "ReadTableFor"},
		"not a name":     {ImportPath: "example.invalid/settings", Package: "settings", Name: "Read Table"},
	} {
		_, err := generateReadTableDefault(t, ref)
		require.Error(t, err, name)
	}
}
