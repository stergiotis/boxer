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

// SetTimelineBandsSql seeds the Timeline panel's bands editor (the
// panel-local SQL of ADR-0097 slice 5d). An embedder whose own definition
// carries bands SQL — e.g. a sqlapplet aux fence (ADR-0132 §SD1) — applies
// it between construction and mount; empty is a valid value (no bands).
func (inst *PlayApp) SetTimelineBandsSql(sql string) {
	inst.timelineBandsSql = sql
}

// SetLiveMain presets the `main` lane's Live toggle (ADR-0097 slice 5e, D2).
// The toggle stays user-reachable in the top bar; presetting it on lets an
// embedder open with signal-driven re-runs active (e.g. a sqlapplet whose
// buffer reads `{selection_id:UInt64}`, ADR-0132 §SD3).
func (inst *PlayApp) SetLiveMain(on bool) {
	inst.liveMain = on
}

// SetToolbarMinimal attenuates the top bar to the applet surface (ADR-0132
// §SD3): Load .sql, the endpoint switcher, and the prelude/conditions
// toggles disappear; Run/Cancel, the Live toggle, and the unfilled-inputs
// hint stay; a "Copy SQL" escape hatch appears when a bus is wired. Call it
// between construction and mount, like the tab registry.
func (inst *PlayApp) SetToolbarMinimal(on bool) {
	inst.toolbarMinimal = on
}

// BindTab points a panel tab at a split node by CTE name (ADR-0097 slice 6c).
// An unknown tab id is an error, matching ActivateTab's validation style; an
// unknown node name is deliberately NOT one — bindings key on CTE names, sit
// inert while a split lacks the name, and revive when it returns.
func (inst *PlayApp) BindTab(tabID string, cteName string) (err error) {
	if _, ok := inst.tabs.dockIDForSlug(tabID); !ok {
		err = eh.Errorf("play: BindTab: unknown tab %q", tabID)
		return
	}
	inst.bindTab(tabID, NodeID(cteName))
	return
}
