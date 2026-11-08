package chatview

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Height-cached virtualization of the transcript (ADR-0363 M5), the
// technique of the zapfast messaging client's conversation view (estimated
// row heights, a cache of measured ones, and an offset that follows rows
// above the viewport that change height) reimplemented for a host that runs
// one egui pass per frame. The transcript is a list of units — a message or system row together with the
// day separator that opens it, when one does. A unit near the viewport is
// laid out and measured; every other unit stands in as space of its last
// measured height, or of an estimate until it has been measured. Every
// readback here is last frame's: the host runs one egui pass per frame, so a
// unit's height is known the frame after it is first laid out, and a scroll
// correction lands the frame after it is asked for.

const (
	// marginAbove and marginBelow are how many viewports beyond each edge of
	// the viewport units are laid out, minMargin the floor of either. The
	// viewport is last frame's, so a scroll of up to a margin per frame
	// still shows units that are laid out. Above, the margin is also how
	// far ahead of the reader a unit is first measured: a unit first
	// measured inside the viewport, after a scroll of more than the margin
	// in one frame, moves what the reader sees without a correction, so
	// that side is wider. Below, a unit's height moves nothing the reader
	// sees.
	marginAbove float32 = 3
	marginBelow float32 = 1
	minMargin   float32 = 240
	// fallbackViewH stands in for the viewport's height before a readback
	// has given it.
	fallbackViewH float32 = 600
	// minCorrection is the smallest change above the viewport worth
	// correcting the scroll for.
	minCorrection float32 = 0.5
	// seekFrames bounds how many shown frames a jump keeps asking for its
	// target (see State.seek).
	seekFrames = 90
	// estimateLineH, estimateSmallH and estimateCharW model body text, the
	// small chrome lines and an average glyph's advance at the default body
	// size, for units not measured yet.
	estimateLineH  float32 = 18
	estimateSmallH float32 = 15
	estimateCharW  float32 = 7
	estimateQuoteH float32 = 48
	estimateReactH float32 = 24
)

// unit is one measured, cached and stood-in element of the transcript:
// rows[from:to] of the frame's plan; ord is the message ordinal of its last
// row and key that message's key (Model.key), which names it in the height
// cache and the accessibility tree.
type unit struct {
	from, to int
	ord      int32
	key      uint64
}

// unitsOf groups the plan's rows into units: a day separator joins the row
// after it, which is always a message or a system line.
func unitsOf(m *Model, rows []row, dst []unit) []unit {
	dst = dst[:0]
	start := 0
	var kc keyCounter
	at := 0 // the next ordinal kc has not counted
	for i, r := range rows {
		if r.kind == rowDay {
			continue
		}
		ord := int(r.msg)
		var key uint64
		if m.Key != nil {
			key = m.Key[ord]
		} else {
			for ; at < ord; at++ {
				kc.next(m, at)
			}
			key = kc.next(m, ord)
			at = ord + 1
		}
		dst = append(dst, unit{from: start, to: i + 1, ord: r.msg, key: key})
		start = i + 1
	}
	return dst
}

// rowHeight is a unit's height as the cache holds it: measured, or the
// estimate it stood in with before it was measured. The estimate is kept so
// a unit far from the viewport stands in with the same height every frame.
type rowHeight struct {
	h        float32
	measured bool
}

// laidOut is a unit laid out this frame: the height it was assumed to have
// when the frame was planned, and the probes either side of it.
type laidOut struct {
	key      uint64
	ord      int32
	assumed  float32
	top, bot uint64
}

// viewport is the scroll area as last read back, in content coordinates:
// offset is how far the content is scrolled, h the viewport's height, base
// where the first unit starts below the content's top, and top the
// viewport's top in screen coordinates.
type viewport struct {
	ok     bool
	top    float32
	offset float32
	h      float32
	base   float32
}

// virt is the virtualization's part of State. Its zero value has measured
// nothing.
type virt struct {
	heights map[uint64]rowHeight
	drawn   []laidOut
	view    viewport
	// correctedBy is the scroll a correction asked for last frame; the
	// readback does not hold it yet, so the frame's offset is assumed to be
	// the readback's plus it.
	correctedBy float32
	corrected   bool
	// seekProbe is where last frame asked to scroll to the jump's target.
	seekProbe uint64
	// scrolled is set when last frame scrolled to a cursor in its own
	// layout — the tail, or a jump's target — so the scroll it asked for
	// already holds what that layout changed above the viewport.
	scrolled bool
	// Per-frame scratch.
	units []unit
	hs    []float32
	mask  []bool
}

// rectReader is StateManager.GetUiRect's shape: last frame's rect of a probe.
type rectReader func(seq uint64) (c.UiRectValue, bool)

