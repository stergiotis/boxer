package mdedit

// Links into this window and out of it (ADR-0347). In: a launch
// config says how the handed-over document is shown — a view mode, and a
// location to open at — and the location is applied once the document it
// refers to is the one in the buffer, which for a snapshot handover is
// after the read lands. Out: an `mdedit://` link in the preview or a
// rendered block opens a new window, as every surface opens one
// (mdeditlink.Open), and a document read from a snapshot offers its own
// link at the caret's section.
//
// A location resolves against the parsed document, so it waits in
// pendingLocation for the frame in which refreshDerived has parsed the
// buffer it names; renderBody applies it before the caret tracking of that
// frame. Resolving sets the caret's baseline section too, so the caret's
// arrival next frame does not read as a change and drag the preview from a
// block target back to its heading.

import (
	"path"
	"strconv"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/apps/mdedit/mdeditlink"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdline"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdoutline"
)

const tipCopyLink = "Copy a link to this document at the section the caret is in, in the current view mode: an mdedit:// link that opens a new mdedit window from any app that renders markdown, or from an agent. It names the snapshot, so it stays valid for as long as the snapshot is kept."

var atomsCopyLink = c.Atoms().Text(icons.PhLink + " Link").Keep()

// viewModeOf maps a link's mode onto the window's, and modeOfView back.
func viewModeOf(m mdeditlink.ModeE) viewModeE {
	switch m {
	case mdeditlink.ModeSource:
		return viewSource
	case mdeditlink.ModeRead:
		return viewRead
	case mdeditlink.ModeBlocks:
		return viewBlocks
	}
	return viewSplit
}

func modeOfView(v viewModeE) mdeditlink.ModeE {
	switch v {
	case viewSource:
		return mdeditlink.ModeSource
	case viewRead:
		return mdeditlink.ModeRead
	case viewBlocks:
		return mdeditlink.ModeBlocks
	}
	return mdeditlink.ModeSplit
}

// applyLaunchOptions takes the mode and the location a launch config asks
// for (ADR-0347 §SD3). An unknown mode opens in Split and says so; the
// location waits for the parse.
func (inst *App) applyLaunchOptions(l launchcfg.MdeditLaunch) {
	if m, ok := mdeditlink.ParseMode(l.Mode); ok {
		inst.viewMode = viewModeOf(m)
	} else {
		inst.status = "unknown view mode " + strconv.Quote(l.Mode) + "; opened in Split"
	}
	inst.queueLocation(l.Location)
}

// queueLocation asks the next frame to show a location once the document
// is parsed; empty means the top and queues nothing.
func (inst *App) queueLocation(fragment string) {
	if fragment == "" {
		return
	}
	inst.pendingLocation = fragment
	inst.pendingLocationOk = true
}

// applyPendingLocation resolves a queued location against the parsed
// document: a heading slug scrolls the preview to the heading, a block id or
// a line to the block holding it; the caret goes to the same place when a
// pane that shows a caret is rendering. The caret's reported position and
// its baseline section are set to the target at once, so neither this
// frame's tracking (which would otherwise read the stale cursor as a
// change) nor the editor's report next frame drags the preview from a
// block back to its heading. In Blocks mode the caret request also opens
// the block, which takes focus; the source pane is left unfocused. A
// location that names nothing opens at the top and says so on the status
// line. While a handover's snapshot read is pending the location waits,
// since it refers to the snapshot's bytes, not the text the launch
// carried. Call once per frame, after refreshDerived and before
// trackCaretSection.
func (inst *App) applyPendingLocation() {
	if !inst.pendingLocationOk || inst.doc == nil {
		return
	}
	if inst.handedSnap != nil || inst.files.loadHanded {
		return
	}
	frag := inst.pendingLocation
	inst.pendingLocation, inst.pendingLocationOk = "", false
	loc, err := mdeditlink.ParseLocation(frag)
	if err != nil {
		inst.status = "location " + strconv.Quote(frag) + " is not a heading, a ^block id or a L<n> line"
		return
	}
	off, ok := inst.resolveLocation(loc)
	if !ok {
		inst.status = "location " + strconv.Quote(frag) + " is not in this document; opened at the top"
		return
	}
	switch loc.Kind {
	case mdeditlink.LocationHeading:
		inst.pendingScroll = markdown.SlugHeading(loc.Slug)
	case mdeditlink.LocationBlock:
		inst.pendingScroll = "^" + loc.BlockId
	case mdeditlink.LocationLine:
		inst.pendingScrollOff, inst.pendingScrollOffOk = off, true
	}
	if inst.editorVisible() || inst.viewMode == viewBlocks {
		inst.caretSlug = mdoutline.SectionAt(inst.src, inst.doc.Headings(), off)
		ch := mdline.ByteToChar(inst.src, off)
		inst.cursor = c.PackCursorRange(ch, ch)
		inst.requestCaret(off, off, false)
	}
}

