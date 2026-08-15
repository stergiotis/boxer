package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const definitionDocMD = `---
title: Recent runs
icon: clock
---

# Recent runs

What the applet shows.

` + "```sql" + `
SELECT 1
` + "```" + `
`

// TestSetDefinitionMarkdownGatesTheDrawer pins the affordance's gate: the
// "Definition" toggle and the drawer behind it exist exactly when an embedder
// handed the instance a document. An ordinary playground has none — its buffer
// stands behind no definition — and must not grow a dead button.
func TestSetDefinitionMarkdownGatesTheDrawer(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- x", nil)
	defer inst.Close()

	assert.Nil(t, inst.definition, "a plain playground has no definition")

	inst.SetDefinitionMarkdown([]byte(definitionDocMD))
	require.NotNil(t, inst.definition)
	require.NotNil(t, inst.definition.doc)
	assert.False(t, inst.definition.open, "the drawer opens on a click, not on wiring")

	// The parse is the drawer's whole input, so the frontmatter half of the
	// definition — the manifest the reader came to see — must survive it.
	fm := inst.definition.doc.Frontmatter()
	require.NotNil(t, fm)
	title, ok := fm.Get("title")
	assert.True(t, ok)
	assert.Equal(t, "Recent runs", title)
}

// TestSetDefinitionMarkdownEmptyIsOff covers the hand-built-def path: a def
// that carries no Source hands over nothing, and the affordance stays off
// rather than opening onto a blank drawer.
func TestSetDefinitionMarkdownEmptyIsOff(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- x", nil)
	defer inst.Close()

	inst.SetDefinitionMarkdown([]byte(definitionDocMD))
	require.NotNil(t, inst.definition)

	for _, src := range [][]byte{nil, {}, []byte("  \n\t\n")} {
		inst.SetDefinitionMarkdown(src)
		assert.Nil(t, inst.definition, "%q leaves the drawer off", string(src))
	}
}

// TestPreambleIsIndependentOfTheDefinition pins that the two document-derived
// surfaces do not imply each other: an applet may carry a preamble, a
// definition, both, or neither, and the strip is absent rather than empty
// when the author wrote no preamble.
func TestPreambleIsIndependentOfTheDefinition(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- x", nil)
	defer inst.Close()

	assert.Nil(t, inst.preamble, "a plain playground has no preamble")

	inst.SetDefinitionMarkdown([]byte(definitionDocMD))
	assert.Nil(t, inst.preamble, "a definition alone does not conjure a preamble")

	inst.SetPreambleMarkdown([]byte("Counts are **per package**."))
	require.NotNil(t, inst.preamble)
	require.NotNil(t, inst.definition, "and the preamble does not displace the drawer")

	for _, src := range [][]byte{nil, {}, []byte(" \n\t")} {
		inst.SetPreambleMarkdown(src)
		assert.Nil(t, inst.preamble, "%q renders no strip", string(src))
	}
}

// TestDatasetNoticeIsSetAndCleared pins the host's runtime strip: unlike the
// two document-derived surfaces it is expected to come and go while the
// instance is live, as the condition it reports appears and clears.
func TestDatasetNoticeIsSetAndCleared(t *testing.T) {
	inst := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "-- x", nil)
	defer inst.Close()

	assert.Nil(t, inst.datasetNotice, "an instance with no unmet precondition shows no strip")

	inst.SetDatasetNotice([]byte("Waiting for dataset `pprof_cpu`."))
	require.NotNil(t, inst.datasetNotice)
	assert.Nil(t, inst.preamble, "the notice does not stand in for a preamble")

	// Clearing is what the applet does once the alias binds — the strip goes
	// away rather than lingering as an empty band.
	for _, src := range [][]byte{nil, {}, []byte(" \n\t")} {
		inst.SetDatasetNotice(src)
		assert.Nil(t, inst.datasetNotice, "%q renders no strip", string(src))
	}
}