// readBack takes last frame's probes: the viewport, and the height of every
// unit laid out last frame, which replaces what the cache held for it. grew
// is how much the units that lay entirely above the viewport — by the height
// they were assumed to have — differed from that assumption, the amount the
// content the reader was looking at moved down; it is 0 when last frame
// scrolled to a cursor of its own (see scrolled). seek is where the jump's
// target (message ordinal seekOrd) sat in the viewport, when it was laid
// out, and how much the units laid out above it differed from what they
// were assumed to be. ok is false when the transcript did not reach the
// screen last frame; nothing is changed then.
func (inst *virt) readBack(read rectReader, viewSeq, contentSeq, unitsSeq uint64, seekOrd int32) (grew float32, seek seekReading, ok bool) {
	vr, okV := read(viewSeq)
	cr, okC := read(contentSeq)
	if !okV || !okC {
		return
	}
	ok = true
	v := viewport{ok: true, top: vr.MinY, offset: vr.MinY - cr.MinY, h: cr.MaxY - cr.MinY, base: inst.view.base}
	if ur, okU := read(unitsSeq); okU {
		v.base = ur.MinY - cr.MinY
	}
	if inst.heights == nil {
		inst.heights = map[uint64]rowHeight{}
	}
	aboveSeek := seekOrd >= 0
	for _, d := range inst.drawn {
		if d.ord == seekOrd {
			aboveSeek = false
		}
		t, okT := read(d.top)
		b, okB := read(d.bot)
		if !okT || !okB || b.MinY < t.MinY {
			continue
		}
		h := b.MinY - t.MinY
		inst.heights[d.key] = rowHeight{h: h, measured: true}
		if t.MinY+d.assumed <= vr.MinY {
			grew += h - d.assumed
		}
		if aboveSeek {
			seek.moved += h - d.assumed
		}
	}
	if inst.scrolled {
		grew = 0
	}
	if inst.seekProbe != 0 {
		if s, okS := read(inst.seekProbe); okS {
			seek.pos, seek.seen = s.MinY-vr.MinY, true
		}
	}
	inst.view = v
	return
}

// seekReading is where last frame left a jump's target.
type seekReading struct {
	pos   float32
	moved float32
	seen  bool
}

// height is unit u's height for this frame: the cache's, else the estimate,
// which the cache then keeps.
func (inst *virt) height(u unit, estimate func(unit) float32) float32 {
	if inst.heights == nil {
		inst.heights = map[uint64]rowHeight{}
	}
	if h, ok := inst.heights[u.key]; ok {
		return h.h
	}
	e := estimate(u)
	inst.heights[u.key] = rowHeight{h: e}
	return e
}

// prune drops cached heights of messages no longer in the window once the
// cache holds well more than the window.
func (inst *virt) prune() {
	if len(inst.heights) <= 2*len(inst.units)+64 {
		return
	}
	keep := make(map[uint64]struct{}, len(inst.units))
	for _, u := range inst.units {
		keep[u.key] = struct{}{}
	}
	for k := range inst.heights {
		if _, ok := keep[k]; !ok {
			delete(inst.heights, k)
		}
	}
}

// markNear sets mask[i] for every unit within the margins of the viewport
// [top, top+viewH) — top in unit coordinates, 0 being the first unit's top —
// when the viewport is known or the view is not following, and, when
// following, for the units that fill a viewport and the margin above the
// tail. A view that has never been read back and does not follow is at the
// top, where egui starts a scroll area.
func markNear(hs []float32, mask []bool, top, viewH float32, known, follow bool) {
	above := max(marginAbove*viewH, minMargin)
	below := max(marginBelow*viewH, minMargin)
	if known || !follow {
		lo, hi := top-above, top+viewH+below
		var y float32
		for i, h := range hs {
			if y+h >= lo && y <= hi {
				mask[i] = true
			}
			y += h
		}
	}
	if follow {
		var acc float32
		for i := len(hs) - 1; i >= 0 && acc < viewH+above; i-- {
			mask[i] = true
			acc += hs[i]
		}
	}
}

// spaceBefore is the stand-in space above the first unit that is laid out.
func spaceBefore(hs []float32, mask []bool) (s float32) {
	for i, h := range hs {
		if mask[i] {
			return
		}
		s += h
	}
	return
}

