package attentionchrome

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The person's table of levels as a grid (ADR-0323 §SD5): kinds as rows,
// levels as columns, no rule language. Drawn in host chrome, where no app or
// agent draws or clicks, since the waiting scale decides what reaches the
// person first; a change there goes on the trail as the person's. A row
// offers only the levels of its kind's scale (the floor, §SD2): the
// attention scale's three first, the waiting scale's two last.

const tipLevels = "How loud each kind of event is, for every conversation (ADR-0323). " +
	"A row offers the levels of its scale only: what waits on you cannot be set below your turn, and what does not wait cannot be raised into the queue. " +
	"A conversation's own settings, such as Watch in the chat, win over this table."

// renderLevels draws the grid's window while it is open.
func (inst *Chrome) renderLevels(ids *c.WidgetIdStack) {
	if !inst.levelsOpen {
		return
	}
	levels := inst.svc.Levels
	wf := c.Window(ids.PrepareStr("attention-levels"), c.WidgetText().Text("Levels").Keep()).
		AlwaysOnTop(true).
		Collapsible(false).
		Resizable(false).
		TitleBar(true).
		DefaultOpen(true).
		DefaultPos(listX, listY)
	for range wf.KeepIter() {
		for range c.HoverText(tipLevels).KeepIter() {
			for rt := range c.RichTextLabel("How loud each kind of event is, for every conversation.") {
				rt.Small().Weak()
			}
		}
		table := levels.Overrides("")
		for range c.Grid(ids.PrepareStr("attention-levels-grid")).NumColumns(uint32(2 + len(attention.AllLevels))).Striped(true).KeepIter() {
			for _, row := range attention.GridRows() {
				inst.renderLevelRow(row, table, ids)
				c.EndRow()
			}
		}
		if c.Button(ids.PrepareStr("attention-levels-close"), c.Atoms().Text("Close").Keep()).SendResp().HasPrimaryClicked() {
			inst.levelsOpen = false
		}
	}
}

// rowName is how the grid names a row's key.
func rowName(k attention.Key) string {
	if k.Effect != 0 {
		return k.Kind + " · " + k.Effect.String()
	}
	return k.Kind
}

func (inst *Chrome) renderLevelRow(row attention.GridRow, table map[attention.Key]attention.LevelE, ids *c.WidgetIdStack) {
	k := row.Key
	name := rowName(k)
	scale, _ := attention.ScaleOf(k.Kind)
	set, overridden := table[k]
	effective := row.Default
	if overridden {
		effective = set
	}
	if row.Note != "" {
		for range c.HoverText(row.Note).KeepIter() {
			c.Label(name + " *").Send()
		}
	} else {
		c.Label(name).Send()
	}
	for _, l := range attention.AllLevels {
		if l.Scale() != scale {
			c.Label("").Send()
			continue
		}
		// The level's name in the cell, not only a column's head: a cell is
		// read, and anchored, on its own.
		mark := "○ " + l.String()
		if l == effective {
			mark = "● " + l.String()
		}
		tip := name + ": " + l.String()
		if l == row.Default {
			tip += " (the default)"
		}
		for range c.HoverText(tip).KeepIter() {
			if c.SelectableLabel(ids.PrepareStr("attention-level-"+name+"-"+l.String()), l == effective, mark).SendResp().HasPrimaryClicked() && l != effective {
				if l == row.Default {
					_ = inst.svc.Levels.Clear(attention.AuthorPerson, "", "", k)
				} else {
					_ = inst.svc.Levels.Set(attention.AuthorPerson, "", "", k, l)
				}
			}
		}
	}
	if overridden {
		if c.Button(ids.PrepareStr("attention-level-reset-"+name), c.Atoms().Text("Reset").Keep()).Small().SendResp().HasPrimaryClicked() {
			_ = inst.svc.Levels.Clear(attention.AuthorPerson, "", "", k)
		}
	} else {
		for rt := range c.RichTextLabel("default") {
			rt.Small().Weak()
		}
	}
}
