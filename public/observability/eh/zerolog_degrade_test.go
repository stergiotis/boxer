package eh

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stack whose facts disagree on a frame is dropped, not fatal: the
// error's messages are logged without it, under a marker.
func TestInconsistentStackIsDroppedWithAMarker(t *testing.T) {
	g := newGatherFactsAndStacks()
	g.stacks = append(g.stacks, StackTrace{{PC: 1, File: "a.go", Line: 1, Function: "pkg.f"}})
	g.stackRepresentations = append(g.stackRepresentations, nil)
	g.perStackFacts = append(g.perStackFacts, [][]*errorFact{{
		{Msg: "outer", framePC: 1, Id: 0},
		{Msg: "inner", framePC: 2, Id: 1},
	}})
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Log().Object("error", g).Send()
	out := buf.String()
	assert.Contains(t, out, `"`+StackDroppedFieldName+`":true`)
	assert.Contains(t, out, "outer")
	assert.Contains(t, out, "inner")
	assert.NotContains(t, out, "stack-0")
	assert.NotContains(t, out, "a.go")
}

func TestConsistentStackHasNoMarker(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	logger.Log().Object("error", MarshalError(Errorf("x: %w", New("y"))).(*gatherFactsAndStacks)).Send()
	assert.NotContains(t, buf.String(), StackDroppedFieldName)
	assert.Contains(t, buf.String(), "stack-0")
}

func TestGoRootOfThisBinaryNeedsNoTool(t *testing.T) {
	root := goRootOfThisBinary()
	if root == "" {
		t.Skip("built with -trimpath: no GOROOT in the binary's frames")
	}
	require.True(t, strings.HasPrefix(root, "/"), root)
}
