package ph

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
)

var errSentinel = errors.New("sentinel")

//go:noinline
func panickingSite() error {
	var s []int
	_ = s[3]
	return nil
}

func TestGuardReturnsTheError(t *testing.T) {
	assert.ErrorIs(t, Guard(func() error { return errSentinel }), errSentinel)
	assert.NoError(t, Guard(func() error { return nil }))
}

func TestGuardCarriesThePanicSiteStack(t *testing.T) {
	err := Guard(panickingSite)
	require.Error(t, err)
	st, ok := err.(interface{ StackTrace() eh.StackTrace })
	require.True(t, ok, "the error carries a stack")
	found := false
	for _, f := range st.StackTrace() {
		if strings.HasSuffix(f.Function, ".panickingSite") {
			found = true
		}
	}
	assert.True(t, found, "the stack names the function that panicked")
}

func TestGuardWrapsAnErrorPanicValue(t *testing.T) {
	err := Guard(func() error { panic(errSentinel) })
	assert.ErrorIs(t, err, errSentinel)
	assert.ErrorIs(t, err, ErrPanic)
	assert.Equal(t, "recovering from panic: sentinel", err.Error())
	err = Guard(func() error { panic("boom") })
	assert.ErrorIs(t, err, ErrPanic)
	assert.Equal(t, "recovering from panic", err.Error())
	assert.NotErrorIs(t, Guard(func() error { return errSentinel }), ErrPanic)
}
