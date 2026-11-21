package factsschema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
)

func TestReadTable_DefaultsToTheFactsTable(t *testing.T) {
	envtest.Set(t, ReadTableSetting, "")
	ref, err := ReadTable()
	require.NoError(t, err)
	assert.Equal(t, "boxer.facts", ref)
	ref, err = ReadTableFor(QualifiedTableName)
	require.NoError(t, err)
	assert.Empty(t, ref, "with nothing set a store reads the table it writes")
}

func TestReadTable_TheSettingReachesOnlyStoresOverTheFactsTable(t *testing.T) {
	envtest.Set(t, ReadTableSetting, "carrier.facts_view")
	ref, err := ReadTable()
	require.NoError(t, err)
	assert.Equal(t, "carrier.facts_view", ref)
	ref, err = ReadTableFor(QualifiedTableName)
	require.NoError(t, err)
	assert.Equal(t, "carrier.facts_view", ref)
	ref, err = ReadTableFor("scratch.facts")
	require.NoError(t, err)
	assert.Empty(t, ref, "a store over another table is unaffected")
}

func TestReadTable_RefusesAReferenceItCannotSplice(t *testing.T) {
	envtest.Set(t, ReadTableSetting, "carrier.facts_view; DROP TABLE x")
	_, err := ReadTable()
	require.Error(t, err)
	_, err = ReadTableFor(QualifiedTableName)
	require.Error(t, err)
}
