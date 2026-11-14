package mdedit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The hand-over to the notebook window is offered for a buffer that holds
// a record, and not for an applet document, which spells its panels the way
// a notebook spells presentation cells.
func TestTheNotebookHandOverIsOfferedForANotebook(t *testing.T) {
	inst := newApp()
	inst.src = "# Notes\n\n```sql\nSELECT 1 AS one\n```\n"
	assert.False(t, inst.bufferIsNotebook(), "a sql fence without an id is prose")
	inst.src = "# Report\n\n```sql table\nSELECT * FROM one\n```\n"
	assert.False(t, inst.bufferIsNotebook(), "a panel fence alone is an applet document's")
	inst.src = "# Report\n\n```sql\nSELECT 1 AS one\n```\n^one\n\n```sql table\nSELECT * FROM one\n```\n"
	assert.True(t, inst.bufferIsNotebook())
	inst.src = "edited"
	assert.False(t, inst.bufferIsNotebook(), "decided again when the text changes")
}
