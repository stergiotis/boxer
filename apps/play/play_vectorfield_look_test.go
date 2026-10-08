package play

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
)

const vectorFieldLookChildKey = "BOXER_TEST_VECTORFIELD_LOOK_CHILD"

// requireTrailsReadable checks every palette stop against the map's two
// grounds at the UI-component floor: a trail is drawn over ocean and land
// alike.
func requireTrailsReadable(t *testing.T, l vectorFieldLook) {
	t.Helper()
	floor := contrast.AAFloor(contrast.KindUI)
	for _, ground := range []uint32{l.ocean, l.land} {
		for _, stop := range l.palette {
			r := contrast.Ratio(uint8(stop>>24), uint8(stop>>16), uint8(stop>>8),
				uint8(ground>>24), uint8(ground>>16), uint8(ground>>8))
			require.GreaterOrEqualf(t, r, floor, "stop #%08x on ground #%08x", stop, ground)
		}
	}
}

// TestVectorFieldLookDark is the default theme: the dark map and the ramp
// made for it.
func TestVectorFieldLookDark(t *testing.T) {
	if styletokens.ActiveTheme() != styletokens.ThemeDark {
		t.Skip("the process runs another theme")
	}
	require.Equal(t, flowoverlay.DefaultPalette, vfLook.palette)
	requireTrailsReadable(t, vfLook)
}

// TestVectorFieldLookFresh runs the check under IMZERO2_THEME=fresh in a
// child process: the theme is fixed at package initialisation, so this
// process cannot switch to it.
func TestVectorFieldLookFresh(t *testing.T) {
	if os.Getenv(vectorFieldLookChildKey) != "" {
		require.Equal(t, styletokens.ThemeFresh, styletokens.ActiveTheme())
		require.Equal(t, flowoverlay.LightPalette, vfLook.palette)
		requireTrailsReadable(t, vfLook)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestVectorFieldLookFresh$", "-test.v")
	cmd.Env = append(os.Environ(), vectorFieldLookChildKey+"=1", "IMZERO2_THEME=fresh")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	require.Contains(t, string(out), "--- PASS: TestVectorFieldLookFresh")
}
