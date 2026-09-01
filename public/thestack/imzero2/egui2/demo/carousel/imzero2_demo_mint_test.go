package demo

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/sqlapplet"
)

// mintOnce guards the applet corpus mint for the whole package. Minting
// registers each document into the process-global app registry, so a second
// call fails every document with "duplicate Id" — which is a test-ordering
// artefact, not a finding. Tests in this package that need the applet corpus
// present call mintCorpusOnce instead of MintManifests.
var (
	mintOnce sync.Once
	mintErrs []error
	mintN    int
)

// mintCorpusOnce mints the ADR-0132 applet corpus into the registry the first
// time any test asks, and asserts it minted cleanly every time. The error
// assertion is per-caller rather than inside the Once so a test that depends
// on a clean corpus still fails when the corpus is broken, whichever test
// happened to run first.
func mintCorpusOnce(t *testing.T) (n int) {
	t.Helper()
	mintOnce.Do(func() {
		mintN, mintErrs = sqlapplet.MintManifests(zerolog.Nop())
	})
	require.Empty(t, mintErrs, "the applet corpus must mint cleanly")
	n = mintN
	return
}