// correction is what a frame does about units above the viewport having
// changed height by grew: how far to scroll (asked for this frame, relative
// to wherever the offset then is, in effect from the next frame), and how
// much of the change to take out of the space above the first laid-out unit
// this frame, so the frame between the readback and the scroll already
// shows the reader's content where it was. offset is the frame's assumed
// offset; the scroll range bounds both.
func correction(grew, offset, viewH, contentH, spaceAbove float32) (delta, absorb float32, ok bool) {
	if math.Abs(float64(grew)) < float64(minCorrection) {
		return
	}
	delta = max(0, min(offset+grew, contentH-viewH)) - offset
	absorb = delta
	if grew >= 0 {
		absorb = max(0, min(absorb, grew, spaceAbove))
	} else {
		absorb = min(0, max(absorb, grew))
	}
	return delta, absorb, delta != 0
}

// estimate is the height assumed for unit u before it has been measured: the
// body's characters wrapped at textW, plus the chrome its rows carry. A
// rough figure is enough — the unit is measured a few viewports before it
// shows, and a change above the viewport is corrected for — so it only
// shapes the scrollbar and how far a jump has to travel.
func estimate(m *Model, rows []row, u unit, layout LayoutE, viewer int32, textW float32, dens styletokens.DensityE) (h float32) {
	gapItems, gapInline := styletokens.GapItems(dens), styletokens.GapInline(dens)
	pad := styletokens.PaddingDefault(dens)
	for _, r := range rows[u.from:u.to] {
		switch r.kind {
		case rowDay:
			h += gapItems + estimateSmallH + gapInline + gapItems
		case rowSystem:
			h += gapInline + estimateSmallH + gapItems
		case rowMessage:
			i := int(r.msg)
			if r.first {
				h += gapItems
			} else {
				h += gapInline
			}
			body := estimateLineH
			if m.Flags[i]&FlagDeleted == 0 {
				body = float32(wrappedLines(m.Body[i], textW-2*pad)) * estimateLineH
			}
			// The frame: padding, the body, and the footer line (in a
			// thread, the header line with the name and the time).
			h += 2*pad + body + gapItems + estimateSmallH
			if layout == LayoutGroup && r.first && m.Sender[i] != viewer {
				h += estimateSmallH + gapItems
			}
			if m.ReplyTo[i] >= 0 {
				h += estimateQuoteH + gapItems
			}
			if keys, _, _ := m.Reactions(i); len(keys) > 0 {
				h += estimateReactH + gapItems
			}
			h += gapItems
		}
	}
	return
}

// wrappedLines is how many lines s takes wrapped at width w, at the average
// glyph advance.
func wrappedLines(s string, w float32) (n int) {
	perLine := max(1, int(w/estimateCharW))
	runes := 0
	for _, r := range s {
		if r == '\n' {
			n += max(1, (runes+perLine-1)/perLine)
			runes = 0
			continue
		}
		runes++
	}
	n += max(1, (runes+perLine-1)/perLine)
	return
}

// unitProbes derives the probe seqs either side of a unit, under the same
// scope the unit's message row opens. The scope is opened for the
// derivation alone; it emits nothing.
func unitProbes(ids *c.WidgetIdStack, ord int32) (top, bot uint64) {
	for range c.IdScope(ids.PrepareSeq(uint64(ord))) {
		top = ids.ProbeSeq("unit-top")
		bot = ids.ProbeSeq("unit-bottom")
	}
	return
}

// settleSeek follows a jump to its target: prev is the distance left at the
// readback before (0 before the first), and the result the distance left
// now, or -1 once the jump is over — the target sits where the jump puts
// it, or as close as the scroll range lets it come, or the reader scrolled
// away from it. A distance that changed because units above the target were
// measured on the way is neither.
func settleSeek(prev float32, sk seekReading, align uint8, gap, viewH float32) (left float32) {
	want := gap
	if align == scrollAlignCenter {
		want = viewH / 2
	}
	left = max(float32(math.Abs(float64(sk.pos-want))), math.SmallestNonzeroFloat32)
	if prev == 0 || math.Abs(float64(sk.moved)) >= float64(minCorrection) {
		return
	}
	if left < 1 || math.Abs(float64(left-prev)) < 0.25 || left > prev+1 {
		return -1
	}
	return
}

// beginFrame takes up a jump requested since the last frame and returns the
// first message ordinal of the window, widened to hold the jump's target. A
// jump is taken up at the start of a frame — a quote strip requests one
// mid-loop, after the quoted (earlier) row has been drawn — and then stays
// under way (seek) until its target has arrived.
func (inst *State) beginFrame(n int) (first int) {
	if inst.jump > 0 {
		inst.seek, inst.seekAlign = inst.jump, scrollAlignCenter
		if inst.jumpTop {
			inst.seekAlign = scrollAlignTop
		}
		inst.seekLeft, inst.seekErr = seekFrames, 0
	}
	inst.jump, inst.jumpTop = 0, false
	first = max(0, n-inst.Window())
	if inst.seek > 0 {
		if j := int(inst.seek - 1); j >= n {
			inst.seek = 0
		} else if j < first {
			inst.window = n - j
			first = j
		}
	}
	return
}

