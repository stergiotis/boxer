package meshdemo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/meshdemo"
)

// A store generated against the facts table reads the relation the
// deployment's setting names without its caller passing it (ADR-0402 §SD3).
func TestFleetStore_ReadsTheConfiguredFactsReadTable(t *testing.T) {
	envtest.Set(t, factsschema.ReadTableSetting, "carrier.facts_view")

	byDefault := meshdemo.NewFleetStore(nil, nil, meshdemo.FleetStoreConfig{})
	assert.Equal(t, "carrier.facts_view", byDefault.SchemaContract().Table)

	// The setting is the facts table's: a store pointed at another table
	// reads that table.
	scratch := meshdemo.NewFleetStore(nil, nil, meshdemo.FleetStoreConfig{Table: "scratch.facts"})
	assert.Equal(t, "scratch.facts", scratch.SchemaContract().Table)

	// What the caller names wins, the written table included.
	explicit := meshdemo.NewFleetStore(nil, nil, meshdemo.FleetStoreConfig{ReadTable: "other.relation"})
	assert.Equal(t, "other.relation", explicit.SchemaContract().Table)
	written := meshdemo.NewFleetStore(nil, nil, meshdemo.FleetStoreConfig{ReadTable: meshdemo.FleetTableName})
	assert.Equal(t, meshdemo.FleetTableName, written.SchemaContract().Table)
}

func TestFleetStore_ReadsTheFactsTableWhenNothingIsSet(t *testing.T) {
	envtest.Set(t, factsschema.ReadTableSetting, "")
	store := meshdemo.NewFleetStore(nil, nil, meshdemo.FleetStoreConfig{})
	assert.Equal(t, meshdemo.FleetTableName, store.SchemaContract().Table)
}

// A setting that cannot be spliced into SQL stops the store at
// construction, naming the variable, instead of reading the written table.
func TestFleetStore_RefusesAnUnusableSetting(t *testing.T) {
	envtest.Set(t, factsschema.ReadTableSetting, "facts_view; DROP TABLE x")
	defer func() {
		r := recover()
		require.NotNil(t, r, "the constructor panics")
		msg, _ := r.(string)
		assert.Contains(t, msg, "FleetStore: ReadTable")
		assert.Contains(t, msg, "BOXER_FACTS_READ_TABLE")
	}()
	meshdemo.NewFleetStore(nil, nil, meshdemo.FleetStoreConfig{})
}
