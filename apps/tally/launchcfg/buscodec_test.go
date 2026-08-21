package launchcfg_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func TestRoundTrip(t *testing.T) {
	in := launchcfg.TallyLaunch{
		At:     time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		MountA: "3bfe363bcf148002", SnapA: "", DirA: "doc/adr",
		MountB: "3bfe363bcf148003", SnapB: "2026-08-20T21:04:54.483286742Z", DirB: ".",
		Sync: true, Target: "B",
	}
	b, err := buscodec.Encode(in)
	require.NoError(t, err)
	out, err := buscodec.Decode[launchcfg.TallyLaunch](b)
	require.NoError(t, err)
	assert.Equal(t, in.MountA, out.MountA)
	assert.Equal(t, in.SnapA, out.SnapA)
	assert.Equal(t, in.DirA, out.DirA)
	assert.Equal(t, in.MountB, out.MountB)
	assert.Equal(t, in.SnapB, out.SnapB)
	assert.Equal(t, in.DirB, out.DirB)
	assert.Equal(t, in.Sync, out.Sync)
	assert.Equal(t, in.Target, out.Target)
}

func TestKindIsRegisteredForTheHostProbe(t *testing.T) {
	in := launchcfg.TallyLaunch{MountA: "x", DirA: ".", Target: "A"}
	b, err := buscodec.Encode(in)
	require.NoError(t, err)
	require.NoError(t, kindcheck.Check(launchcfg.Kind, b))
	assert.Error(t, kindcheck.Check(launchcfg.Kind, []byte("not a config")))
}
