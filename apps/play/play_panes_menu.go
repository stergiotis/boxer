package play

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_panes_menu.go is the prose half of the ADR-0097 2026-07-27 Update. A
// dock title is a plain string with no tooltip and no styling, so a strip mark
// can only be a scent; this menu is where the same verdicts get to explain
// themselves. It answers "where can I look at this result, and which pane
// feeds the query" in two groups, from the same pure functions the marks use.
//
// It hides nothing and moves nothing: a rejecting row still activates its
// pane, so the pane's own contract help stays one click away instead of being
// locked behind a pane the menu just advised against. That is deliberate —
// the Update records the filter that would have hidden panes as descoped, on
// the grounds that the dock loses a withheld tab's position permanently.

// paneMenuRow is one pane's row. Title is the spec's static title: the marked
// title belongs to the strip, and repeating a `-` here beside prose that says
// the same thing at length would be noise.
type paneMenuRow struct {
	TabID  string
	Title  string
	DockID uint64
	// Node is the node feeding this pane when it is not the active one — a 6c
	// binding, or the Detail follow. Empty otherwise.
	Node NodeID
	// Reject is the panel's own reason its required channels cannot be filled,
	// or "" when they can (or when there is no honest text — see paneReject).
	Reject string
	// Drives are the names this pane writes that the split reads; Unfilled is
	// the subset nothing has filled yet.
	Drives   []string
	Unfilled []string
}

// paneMenuRows builds both groups for this frame. active is the current result
// schema; a pane bound to another node is judged against ITS schema, the same
// swap frameFor does for the body.
//
// Panes appear in registration order, which is the order of the dock strip —
// the menu is read next to the tabs, so a different sort would cost the reader
// the mapping between them.
func (inst *PlayApp) paneMenuRows(active *arrow.Schema) (shows, drives []paneMenuRow) {
	specs := inst.tabs.all()
	reads := bufferReads(inst.paramSlots)
	shows = make([]paneMenuRow, 0, len(specs))
	for i := range specs {
		spec := &specs[i]
		row := paneMenuRow{TabID: spec.ID, Title: spec.Title, DockID: spec.DockID}
		in := tabVerdict{
			schema: inst.paneSchemaFor(spec.ID, active),
			split:  inst.currentSplit,
			reads:  reads,
			sig:    inst.frameSig,
			bound:  inst.paramSyncedValues,
		}
		if node := inst.resolvedTabNode(spec.ID); node != inst.activeNodeID() {
			row.Node = node
		}
		row.Drives, row.Unfilled = signalRelation(spec, in)
		if spec.Panel != nil {
			// Unlike the strip, the menu reports an interaction-gated reason
			// too ("Select a row …"): the entropy argument that keeps those
			// marks off the tabs is about a glyph every tab pays width for,
			// not about a line the reader opened this menu to see.
			row.Reject, _ = paneReject(spec.Panel, in)
			shows = append(shows, row)
		}
		if len(row.Drives) > 0 {
			drives = append(drives, row)
		}
	}
	return
}

// paneSchemaFor is the schema a tab's frame-fed channels would be offered this
// frame: the bound node's lane view when the tab resolves off the active node
// (6c), else the active result's. The narrow half of frameFor — the menu needs
// the schema, not the record.
func (inst *PlayApp) paneSchemaFor(tabID string, active *arrow.Schema) (schema *arrow.Schema) {
	schema = active
	node, bound := inst.resolvedNodes[tabID]
	if !bound || node == inst.activeNodeID() {
		return
	}
	if v, has := inst.boundViews[node]; has {
		schema = v.schema
	}
	return
}

// renderPanesMenu draws the topbar's "Panes" menu. The label is fixed: a
// dynamic one shifts the MenuButton's derived id and drops the menu's open
// state (the reason the Endpoint switcher keeps its own label static).
func (inst *PlayApp) renderPanesMenu(active *arrow.Schema) {
	for range c.MenuButton(c.Atoms().Text("Panes").Keep()).KeepIter() {
		shows, drives := inst.paneMenuRows(active)
		menuWeak("shows this result")
		for i := range shows {
			row := &shows[i]
			inst.paneMenuButton(row, "paneMenuShow-")
			if row.Reject != "" {
				menuWeak(row.Reject)
			}
		}
		if len(drives) > 0 {
			c.Separator().Send()
			menuWeak("drives this query")
			for i := range drives {
				row := &drives[i]
				inst.paneMenuButton(row, "paneMenuDrive-")
				menuWeak("writes " + describeDrivenSignals(row.Drives, row.Unfilled))
			}
		}
	}
}

// paneMenuButton draws a row's activating button. Each group prints only its
// own payload under it — a pane in both groups (the World shows a result AND
// drives the query) would otherwise repeat one line twice on one screen.
//
// idPrefix namespaces the widget id for that same reason: two buttons sharing
// an id is an id-stack collision, not a duplicate label.
func (inst *PlayApp) paneMenuButton(row *paneMenuRow, idPrefix string) {
	ids := inst.ids
	label := row.Title
	if row.Node != "" {
		label += " · " + string(row.Node)
	}
	if c.Button(ids.PrepareStr(idPrefix+row.TabID), c.Atoms().Text(label).Keep()).
		SendResp().HasPrimaryClicked() {
		// Activation only. A rejecting pane opens too: its body carries the
		// contract help the reason line is a summary of.
		_ = inst.ActivateTab(row.TabID)
	}
}

// describeDrivenSignals lists the driven names, marking the ones nothing has
// filled — those are why a Run is refused, and this pane is where to fix it.
func describeDrivenSignals(drives, unfilled []string) string {
	if len(unfilled) == 0 {
		return strings.Join(drives, ", ")
	}
	needs := make(map[string]bool, len(unfilled))
	for _, n := range unfilled {
		needs[n] = true
	}
	parts := make([]string, 0, len(drives))
	for _, n := range drives {
		if needs[n] {
			n += " (needs a value)"
		}
		parts = append(parts, n)
	}
	return strings.Join(parts, ", ")
}

func menuWeak(text string) {
	for rt := range c.RichTextLabel(text) {
		rt.Small().Weak()
	}
}
