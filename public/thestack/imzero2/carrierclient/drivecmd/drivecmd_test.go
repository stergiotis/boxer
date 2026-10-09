package drivecmd

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestStepFlagKeepsWhitespaceBesideCommas(t *testing.T) {
	// The flag library splits a slice value on commas; the pieces are joined
	// back with commas, which is exact only if no piece was trimmed.
	step := `{"do":"type","role":"text_input","text":"SELECT a, b FROM t"}`
	cmd := NewCommand()
	var got string
	cmd.Action = func(ctx context.Context, cmd *cli.Command) error {
		got = strings.Join(cmd.StringSlice(flagStep), ",")
		return nil
	}
	app := &cli.Command{Commands: []*cli.Command{cmd}}
	require.NoError(t, app.Run(context.Background(), []string{"imzero2", cmd.Name, "--" + flagStep, step}))
	assert.Equal(t, step, got)
}
