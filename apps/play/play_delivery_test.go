package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Editor-delivery seam tests (ADR-0097 slice-6 D5 Update, 2026-07-17): the
// exported ops a tab body uses to push SQL into the editor and focus a dock
// tab, plus the consume-order the editor applies them in. tabsTestApp builds a
// real PlayApp (nil client), so these exercise the same registry an embedder's
// snippet-class pane reaches.

// Insert stages the caret splice AND focuses the editor — the activation is the
// invariant the op bundles (a hidden editor discards its body buffer, losing
// the insert).
func TestInsertSqlAtCaretStagesInsertAndFocusesEditor(t *testing.T) {
	app := tabsTestApp()
	app.InsertSqlAtCaret("SELECT 1")
	assert.Equal(t, "SELECT 1", app.pendingSnippetInsert)
	assert.Equal(t, "", app.pendingSnippetReplace)
	assert.Equal(t, dockTabEditor, app.pendingDockActivate)
}

// Replace stages the whole-buffer swap and likewise focuses the editor.
func TestReplaceSqlStagesReplaceAndFocusesEditor(t *testing.T) {
	app := tabsTestApp()
	app.ReplaceSql("SELECT 2")
	assert.Equal(t, "SELECT 2", app.pendingSnippetReplace)
	assert.Equal(t, "", app.pendingSnippetInsert)
	assert.Equal(t, dockTabEditor, app.pendingDockActivate)
}

// Insert alone: the returned text is handed to the editor; the buffer is
// untouched (the Rust side splices it at the caret next frame).
func TestConsumePendingSnippetInsertAlone(t *testing.T) {
	app := tabsTestApp()
	app.sql = "ORIGINAL"
	app.InsertSqlAtCaret("INSERTED")
	got := app.consumePendingSnippet()
	assert.Equal(t, "INSERTED", got)
	assert.Equal(t, "ORIGINAL", app.sql)
	assert.Equal(t, "", app.pendingSnippetInsert, "pending cleared so each click applies once")
}

// A same-frame Replace supersedes an Insert: the buffer swaps and the insert is
// dropped (the editor never sees it).
func TestConsumePendingSnippetReplaceSupersedesInsert(t *testing.T) {
	app := tabsTestApp()
	app.sql = "ORIGINAL"
	app.InsertSqlAtCaret("INSERTED")
	app.ReplaceSql("REPLACED")
	got := app.consumePendingSnippet()
	assert.Equal(t, "", got, "Replace supersedes the same-frame Insert")
	assert.Equal(t, "REPLACED", app.sql)
	assert.Equal(t, "", app.pendingSnippetInsert)
	assert.Equal(t, "", app.pendingSnippetReplace)
}

// ActivateTab resolves a registry slug to its frozen DockID; a later call
// overrides the earlier pending (last write wins within a frame).
func TestActivateTabBySlug(t *testing.T) {
	app := tabsTestApp()
	require.NoError(t, app.ActivateTab("table"))
	assert.Equal(t, dockTabTable, app.pendingDockActivate)

	require.NoError(t, app.ActivateTab("schema"))
	assert.Equal(t, dockTabSchema, app.pendingDockActivate)
}

// An unknown slug errors and stages nothing.
func TestActivateTabUnknownSlugErrors(t *testing.T) {
	app := tabsTestApp()
	err := app.ActivateTab("no-such-tab")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-tab")
	assert.Equal(t, uint64(0), app.pendingDockActivate)
}
