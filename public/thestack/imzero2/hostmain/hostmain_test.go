package hostmain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/logging"
)

// New keeps the binary's own name, version and Before, and adds the host's
// flags, its command tree and the exit hook.
func TestNewDecoratesTheBinarysApp(t *testing.T) {
	a := &cli.Command{Name: "acme", Version: "v0", Before: logging.Apply}
	p := New(Options{}, a)
	require.Same(t, a, p.app)
	require.Equal(t, "acme", a.Name)
	require.Equal(t, "v0", a.Version)
	require.NotNil(t, a.Before)
	require.NotNil(t, a.After)

	names := make(map[string]bool, len(a.Flags))
	for _, f := range a.Flags {
		for _, n := range f.Names() {
			names[n] = true
		}
	}
	for _, f := range logging.LoggingFlags {
		require.True(t, names[f.Names()[0]], "logging flag %q", f.Names()[0])
	}

	group := a.Command("imzero2")
	require.NotNil(t, group)
	require.NotNil(t, group.Before)
	for _, sub := range []string{"demo", "scene", "drive"} {
		require.NotNil(t, group.Command(sub), "imzero2 %s", sub)
	}
	require.NotNil(t, a.Command("watchbill"))
}

// An After the binary set still runs, before the host's exit hook.
func TestNewKeepsTheBinarysAfter(t *testing.T) {
	ran := false
	a := &cli.Command{Name: "acme", After: func(context.Context, *cli.Command) error {
		ran = true
		return nil
	}}
	New(Options{}, a)
	require.NoError(t, a.After(context.Background(), a))
	require.True(t, ran)
}