// framePlan is a frame's virtualization decision; the units laid out are
// virt.mask's.
type framePlan struct {
	// seekUnit is the jump's target unit, laid out wherever it is; -1 none.
	seekUnit int
	// correct asks for a scroll by delta, for units above the viewport that
	// changed height; absorb is taken out of the space above the first
	// laid-out unit for this frame.
	correct       bool
	delta, absorb float32
	// current is the unit the reader is at — the newest message while
	// following, else the one at the viewport's top; -1 none.
	current int
}

// planFrame reads last frame's probes back and plans the frame over rows,
// the plan of the window of m: it settles a jump under way,
// fills the height cache, marks the units to lay out and decides the
// scroll correction. gap is the content's item spacing.
func (inst *State) planFrame(read rectReader, viewSeq, contentSeq, unitsSeq uint64, m *Model, rows []row, gap float32, est func(unit) float32) (fp framePlan) {
	vt := &inst.virt
	grew, sk, readOk := vt.readBack(read, viewSeq, contentSeq, unitsSeq, inst.seek-1)
	follow := inst.Follow()
	if follow {
		inst.seek = 0
	}
	if inst.seek > 0 && readOk {
		inst.seekLeft--
		if sk.seen {
			inst.seekErr = settleSeek(inst.seekErr, sk, inst.seekAlign, gap, vt.view.h)
			if inst.seekErr < 0 {
				inst.seek = 0
			}
		}
		if inst.seekLeft <= 0 {
			inst.seek = 0
		}
	}

	vt.units = unitsOf(m, rows, vt.units)
	vt.prune()
	vt.hs = vt.hs[:0]
	vt.mask = vt.mask[:0]
	fp.seekUnit = -1
	var unitsH float32
	for i, u := range vt.units {
		h := vt.height(u, est)
		vt.hs = append(vt.hs, h)
		vt.mask = append(vt.mask, false)
		unitsH += h
		if inst.seek > 0 && u.ord == inst.seek-1 {
			fp.seekUnit = i
		}
	}
	offset, viewH := float32(0), fallbackViewH
	if vt.view.ok {
		offset, viewH = vt.view.offset, vt.view.h
	}
	if vt.corrected {
		offset += vt.correctedBy
	}
	markNear(vt.hs, vt.mask, offset-vt.view.base, viewH, vt.view.ok, follow)
	if fp.seekUnit >= 0 {
		vt.mask[fp.seekUnit] = true
	}
	// A unit above the viewport that changed height last frame moved what
	// the reader was looking at: scroll by as much, and take it out of the
	// space above until the scroll lands. Following and a jump under way
	// set the scroll themselves.
	if readOk && !follow && inst.seek == 0 {
		fp.delta, fp.absorb, fp.correct = correction(grew, offset, viewH, vt.view.base+unitsH, spaceBefore(vt.hs, vt.mask))
	}
	vt.corrected, vt.correctedBy = fp.correct, fp.delta
	fp.current = currentUnit(m, vt.units, vt.hs, vt.mask, offset-vt.view.base+fp.absorb, follow)
	vt.scrolled = follow || fp.seekUnit >= 0
	vt.drawn = vt.drawn[:0]
	vt.seekProbe = 0
	return
}

// walk emits the frame's units in order: lay(i) for a unit laid out, and
// space(h) once for each run of units standing in, less absorb before the
// first one laid out.
func (inst *virt) walk(absorb float32, space func(float32), lay func(i int)) {
	s := -absorb
	for i, h := range inst.hs {
		if !inst.mask[i] {
			s += h
			continue
		}
		if s != 0 {
			space(s)
			s = 0
		}
		lay(i)
	}
	if s > 0 {
		space(s)
	}
}

// currentUnit is the unit the reader is at, among those laid out: while
// following, the newest that is a message rather than a marker; else the
// first whose bottom is below top, the viewport's top in unit coordinates.
// -1 when nothing is laid out.
func currentUnit(m *Model, units []unit, hs []float32, mask []bool, top float32, follow bool) int {
	if follow {
		last := -1
		for i := len(units) - 1; i >= 0; i-- {
			if !mask[i] {
				continue
			}
			if last < 0 {
				last = i
			}
			if m.Flags[units[i].ord]&FlagMarker == 0 {
				return i
			}
		}
		return last
	}
	var y float32
	last := -1
	for i, h := range hs {
		y += h
		if !mask[i] {
			continue
		}
		last = i
		if y > top {
			return i
		}
	}
	return last
}
