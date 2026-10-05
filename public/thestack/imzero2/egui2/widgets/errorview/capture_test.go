package errorview

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func flatten(ctx Context) (msgs, diags, frames string) {
	var m, d, f []string
	for _, s := range ctx.Streams {
		for _, fact := range s.Facts {
			m = append(m, fact.Msg)
			d = append(d, fact.DataDiag)
			if fact.Source != "" {
				f = append(f, FormatFrame(fact))
			}
		}
	}
	return strings.Join(m, "\n"), strings.Join(d, "\n"), strings.Join(f, "\n")
}

// TestFromErrorCarriesWrapsFieldsAndFrames: what err.Error() drops — the
// value eb attached and where each wrap happened — is in the chain.
func TestFromErrorCarriesWrapsFieldsAndFrames(t *testing.T) {
	leaf := eb.Build().Str("hours", "999").Errorf("hours must be a number from 0 to 120")
	err := eh.Errorf("fetch: %w", leaf)
	msgs, diags, frames := flatten(FromError(err))
	require.Contains(t, msgs, "hours must be a number from 0 to 120")
	require.Contains(t, msgs, "fetch: ")
	require.Contains(t, diags, `"999"`)
	require.Contains(t, frames, "capture_test.go:")
}

// TestFromErrorForeignError: an error eh did not build still shows its text.
func TestFromErrorForeignError(t *testing.T) {
	msgs, _, _ := flatten(FromError(errors.New("plain")))
	require.Contains(t, msgs, "plain")
}

func TestCaptureZeroValue(t *testing.T) {
	require.True(t, FromError(nil).IsEmpty())
	require.True(t, Capture(nil).IsEmpty())
	require.True(t, Captured{}.IsEmpty())
	require.NoError(t, Captured{}.Err())

	err := eh.New("boom")
	got := Capture(err)
	require.False(t, got.IsEmpty())
	require.Same(t, err, got.Err())
	require.False(t, got.Chain().IsEmpty())
}
