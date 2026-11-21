package registration

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailRecordsAndJoins(t *testing.T) {
	before := len(Failures())
	Fail("x", nil)
	require.Len(t, Failures(), before)
	Fail("x", errors.New("first"))
	Fail("y", errors.New("second"))
	errs := Failures()
	require.Len(t, errs, before+2)
	require.ErrorContains(t, Err(), "x: first")
	require.ErrorContains(t, Err(), "y: second")
}
