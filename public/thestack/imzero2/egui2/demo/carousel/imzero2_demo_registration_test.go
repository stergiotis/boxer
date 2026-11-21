package demo

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/registration"
)

// TestNoRegistrationFailed is ADR-0396 §2 over the whole tree: this package
// blank-imports every app, so a manifest, catalog, launch kind or
// destination refused at package initialisation shows here, as it would
// stop a host.
func TestNoRegistrationFailed(t *testing.T) {
	mintCorpusOnce(t)
	require.NoError(t, registration.Err())
}