// resolveLocation is the byte offset a location names in the parsed
// document: the heading's line, the block's start, or the line's start. ok
// is false for a heading or block id the document does not have, or a line
// past its end.
func (inst *App) resolveLocation(loc mdeditlink.Location) (off int, ok bool) {
	switch loc.Kind {
	case mdeditlink.LocationHeading:
		slug := markdown.SlugHeading(loc.Slug)
		for _, h := range inst.doc.Headings() {
			if h.Slug == slug && h.ByteOffset >= 0 {
				return lineStart(inst.src, h.ByteOffset), true
			}
		}
	case mdeditlink.LocationBlock:
		for _, b := range inst.doc.Blocks() {
			if !b.Synthetic && b.Id == loc.BlockId {
				return b.Start, true
			}
		}
	case mdeditlink.LocationLine:
		return lineOffset(inst.src, loc.Line)
	case mdeditlink.LocationTop:
		return 0, true
	}
	return 0, false
}

// lineOffset is the byte offset at which 1-based line n starts; ok is false
// past the last line. A trailing newline does not open a line of its own.
func lineOffset(src string, n int) (off int, ok bool) {
	if n < 1 {
		return 0, false
	}
	for line := 1; line < n; line++ {
		i := indexByteFrom(src, off, '\n')
		if i < 0 || i+1 >= len(src) {
			return 0, false
		}
		off = i + 1
	}
	return off, true
}

func indexByteFrom(s string, from int, b byte) int {
	for i := from; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// followLink opens an mdedit:// link clicked in this window: a new window,
// as from every surface (ADR-0347 §SD5). The request runs off the render
// thread; the status line says what was asked for.
func (inst *App) followLink(link string) {
	if inst.bus == nil {
		inst.status = "no bus wired — cannot open a link"
		return
	}
	r, err := mdeditlink.Parse(link)
	if err != nil {
		inst.status = "the link does not parse: " + err.Error()
		return
	}
	mdeditlink.Follow(inst.bus, inst.logger, link)
	inst.status = "opening " + path.Base(r.Path) + " in a new window"
}

// linkHere is the link to this document at the caret's section, in the
// current mode; ok is false for a document not read from a snapshot, which
// has no identity a link could carry (ADR-0347 §SD6). A handed-over
// document offers it once the snapshot's text is in the buffer, not while
// the buffer holds the text the launch carried.
func (inst *App) linkHere() (link string, ok bool) {
	r := inst.snapRef
	if r == nil || r.path == "" || !inst.readFromSnapshot {
		return "", false
	}
	ref := mdeditlink.Ref{Mount: r.mount, Snap: r.snap, Path: r.path, Mode: modeOfView(inst.viewMode)}
	if inst.doc != nil {
		caret, _ := c.UnpackCursorRange(inst.cursor)
		ref.Fragment = mdoutline.SectionAt(inst.src, inst.doc.Headings(), mdline.CharToByte(inst.src, caret))
	}
	return mdeditlink.Format(ref), true
}

// renderCopyLink is the Link button on the summary row, beside the rclone
// command, for a document a link can name.
func (inst *App) renderCopyLink() {
	if _, ok := inst.linkHere(); !ok {
		return
	}
	clicked := false
	for range c.HoverText(tipCopyLink).KeepIter() {
		clicked = c.Button(inst.ids.PrepareStr("copylink"), atomsCopyLink).Small().SendResp().HasPrimaryClicked()
	}
	if clicked && !inst.exportInFlight() {
		if link, ok := inst.linkHere(); ok {
			inst.copyToClipboard(link, false)
		}
	}
}
