package diskbacked

import (
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/caching"
	"github.com/stergiotis/boxer/public/caching/stashtest"
	"github.com/stretchr/testify/require"
)

func TestPebbleStash_Conformance(t *testing.T) {
	stashtest.Run(t, func(t *testing.T, capacity int) caching.StashBackendI[string, int] {
		s, err := NewPebbleStash[string, int](filepath.Join(t.TempDir(), "pebble"), capacity, true)
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		return s
	}, stashtest.Opts{SupportsUnbounded: true})
}
