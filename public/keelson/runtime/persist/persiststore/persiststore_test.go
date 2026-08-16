package persiststore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/storegen"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBakedIdsAreTheVocabularys pins the committed store to the runtime
// vocabulary: every membership the State component carries is baked as the
// registry's id, not as a declaration-order ordinal. This is what makes a
// field reorder in state_dto.go a no-op for rows on disk — and what lets a
// publication of the runtime vocabulary name the id `LW_GET('stateBlob',
// '<id>')` takes.
func TestBakedIdsAreTheVocabularys(t *testing.T) {
	ids, ok := persiststore.PersistMembershipIds["State"]
	require.True(t, ok, "the store must carry the State component")
	assert.Equal(t, map[string]uint64{
		"runtimeApp":          vocab.MembRuntimeApp.GetId().Value(),
		"runtimePersistKey":   vocab.MembPersistKey.GetId().Value(),
		"runtimePersistValue": vocab.MembPersistValue.GetId().Value(),
		// Provenance added by ADR-0191 §SD5, on memberships the vocabulary
		// already had. That is the property worth pinning here: adding two
		// columns to this table minted nothing, so no id on disk moved.
		"runtimeRun":              vocab.MembRuntimeRun.GetId().Value(),
		"runtimeLifecycleTileKey": vocab.MembLifecycleTileKey.GetId().Value(),
	}, ids)
	for name, id := range ids {
		assert.Greater(t, id, uint64(1000), "%s: %d looks like a declaration-order id, not a registry id", name, id)
	}
}

// TestGenerationRefusesUnregisteredMemberships: a DTO tag naming a
// membership the vocabulary does not register must fail generation loudly,
// not bake a zero. FixedIdsWrapper emits a deliberately non-compiling
// symbol for a name it cannot resolve; the committed store carries none.
func TestGenerationRefusesUnregisteredMemberships(t *testing.T) {
	ids, err := storegen.MembershipIds(vocab.NkRegistry)
	require.NoError(t, err)
	delete(ids, "runtimePersistValue")

	manip, err := persiststore.GetPersistSchemaInManipulator()
	require.NoError(t, err)
	td, err := manip.BuildTableDesc()
	require.NoError(t, err)
	out := t.TempDir()
	err = gen.Input{
		PackageName:    "persiststore",
		StoreName:      "Persist",
		TableName:      persiststore.TableName,
		Database:       persiststore.DatabaseName,
		Table:          td,
		RowConfig:      persiststore.TableRowConfig,
		ComponentPaths: []string{"./state_dto.go"},
		OutDir:         out,
		ImportPath:     "github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore",
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: ids},
	}.Generate()
	if err == nil {
		src, rerr := os.ReadFile(filepath.Join(out, "state_dto.out.go"))
		require.NoError(t, rerr)
		assert.Contains(t, string(src), "MISSING_MEMBERSHIP_ID",
			"an unresolved membership must surface as a non-compiling symbol, never as a silent zero")
	}

	committed, err := os.ReadFile("state_dto.out.go")
	require.NoError(t, err)
	assert.NotContains(t, string(committed), "MISSING_MEMBERSHIP_ID")
}
