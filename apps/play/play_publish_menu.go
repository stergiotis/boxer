package play

// The person's own publish (ADR-0288 §SD4): a Publish menu in
// the top bar with the form publish_result takes — the bundle's name and
// the panes it opens on. It runs as a gesture through play's catalog, the
// handler an agent's call runs, so the person's publish is logged and
// audited as theirs.

import (
	"errors"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// publishMenu is the Publish menu's state. The launcher sets last, which
// also offers the menu: an embedded play serves no publish_result.
type publishMenu struct {
	last    func() (last LastPublish)
	bundle  string
	panes   string
	refusal string
}

// renderPublishMenu draws the Publish menu when the window serves
// publish_result and a gesture can reach it.
func (inst *PlayApp) renderPublishMenu() {
	if inst.publish.last == nil || inst.gestureCtx == nil {
		return
	}
	ids := inst.ids
	c.Separator().Vertical().Send()
	for range c.MenuButton(c.Atoms().Text("Publish").Keep()).KeepIter() {
		c.Label("Publish the main result as a bundle another window opens by name").Send()
		c.TextEdit(ids.PrepareStr("publishBundle"), inst.publish.bundle, false).
			DesiredWidth(220).HintText("bundle name").
			SendRespVal(&inst.publish.bundle)
		c.TextEdit(ids.PrepareStr("publishPanes"), inst.publish.panes, false).
			DesiredWidth(220).HintText("panes, e.g. table, chart (table when empty)").
			SendRespVal(&inst.publish.panes)
		if c.Button(ids.PrepareStr("publishGo"), c.Atoms().Text("Publish bundle").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.personPublish()
		}
		if line := inst.publishStatusLine(); line != "" {
			for rt := range c.RichTextLabel(line) {
				rt.Small().Weak()
			}
		}
	}
}

// personPublish is the person's Publish: publish_result through the
// gesture path, its refusal kept for the menu to show.
func (inst *PlayApp) personPublish() {
	in := PublishResultArgs{Bundle: strings.TrimSpace(inst.publish.bundle), Tabs: splitPanes(inst.publish.panes)}
	_, err := appops.Gesture[PublishResultArgs, PublishResultOutcome](inst.gestureCtx, opPublishResult, in)
	inst.publish.refusal = ""
	if err != nil {
		inst.publish.refusal = err.Error()
		var refusal *app.OperationRefusal
		if errors.As(err, &refusal) {
			inst.publish.refusal = refusal.Reason
		}
	}
}

// publishStatusLine says why the last Publish was refused, or how the
// window's last publish stands.
func (inst *PlayApp) publishStatusLine() (line string) {
	if inst.publish.refusal != "" {
		return "Not published: " + inst.publish.refusal
	}
	last := inst.publish.last()
	switch {
	case last.Bundle == "":
		return ""
	case last.Pending:
		return "Publishing " + last.Bundle + "…"
	case last.Error != "":
		return "Publishing " + last.Bundle + " failed: " + last.Error
	}
	return "Published " + last.Bundle + " (revision " + strconv.FormatUint(last.Revision, 10) + ", " + strconv.FormatInt(last.Rows, 10) + " rows)"
}

// splitPanes reads a comma- or space-separated pane list.
func splitPanes(s string) (panes []string) {
	for p := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		panes = append(panes, p)
	}
	return
}
