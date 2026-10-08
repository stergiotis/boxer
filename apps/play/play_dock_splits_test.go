package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyDockSplits(t *testing.T) {
	got, err := ApplyDockSplits(DefaultDockSplits, " editor=0.3 , tools=0.8,,")
	require.NoError(t, err)
	want := DefaultDockSplits
	want.Editor, want.Tools = 0.3, 0.8
	require.Equal(t, want, got)

	for _, bad := range []string{"editor", "editor=1", "editor=0", "editor=x", "middle=0.5"} {
		got, err = ApplyDockSplits(DefaultDockSplits, bad)
		require.Error(t, err, bad)
		require.Equal(t, DefaultDockSplits, got, bad)
	}
}
