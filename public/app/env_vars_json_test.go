package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stergiotis/boxer/public/config/env/envdoc"
	"github.com/stretchr/testify/require"
)

// TestEnvVarsJSONMatchesRegistry pins doc/env-vars.json to the specs this
// binary links (ADR-0009 §4). It lives here rather than in envdoc because
// only this package's import graph registers the full set.
func TestEnvVarsJSONMatchesRegistry(t *testing.T) {
	want, err := envdoc.RenderJSON()
	require.NoError(t, err)
	path := filepath.Join("..", "..", "doc", "env-vars.json")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got),
		"doc/env-vars.json does not match the registry; run `go generate ./public/config/env/...`")
}
