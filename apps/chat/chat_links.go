package chat

// mdedit:// links in what the chat renders (ADR-0347 §SD5): a
// message body or the artefact may carry one — written by the model, or
// pasted by the person — and a click opens a new mdedit window on the
// snapshot document it names, the way every surface opens one. The open is
// an action, so the line above the composer says what it did.

import (
	"context"
	"path"

	"github.com/stergiotis/boxer/apps/mdedit/mdeditlink"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// mdeditLinkClaims is the link claim a render site passes: the scheme when
// the window has a bus to open it over, nil otherwise, so a link the window
// could not follow stays a hyperlink rather than a dead click — the same
// rule play, tally and the help center apply.
func (inst *App) mdeditLinkClaims() func(url string) bool {
	if inst.bus == nil {
		return nil
	}
	return mdeditlink.Claimed
}

// followMdeditLinks opens every claimed link clicked in a rendered document
// this frame.
func (inst *App) followMdeditLinks(res markdown.Result) {
	for _, l := range res.Links {
		inst.openMdeditLink(l.URL)
	}
}

// openMdeditLink follows one link, off the render thread, reporting on the
// action line.
func (inst *App) openMdeditLink(link string) {
	r, err := mdeditlink.Parse(link)
	if err != nil {
		inst.setNote("the link does not parse: "+err.Error(), true)
		return
	}
	name := path.Base(r.Path)
	bus := inst.bus
	inst.runAction("chat-open-link", "open "+name+" in mdedit", func(ctx context.Context) (note string, err error) {
		if _, err = mdeditlink.Open(bus, link); err != nil {
			return
		}
		return "opened " + name + " in mdedit", nil
	}, nil)
}
