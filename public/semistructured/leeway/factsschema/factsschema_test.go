package factsschema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSchemaInManipulator_OK(t *testing.T) {
	manip, err := GetSchemaInManipulator()
	require.NoError(t, err)
	require.NotNil(t, manip)
}

func TestGetSchemaInManipulator_BuildTableDescSucceeds(t *testing.T) {
	manip, err := GetSchemaInManipulator()
	require.NoError(t, err)
	tblDesc, err := manip.BuildTableDesc()
	require.NoError(t, err)
	assert.NotEmpty(t, tblDesc.PlainValuesNames, "expect at least one plain value column")
	assert.NotEmpty(t, tblDesc.TaggedValuesSections, "expect at least one tagged value section")
}
