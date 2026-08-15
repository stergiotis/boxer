package contract

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stretchr/testify/require"
)

// Regression for review G-2: ValidateMembershipVerbatimHumanReadable was
// inverted — it errored on valid names and accepted invalid ones. Mirror the
// (correct) ValidateNaturalKeyHumanReadable polarity.
func TestValidateMembershipVerbatimHumanReadable(t *testing.T) {
	c := NewVcsManagedContract()
	require.NoError(t, c.ValidateMembershipVerbatimHumanReadable(naming.StylableName("valid")), "a valid name must pass")
	require.Error(t, c.ValidateMembershipVerbatimHumanReadable(naming.StylableName("")), "an invalid (empty) name must be rejected")
}
