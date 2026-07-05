package defaults

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
)

func TestRegisterStandard(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	es := r.Entries(passreg.StagePreExecute)
	require.Len(t, es, 1)
	require.Equal(t, "ExpandLwIdMacros", es[0].Pass.Name)
	require.NotEmpty(t, es[0].Description)
	require.NotEmpty(t, es[0].Provenance)

	// Registering twice into the same registry must fail loudly (duplicate
	// key), not silently double the entries.
	require.Error(t, RegisterStandard(r))
	require.Len(t, r.Entries(passreg.StagePreExecute), 1)
}

// TestStandardSetExpandsLwIdMacros proves the wiring end to end: a query
// carrying an LW_ID_* call leaves the pre-execute stage expanded.
func TestStandardSetExpandsLwIdMacros(t *testing.T) {
	r := passreg.NewRegistry()
	require.NoError(t, RegisterStandard(r))

	out := r.ApplyBestEffort(passreg.StagePreExecute, "SELECT LW_ID_IS_VALID(id) FROM t", zerolog.Nop())
	require.NotContains(t, out, "LW_ID_IS_VALID", "macro call must be expanded")
	require.Contains(t, out, "FROM t", "surrounding query must survive")
}
