package registry

import (
	"testing"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stretchr/testify/require"
)

func newTestNkRegistry(t *testing.T) *HumanReadableNaturalKeyRegistry[*contract.EphemeralContract] {
	t.Helper()
	reg, err := NewNaturalKeyRegistry[*contract.EphemeralContract](testClaimReview, 8, naming.LowerSnakeCase, contract.NewEphemeralContract())
	require.NoError(t, err)
	return reg
}

// Regression for review G-4: SetFinal did not call register, so the registry's
// stored copy never carried the Final flag and Lookup diverged from the
// returned handle.
func TestSetFinalPersistsToStoredCopy(t *testing.T) {
	reg := newTestNkRegistry(t)
	handle := reg.MustBegin("with_final", 0).SetFinal().End()
	require.True(t, handle.GetFlags().HasFinal(), "returned handle must carry Final")

	got, err := reg.Lookup("with_final")
	require.NoError(t, err)
	require.True(t, got.GetFlags().HasFinal(), "stored copy (via Lookup) must also carry Final")
}

// Regression for review G-3: ClearFinal executed flags.ClearVirtual(), so the
// Final flag survived a ClearFinal call.
func TestClearFinalClearsFinal(t *testing.T) {
	reg := newTestNkRegistry(t)
	handle := reg.MustBegin("toggle_final", 0).SetFinal().ClearFinal().End()
	require.False(t, handle.GetFlags().HasFinal(), "ClearFinal must actually clear the Final flag")
}

// Regression for review G-5: a same-origin re-Begin of an existing key minted a
// fresh TaggedId from the grown lookup length and overwrote the record, so the
// natural-key -> id mapping was unstable. Re-Begin from the same call site must
// be idempotent. (Calling from the same source line keeps the origin equal; a
// genuinely different location is still rejected as a collision.)
func TestReBeginIsIdempotent(t *testing.T) {
	reg := newTestNkRegistry(t)
	reg.MustBegin("filler", 0).End() // grow the lookup so a re-mint would differ

	ids := make([]identifier.TaggedId, 0, 3)
	for range 3 {
		ids = append(ids, reg.MustBegin("repeated", 1).End().GetId()) // same source line => same origin
	}
	require.Equal(t, ids[0], ids[1], "re-Begin must return the same id")
	require.Equal(t, ids[0], ids[2], "re-Begin must return the same id")
}
