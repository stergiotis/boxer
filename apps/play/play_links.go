package play

// Links in what play renders (ADR-0409, which takes over
// ADR-0347 §SD5's per-surface claim): a markdown detail cell, the docs
// panel, a snippet, the definition or the preamble may carry a boxer:
// link or an alias such as mdedit://, and a click asks the host to follow
// it, naming who wrote the text it was in. The host decides what the click
// may do.

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// The authors of play's texts (ADR-0311 §SD1): the docs and the snippets
// ship with the binary; a cell, a definition, a dataset's notice and a
// preamble hold whatever the data or a bundle's author put there.
var (
	shippedAuthor = applink.Author{Level: applink.LevelSystem}
	dataAuthor    = applink.UnknownAuthor
)

// linkClaims is the link claim a render site passes when the window can
// follow a link — with a bus; nil otherwise, so a link the window could
// not follow stays a hyperlink rather than a dead click.
func (inst *PlayApp) linkClaims() func(url string) bool {
	if inst.bus == nil {
		return nil
	}
	return applink.Claimed
}

// linkHover is the hover of a link in text author wrote: the host's
// reading of it, asked for off the render goroutine (ADR-0409 §SD8); nil
// without a bus.
func (inst *PlayApp) linkHover(author applink.Author) func(url string) string {
	if inst.bus == nil {
		return nil
	}
	if inst.linkHovers == nil {
		inst.linkHovers = applink.NewHoverSet(inst.bus, inst.client.Go)
	}
	return inst.linkHovers.For(author)
}

// followLinks follows every claimed link clicked this frame.
func (inst *PlayApp) followLinks(res markdown.Result, author applink.Author) {
	for _, l := range res.Links {
		inst.followLink(l.URL, author)
	}
}

// followLink follows one link, off the render thread; what came of it is
// logged, as a failed clipboard copy is.
func (inst *PlayApp) followLink(link string, author applink.Author) {
	if inst.bus == nil {
		return
	}
	applink.FollowLogged(inst.bus, inst.logger, link, author, inst.client.Go)
}

// followDataLink is a rich cell's link follow: the cell's text is the
// data's.
func (inst *PlayApp) followDataLink(link string) {
	inst.followLink(link, dataAuthor)
}

// docsLinkClaimed is the docs panel's claim: the corpus's own links, which
// the pane follows in place, and the links the host follows.
func (inst *PlayApp) docsLinkClaimed(url string) bool {
	return applink.Claimed(url) || inst.docs.source.LinkClaimed(url)
}

// docsLinkHover is the docs panel's hover: the host's reading of a link it
// follows; the corpus's own links, which the pane follows in place, show
// none.
func (inst *PlayApp) docsLinkHover() func(url string) string {
	h := inst.linkHover(shippedAuthor)
	if h == nil {
		return nil
	}
	return func(url string) string {
		if !applink.Claimed(url) {
			return ""
		}
		return h(url)
	}
}
