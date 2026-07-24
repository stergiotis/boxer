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

// panickingDML satisfies the entity frame but panics from inside a method the
// codec calls — standing in for a bug in the DML, as opposed to a caller
// failing the write contract.
type panickingDML struct{ recordingDML }

func (p *panickingDML) BeginEntity() { panic("boom from the DML") }

// A caller that skips the preflight and drives a DML missing a method must get
// an ERROR, not a panic out of a library call. Validate remains the better
// diagnostic (it reports every mismatch at once); this is the backstop, and it
// keeps the package's failure mode uniform — everything else here returns an
// error too.
func TestMarshal_MissingMethodIsAnError(t *testing.T) {
	rows := []validateMissingSectionDrone{{Id: 1, NaturalKey: []byte("k"), Val: "v"}}
	err := marshallreflect.Marshal(&recordingDML{}, rows, marshallreflect.MapLookup{"m": 1})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GetSectionBaz")
}

// The same backstop on the RowComposer entry points.
func TestRowComposer_MissingMethodIsAnError(t *testing.T) {
	c := marshallreflect.NewRowComposer(&recordingDML{}, marshallreflect.MapLookup{"m": 1})
	err := c.BeginRow(validateMissingSectionDrone{Id: 1, NaturalKey: []byte("k"), Val: "v"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GetSectionBaz")
}

// A panic that is NOT a contract violation must pass through untouched — the
// recover is a translation of one specific caller mistake, not a blanket
// swallow that would hide a bug in this package or in the DML.
func TestMarshal_ForeignPanicIsNotSwallowed(t *testing.T) {
	rows := []mixedVerbatimDrone{{Id: 1, NaturalKey: []byte("k")}}
	require.PanicsWithValue(t, "boom from the DML", func() {
		_ = marshallreflect.Marshal(&panickingDML{}, rows, marshallreflect.NoLookup{})
	})
}
