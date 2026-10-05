package chat

// The Artefact panel's screenshots (ADR-0284 §SD5): the set with a
// thumbnail, size and source per name, the budget's use, and Remove and
// Purge — the person's way out of a full budget; and an image change
// waiting under Ask first, shown as the image it adds or removes.

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	// imageThumbBox bounds a thumbnail as drawn, in logical points.
	imageThumbBox uint32 = 160

	tipImageRemove = "Remove this name from the set, as a new revision. The bytes stay while an earlier revision names them, so a revert brings it back."
	tipImagePurge  = "Remove it and free its bytes for good: earlier revisions keep the name, marked purged, and a revert cannot bring the image back."
)

var (
	atomsImageRemove = c.Atoms().Text(icons.PhTrash + " Remove").Keep()
	atomsImagePurge  = c.Atoms().Text(icons.PhTrash + " Purge").Keep()
)

// renderImages is the Images tab.
func (inst *App) renderImages(art *artefact) {
	b := art.budget()
	weak("screenshots " + strconv.Itoa(b.Names) + " of " + strconv.Itoa(b.NamesLimit) + " · " +
		humanBytes(b.Bytes) + " of " + humanBytes(b.BytesLimit) + " sealed on disk")
	set := art.headImages()
	if len(set) == 0 {
		weak("None. With Apps, the model adds screenshots of its windows with artefact_capture.")
		return
	}
	for i, e := range set {
		for range c.IdScope(inst.ids.PrepareStr("image-" + e.name)) {
			if i > 0 {
				c.Separator().Horizontal().Send()
			}
			inst.renderImageThumb(art, e)
			c.Label(e.name + " · " + strconv.Itoa(e.w) + "×" + strconv.Itoa(e.h) + " · " + humanBytes(e.bytes)).Selectable(true).Send()
			weak(imageOrigin(e))
			for range c.HorizontalTop().KeepIter() {
				for range c.HoverText(tipImageRemove).KeepIter() {
					if c.Button(inst.ids.PrepareStr("remove"), atomsImageRemove).Small().SendResp().HasPrimaryClicked() {
						inst.removeImage(art, e.name, false)
					}
				}
				if !e.purged {
					for range c.HoverText(tipImagePurge).KeepIter() {
						if c.Button(inst.ids.PrepareStr("purge"), atomsImagePurge).Small().SendResp().HasPrimaryClicked() {
							inst.removeImage(art, e.name, true)
						}
					}
				}
			}
		}
	}
}

func (inst *App) removeImage(art *artefact, name string, purge bool) {
	if _, err := art.removeImage(name, purge); err != nil {
		inst.setNote(err.Error(), true)
	}
}

// renderImageProposal is an image change waiting for the person.
func (inst *App) renderImageProposal(p *artProposal, what string, head int) {
	verb := "adds"
	if p.removes {
		verb = "removes"
	}
	if p.image != nil {
		c.Label(what + " · " + verb + " " + p.image.name + " · " + strconv.Itoa(p.image.w) + "×" + strconv.Itoa(p.image.h)).Selectable(false).Send()
	}
	if p.base != head {
		weak("The artefact moved since this was computed; accepting it will be refused as stale.")
	}
	for range c.HorizontalTop().KeepIter() {
		for range c.HoverText(tipAccept).KeepIter() {
			if c.Button(inst.ids.PrepareStr("art-accept"), atomsAccept).SendResp().HasPrimaryClicked() {
				p.decide(true)
			}
		}
		for range c.HoverText(tipReject).KeepIter() {
			if c.Button(inst.ids.PrepareStr("art-reject"), atomsReject).SendResp().HasPrimaryClicked() {
				p.decide(false)
			}
		}
	}
	if p.image != nil {
		for range c.IdScope(inst.ids.PrepareStr("proposal-image")) {
			inst.renderImageThumb(inst.conv.art, *p.image)
			weak(imageOrigin(*p.image))
		}
	}
}

// renderImageThumb draws an entry's thumbnail, or says why there is none.
// Each thumbnail is its own widget id, and its pixels go over the wire only
// when the host does not hold them.
func (inst *App) renderImageThumb(art *artefact, e artImage) {
	if e.purged {
		weak("purged: the bytes are gone")
		return
	}
	t, ok := art.store.thumbnail(e.hash)
	if !ok || len(t.Pixels) == 0 {
		weak("no thumbnail")
		return
	}
	id := inst.ids.PrepareStr("thumb-" + e.hash).Derive()
	if inst.artView.thumbs == nil {
		inst.artView.thumbs = c.NewImageVersionTracker[uint64]()
	}
	px := inst.artView.thumbs.PixelsToSendFor(id, id, 1, t.Pixels)
	c.Image(c.MakeAbsoluteIdHighEntropy(id), t.WidthPx, t.HeightPx, 1, uint8(c.FitAspectMaxE), imageThumbBox, imageThumbBox,
		uint8(c.FilterLinearE), c.TintNoneRgba, px).Send()
}

// imageOrigin says where an entry came from.
func imageOrigin(e artImage) string {
	switch e.source {
	case imageSourceCopy:
		return "a copy of " + e.from
	case imageSourceCrop:
		return "cut from " + e.from + " at " + strconv.Itoa(e.rect[0]) + "," + strconv.Itoa(e.rect[1]) + " · " +
			strconv.Itoa(e.rect[2]) + "×" + strconv.Itoa(e.rect[3])
	}
	ws := make([]string, 0, len(e.windows))
	for _, w := range e.windows {
		ws = append(ws, strconv.FormatUint(w, 10))
	}
	s := "captured from window"
	if len(ws) > 1 {
		s += "s"
	}
	s += " " + strings.Join(ws, ", ")
	switch {
	case e.autoCrop:
		s += ", cut to the window bounds"
	case e.crop != nil:
		s += ", cropped"
	}
	return s
}

// humanBytes is a byte count as the panel shows it.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MiB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KiB"
	}
	return strconv.FormatInt(n, 10) + " B"
}
