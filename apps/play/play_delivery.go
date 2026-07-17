package play

import "github.com/stergiotis/boxer/public/observability/eh"

// play_delivery.go is the editor-delivery seam (ADR-0097 slice-6 D5 Update,
// 2026-07-17): the exported ops a tab body uses to push SQL into the editor and
// focus a dock tab. Before this, the built-in Snippets tab reached the editor
// through private pending state, so a snippet-class pane (a saved-query library,
// a query generator, a rewrite affordance) could not be built by an embedder —
// the tab registry failed its own dogfood test. These ops are that capability,
// made public; renderSnippetsTab is their in-tree consumer.
//
// All three are render-thread state, exactly like the tab registry: call them
// from a tab body during Render, not from another goroutine. The SQL ops are
// applied at the next editor render (one-frame latency); a same-frame ReplaceSql
// supersedes an InsertSqlAtCaret (the editor's existing consume-order, D5).

// InsertSqlAtCaret hands text to the SQL editor to splice at its caret on the
// next editor render (TextEditFluid.InsertAtCursor, ADR-0063) and focuses the
// Editor tab. The activation is a correctness step, not cosmetic: a hidden tab's
// body buffer is discarded uninterpreted, so an insert into an unfocused editor
// is silently lost — bundling it here keeps the visibility invariant the API's
// to hold, not each caller's to remember.
func (inst *PlayApp) InsertSqlAtCaret(text string) {
	inst.pendingSnippetInsert = text
	inst.pendingDockActivate = dockTabEditor
}

// ReplaceSql swaps the whole SQL editor buffer with text on the next editor
// render and focuses the Editor tab. A same-frame ReplaceSql supersedes an
// InsertSqlAtCaret. See InsertSqlAtCaret for the activation rationale.
func (inst *PlayApp) ReplaceSql(text string) {
	inst.pendingSnippetReplace = text
	inst.pendingDockActivate = dockTabEditor
}

// ActivateTab focuses the dock tab with the given registry slug ("editor",
// "table", … — the ids from Tabs().Specs()) on the next dock send. An unknown
// slug is an error, matching the registry's validation style. The SQL delivery
// ops focus the editor for you; reach for this to raise a different pane.
func (inst *PlayApp) ActivateTab(id string) (err error) {
	dockID, ok := inst.tabs.dockIDForSlug(id)
	if !ok {
		err = eh.Errorf("play: ActivateTab: unknown tab %q", id)
		return
	}
	inst.pendingDockActivate = dockID
	return
}
