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
// vocabulary: every membership each component carries is baked as the
// registry's id, not as a declaration-order ordinal. This is what makes a
// field reorder in a DTO a no-op for rows on disk — and what lets a
// publication of the runtime vocabulary name the id `LW_GET('blob', '<id>')`
// takes.
//
// The four maps are also disjoint, which is the property that lets the
// kinds share the typed sections at all: decode matches (section,
// membership id), never "kind".
func TestBakedIdsAreTheVocabularys(t *testing.T) {
	want := map[string]map[string]uint64{
		"Owner": {
			"runtimeApp":              vocab.MembRuntimeApp.GetId().Value(),
			"runtimeRun":              vocab.MembRuntimeRun.GetId().Value(),
			"runtimeLifecycleTileKey": vocab.MembLifecycleTileKey.GetId().Value(),
		},
		"State": {
			"runtimePersistKey":   vocab.MembPersistKey.GetId().Value(),
			"runtimePersistValue": vocab.MembPersistValue.GetId().Value(),
		},
		// Workingset and ColumnWidth reuse the memberships their facts rows
		// carried, so the move minted nothing and no id changed meaning.
		"Workingset": {
			"runtimeWorkingsetName":      vocab.MembWorkingsetName.GetId().Value(),
			"runtimeLaunchConfigKind":    vocab.MembLaunchConfigKind.GetId().Value(),
			"runtimeLaunchConfig":        vocab.MembLaunchConfig.GetId().Value(),
			"runtimeLifecycleStopReason": vocab.MembLifecycleStopReason.GetId().Value(),
		},
		"ColumnWidth": {
			"runtimeColWidthTier":      vocab.MembColWidthTier.GetId().Value(),
			"runtimeColWidthScope":     vocab.MembColWidthScope.GetId().Value(),
			"runtimeColWidthColumnKey": vocab.MembColWidthColumnKey.GetId().Value(),
			"runtimeColWidthPoints":    vocab.MembColWidthPoints.GetId().Value(),
			"runtimeColWidthFontSize":  vocab.MembColWidthFontSize.GetId().Value(),
		},
	}
	assert.Equal(t, want, persiststore.PersistMembershipIds)
	seen := map[uint64]string{}
	for kind, ids := range persiststore.PersistMembershipIds {
		for name, id := range ids {
			assert.Greater(t, id, uint64(1000), "%s.%s: %d looks like a declaration-order id, not a registry id", kind, name, id)
			prev, dup := seen[id]
			assert.False(t, dup, "%s.%s shares id %d with %s", kind, name, id, prev)
			seen[id] = kind + "." + name
		}
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
