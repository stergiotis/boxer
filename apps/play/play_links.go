package play

// mdedit:// links in what play renders (ADR-0347 §SD5): a
// markdown detail cell, the docs panel, a snippet, the definition or the
// preamble may carry one, and a click opens a new mdedit window on the
// snapshot document it names, the way every surface opens one.

import (
	"github.com/stergiotis/boxer/apps/mdedit/mdeditlink"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// mdeditLinkClaims is the link claim a render site passes when the window
// can follow a link — with a bus; nil otherwise, so a link the window could
// not open stays a hyperlink rather than a dead click.
func (inst *PlayApp) mdeditLinkClaims() func(url string) bool {
	if inst.bus == nil {
		return nil
	}
	return mdeditlink.Claimed
}

// followMdeditLinks opens every claimed link clicked this frame.
func (inst *PlayApp) followMdeditLinks(res markdown.Result) {
	for _, l := range res.Links {
		inst.openMdeditLink(l.URL)
	}
}

// openMdeditLink follows one link, off the render thread; a failure is
// logged, as a failed clipboard copy is.
func (inst *PlayApp) openMdeditLink(link string) {
	if inst.bus == nil {
		return
	}
	mdeditlink.Follow(inst.bus, inst.logger, link)
}

// docsLinkClaimed is the docs panel's claim: the corpus's own links, which
// the pane follows in place, and mdedit:// links, which open a window.
func (inst *PlayApp) docsLinkClaimed(url string) bool {
	return mdeditlink.Claimed(url) || inst.docs.source.LinkClaimed(url)
}
