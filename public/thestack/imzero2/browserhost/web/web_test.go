package web

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The three files a bundle no longer needs to carry are all there, under the
// names the page and the worker load them by.
func TestAssetsCarryPageWorkerAndShim(t *testing.T) {
	a := Assets()
	require.Len(t, a, 3)
	require.True(t, strings.Contains(string(a["index.html"]), "worker"), "the viewer page loads the worker")
	require.True(t, strings.Contains(string(a["worker.mjs"]), "bridge.mjs"), "the worker imports the shim")
	require.NotEmpty(t, a["bridge.mjs"])
}
