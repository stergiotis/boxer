package capinspector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
)

// The Facts cap's backend owns boxer.facts; the section header and the
// schema the inspector renders both come from factsschema, so the
// coordinates must not drift apart from it.
func TestCapFacts_DeclaresTheBoxerFactsSchema(t *testing.T) {
	spec := Registry[CapFacts]
	require.NotNil(t, spec.Schema, "the facts cap must declare its storage schema")
	assert.Equal(t, factsschema.DatabaseName, spec.Schema.Database)
	assert.Equal(t, factsschema.TableName, spec.Schema.Table)
	assert.Equal(t, "boxer.facts", spec.Schema.Qualified())
	require.NotNil(t, spec.Schema.Load, "a declared schema needs a loader")
}

// The persist cap's durable backend owns boxer.persiststate (ADR-0105
// D3a); its coordinates come from the generated store's package, and its
// declared backends carry the ids the carousel reports — the status-bar
// labels — so the schematic can highlight the effective one.
func TestCapPersist_DeclaresTheStoreSchemaAndBothBackends(t *testing.T) {
	spec := Registry[CapPersist]
	require.NotNil(t, spec.Schema, "the persist cap must declare its storage schema")
	assert.Equal(t, persiststore.DatabaseName, spec.Schema.Database)
	assert.Equal(t, persiststore.TableName, spec.Schema.Table)
	assert.Equal(t, "boxer.persiststate", spec.Schema.Qualified())
	require.NotNil(t, spec.Schema.Load, "a declared schema needs a loader")

	ids := make([]string, 0, len(spec.Backends))
	for _, b := range spec.Backends {
		ids = append(ids, b.Id)
	}
	assert.Equal(t, []string{"store", "mem"}, ids,
		"the persist backend ids must be the labels selectPersistBackend returns")

	tbl, err := loadPersistTableDesc()
	require.NoError(t, err)
	require.NotNil(t, tbl)
	assert.NotEmpty(t, tbl.PlainValuesNames, "boxer.persiststate has plain envelope columns")
	assert.NotEmpty(t, tbl.TaggedValuesSections, "boxer.persiststate has tagged value sections")
	again, err := loadPersistTableDesc()
	require.NoError(t, err)
	assert.Same(t, tbl, again, "the loader must memoise")
}

// Every other cap leaves Schema nil — the rest land their rows in the
// facts table. Adding a table to one of them means updating this list,
// which is the point: the section appears the moment a cap declares one.
func TestRegistry_OnlyFactsAndPersistCarryASchema(t *testing.T) {
	for _, capId := range allCapIdsOrdered() {
		spec := Registry[capId]
		if capId == CapFacts || capId == CapPersist {
			continue
		}
		assert.Nilf(t, spec.Schema, "cap %q declares a storage schema; add it to the expected set", capId)
	}
}

// The TableDesc is built once per process and shared across windows —
// schemaview reads it and mutates only its own Model — so the loader
// must hand back the same pointer every time, not rebuild per frame.
func TestLoadFactsTableDesc_BuildsOnceAndIsShared(t *testing.T) {
	tbl, err := loadFactsTableDesc()
	require.NoError(t, err)
	require.NotNil(t, tbl)
	assert.NotEmpty(t, tbl.PlainValuesNames, "boxer.facts has plain identity columns")
	assert.NotEmpty(t, tbl.TaggedValuesSections, "boxer.facts has tagged value sections")

	again, err := loadFactsTableDesc()
	require.NoError(t, err)
	assert.Same(t, tbl, again, "the loader must memoise; a rebuild per frame would reset nothing but cost the pipeline")
}

// The widget derives absolute ids (its legend window, its pane probe)
// from the scope key, so two inspector windows must not produce the same
// one. The prefix folds the host's per-window salt; here the two stacks
// stand in for two windows.
func TestSchemaScopePrefix_DiffersPerInstanceAndIsStable(t *testing.T) {
	a, b := newApp(), newApp()
	// SetBaseSalt stands in for the host's per-window id scope: it is the
	// empty stack's XOR base, which is what a stack read outside a pushed
	// scope falls back to.
	a.ids.SetBaseSalt(0x51d3_0f11_0000_0001)
	b.ids.SetBaseSalt(0x51d3_0f11_0000_0002)

	pa, pb := a.schemaScopePrefix(), b.schemaScopePrefix()
	assert.NotEmpty(t, pa)
	assert.NotEqual(t, pa, pb, "two windows must not share a scope key")
	assert.Equal(t, pa, a.schemaScopePrefix(), "the prefix must be stable across frames")
}
