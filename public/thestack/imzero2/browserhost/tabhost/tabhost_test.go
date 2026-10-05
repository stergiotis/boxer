package tabhost

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/thestack/imzero2/browserhost"
)

// New keeps the binary's own name and version and adds the tab's flags, its
// action and `serve`; an Usage the binary set is kept.
func TestNewDecoratesTheBinarysApp(t *testing.T) {
	a := &cli.App{Name: "acmetab", Version: "v0", Usage: "acme's tab"}
	p := New(Options{DefaultApp: "example.com/acme/apps/dashboard"}, a)
	require.Same(t, a, p.app)
	require.Equal(t, "acmetab", a.Name)
	require.Equal(t, "acme's tab", a.Usage)
	require.NotNil(t, a.Action)
	var appFlag *cli.StringFlag
	for _, f := range a.Flags {
		if sf, ok := f.(*cli.StringFlag); ok && sf.Name == "app" {
			appFlag = sf
		}
	}
	require.NotNil(t, appFlag)
	require.Equal(t, "example.com/acme/apps/dashboard", appFlag.Value)
	require.NotNil(t, a.Command("serve"))
}

// An app id no linked package registered is refused before anything starts,
// and a native run leaves no reactor step behind.
func TestUnknownAppIsRefused(t *testing.T) {
	a := &cli.App{Name: "acmetab"}
	p := New(Options{DefaultApp: "example.com/acme/apps/none"}, a)
	err := a.Run([]string{"acmetab"})
	require.True(t, errors.Is(err, browserhost.ErrNoSuchApp), "got %v", err)
	require.Nil(t, p.run([]string{}))
}
