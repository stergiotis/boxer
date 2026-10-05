package drivecmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestStepFlagKeepsWhitespaceBesideCommas(t *testing.T) {
	// The flag library splits a slice value on commas; the pieces are joined
	// back with commas, which is exact only if no piece was trimmed.
	step := `{"do":"type","role":"text_input","text":"SELECT a, b FROM t"}`
	cmd := NewCommand()
	var got string
	cmd.Action = func(ctx *cli.Context) error {
		got = strings.Join(ctx.StringSlice(flagStep), ",")
		return nil
	}
	app := &cli.App{Commands: []*cli.Command{cmd}}
	require.NoError(t, app.Run([]string{"imzero2", cmd.Name, "--" + flagStep, step}))
	assert.Equal(t, step, got)
}
