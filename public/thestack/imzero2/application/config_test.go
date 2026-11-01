package application

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassthroughArgsOmitsWhatIsNotSet(t *testing.T) {
	var cfg ImZeroClientConfig
	assert.Empty(t, cfg.PassthroughArgs(nil))
}

func TestPassthroughArgsCarriesFullscreenAndMonitor(t *testing.T) {
	cfg := ImZeroClientConfig{Fullscreen: "on", Monitor: "1"}
	assert.Equal(t, []string{"-fullscreen", "on", "-monitor", "1"}, cfg.PassthroughArgs(nil))
}

// The client aborts on an argument it does not consume, and it silently ignores
// one it spells differently from the host: `-fullscreen` was once read as
// `-fullscrreen`, so nothing could turn it on and no test noticed. Every
// argument the host can pass must be one the client's parser names.
func TestEveryPassthroughArgIsOneTheClientReads(t *testing.T) {
	src, err := os.ReadFile("../../../../rust/imzero2/src/imzero2/appconfig.rs")
	require.NoError(t, err)
	cfg := ImZeroClientConfig{
		AppTitle:                "x",
		Fullscreen:              "x",
		Monitor:                 "x",
		InitialMainWindowWidth:  "x",
		InitialMainWindowHeight: "x",
		AllowMainWindowResize:   "x",
		ExportBasePath:          "x",
		Vsync:                   "x",
		BackgroundColorRGBA:     "x",
		BackdropFilter:          "x",
	}
	args := cfg.PassthroughArgs(nil)
	require.NotEmpty(t, args)
	for i := 0; i < len(args); i += 2 {
		assert.True(t, strings.Contains(string(src), `"`+args[i]+`"`), "the client does not read %s", args[i])
	}
}
