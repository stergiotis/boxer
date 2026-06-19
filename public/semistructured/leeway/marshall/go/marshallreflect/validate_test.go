package marshallreflect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// validateMissingSectionDrone targets a section (baz) the recordingDML has no
// GetSection getter for — the preflight should name the missing getter.
type validateMissingSectionDrone struct {
	_          struct{} `kind:"vmsd"`
	Id         uint64   `lw:",id"`
	NaturalKey []byte   `lw:",naturalKey"`
	Val        string   `lw:"m,baz"`
}

// validateBadChannelDrone targets the symbol section (which recordingDML does
// have) but on the highCardRef channel, whose AddMembershipHighCardRefP the
// recordingAttr lacks — the kind of mismatch that panics mid-marshal today.
type validateBadChannelDrone struct {
	_          struct{} `kind:"vbcd"`
	Id         uint64   `lw:",id"`
	NaturalKey []byte   `lw:",naturalKey"`
	Val        string   `lw:"sensor,symbol,highCardRef"`
}

// TestValidate_Accepts confirms a DML that satisfies the contract (the same
// recordingDML that Marshal drives) passes the preflight.
func TestValidate_Accepts(t *testing.T) {
	require.NoError(t, marshallreflect.Validate[mixedVerbatimDrone](&recordingDML{}))
}

// TestValidate_NilDML rejects a nil DML up front.
func TestValidate_NilDML(t *testing.T) {
	require.Error(t, marshallreflect.Validate[mixedVerbatimDrone](nil))
}

// TestValidate_MissingSectionGetter reports the absent GetSection<X> by name
// instead of letting it panic when the first row reaches that section.
func TestValidate_MissingSectionGetter(t *testing.T) {
	err := marshallreflect.Validate[validateMissingSectionDrone](&recordingDML{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GetSectionBaz")
}

// TestValidate_ChannelMethodMismatch reports the channel's missing
// AddMembership…P on the attribute type, the mismatch the typed contract
// could not express.
func TestValidate_ChannelMethodMismatch(t *testing.T) {
	err := marshallreflect.Validate[validateBadChannelDrone](&recordingDML{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "AddMembershipHighCardRefP")
}
