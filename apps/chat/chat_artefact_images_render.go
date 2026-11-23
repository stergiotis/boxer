package chat

// The Artefact panel's screenshots (ADR-0284 §SD5): the set with a
// thumbnail, size and source per name, the budget's use, and Remove and
// Purge — the person's way out of a full budget; and an image change
// waiting under Ask first, shown as the image it adds or removes; and a view
// of a screenshot waiting under Pixels' Ask levels (ADR-0287 §SD3); and the
// screenshots the document embeds, drawn where it embeds them (ADR-0284
// update of 2026-10-26).

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/decision"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
)

const (
	// imageThumbBox bounds a thumbnail as drawn, in logical points.
	imageThumbBox uint32 = 160

	tipImageBudget  = "The images are held for this conversation in sealed files on this machine, which the window removes when the conversation ends; the limits are the host's."
	tipImageRemove  = "Remove this name from the set, as a new revision. The bytes stay while an earlier revision names them, so a revert brings it back."
	tipImagePurge   = "Remove it and free its bytes for good: earlier revisions keep the name, marked purged, and a revert cannot bring the image back."
	tipPixelAllow   = "Send this screenshot to the model with the turn. It stays in the model's context for this turn only; what was sent cannot be called back."
	tipPixelDecline = "Keep this screenshot from the model; it is told you declined, and asked not to ask again."

	// pixelPreviewH bounds the height of the image a view's consent shows,
	// and of a screenshot the document embeds.
	pixelPreviewH uint32 = 480
	// imageInlineSide bounds the longer side, in pixels, of a screenshot
	// decoded for the document: about twice the box it is drawn in, so a
	// high-density display draws it sharp.
	imageInlineSide = 960
)

var (
	atomsImageRemove = c.Atoms().Text(icons.PhTrash + " Remove").Keep()
	atomsImagePurge  = c.Atoms().Text(icons.PhTrash + " Purge").Keep()
)

// renderImages is the Images tab.
func (inst *App) renderImages(art *artefact) {
	b := art.budget()
	for range c.HoverText(tipImageBudget).KeepIter() {
		weak(strconv.Itoa(b.Names) + " of " + strconv.Itoa(b.NamesLimit) + " images · " +
			humanBytes(b.Bytes) + " of " + humanBytes(b.BytesLimit) + " held for this conversation")
	}
	for range c.HoverText(tipImageAdd).KeepIter() {
		if c.Button(inst.ids.PrepareStr("add-image"), atomsImageAdd).Small().SendResp().HasPrimaryClicked() {
			inst.addImage(art)
		}
	}
	set := art.headImages()
	if len(set) == 0 {
		weak("None. Add an image file here; with Apps, the model can add screenshots of the windows you share.")
		return
	}
	// Any image of the set can go with the next message, where the Pixels
	// setting lets pixels reach the model (ADR-0385 §SD3, §SD4).
	notOffered := inst.sendOffer()
	if notOffered != "" {
		weak(notOffered)
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
				if !e.purged && notOffered == "" {
					chosen, atoms, tip := slices.Contains(inst.sendNames, e.name), atomsImageSend, tipImageSend
					if chosen {
						atoms, tip = atomsImageUnsend, tipImageUnsend
					}
					for range c.HoverText(tip).KeepIter() {
						if c.Button(inst.ids.PrepareStr("send"), atoms).Small().SendResp().HasPrimaryClicked() {
							inst.toggleSend(e.name)
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

// renderImageProposal is an image change waiting for the person, drawn as
// a decision the model asks for (ADR-0404 §SD3, §SD4); the image follows
// the options.
func (inst *App) renderImageProposal(p *artProposal, head int) {
	verb, title := "adds", "The model proposes to add an image to the artefact"
	if p.removes {
		verb, title = "removes", "The model proposes to remove an image from the artefact"
	}
	subject := ""
	if p.image != nil {
		subject = p.image.name + " · " + strconv.Itoa(p.image.w) + "×" + strconv.Itoa(p.image.h) + " px"
	}
	if p.title != "" {
		subject = strings.TrimPrefix(p.title+" · "+subject, " · ")
	}
	in := decision.Input{
		Ids: inst.ids, ScopeKey: "image-proposal", Origin: decision.OriginRequest,
		Title: title, Subject: subject,
		Follows: []string{
			"Accept " + verb + " it as revision " + strconv.Itoa(head+1) + "; the model goes on with its turn.",
			"Reject leaves the artefact's images as they are, and the model is told you turned it down.",
		},
		Options: proposalOptions(p.tool),
	}
	if p.base != head {
		in.Notes = []decision.Note{{Text: noteStaleProposal, Tone: styletokens.ToneWarning}}
	}
	if p.image != nil {
		img := *p.image
		in.Detail = func() {
			inst.renderImageThumb(inst.conv.art, img)
			c.Label(imageOrigin(img)).Wrap().Send()
		}
	}
	inst.decideProposal(p, decision.Render(in))
}

// renderPixelAsk is a view of a screenshot waiting for the person: what
// would be sent and where, as a decision the model asks for (ADR-0404
// §SD3, §SD4). The preview follows the options.
func (inst *App) renderPixelAsk(a *pixelAsk) {
	to := "the host's model"
	if a.endpoint != "" {
		to = a.endpoint
	}
	again := "Allowed, it is shown for this turn only; the next view asks again."
	if a.level == agent.PixelsAskOnce {
		again = "Allowed, it is not asked for again while its content is unchanged."
	}
	in := decision.Input{
		Ids: inst.ids, ScopeKey: "pixel-ask", Origin: decision.OriginRequest,
		Title:   "The model asks to see a screenshot",
		Subject: a.name + " · " + strconv.Itoa(a.w) + "×" + strconv.Itoa(a.h) + " px · " + a.origin,
		Follows: []string{
			"Allow sends every pixel of it to " + to + " with this turn. What was sent cannot be called back.",
			again,
			"Decline keeps it from the model, which is told you declined and asked not to ask again.",
		},
		Options: []decision.Option{
			{Key: "allow", Label: "Allow", Icon: icons.PhCheck, Role: decision.RoleAffirm, Tooltip: tipPixelAllow},
			{Key: "decline", Label: "Decline", Icon: icons.PhX, Role: decision.RoleDecline, Tooltip: tipPixelDecline},
		},
		Detail: func() { inst.renderPixelPreview(a) },
	}
	switch decision.Render(in).Chosen {
	case "allow":
		a.decide(pixelAllowed)
	case "decline":
		a.decide(pixelDeclined)
	}
}

// renderPixelPreview is the screenshot a view would send, fitted to the
// pane.
func (inst *App) renderPixelPreview(a *pixelAsk) {
	t := a.preview
	if len(t.Pixels) == 0 {
		c.Label("No preview of it could be made.").Wrap().Send()
		return
	}
	weak("The preview is fitted to the pane; the model would get every pixel.")
	id := inst.ids.PrepareStr("pixel-ask-" + a.hash).Derive()
	if inst.artView.thumbs == nil {
		inst.artView.thumbs = c.NewImageVersionTracker[uint64]()
	}
	px := inst.artView.thumbs.PixelsToSendFor(id, id, 1, t.Pixels)
	c.Image(c.MakeAbsoluteIdHighEntropy(id), t.WidthPx, t.HeightPx, 1, uint8(c.FitAspectMaxE), uint32(artefactPanelW)-40, pixelPreviewH,
		uint8(c.FilterLinearE), c.TintNoneRgba, px).Send()
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
	case imageSourceRestored:
		return "restored from the artefact's export when the conversation resumed"
	case imageSourceUpload:
		// The file's name is the person's to read, here and nowhere else.
		if e.fileName == "" {
			return "a file you added"
		}
		return "a file you added: " + e.fileName
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

// artImageResolver shows the document's embeds of its screenshots —
// `![[name]]`, or `![alt](name)` — as the images. Anything else is
// NoopResolver's, as is an embed of a name the set does not hold or whose
// bytes were purged: a link with the image glyph, not an image.
type artImageResolver struct {
	resolver.NoopResolver
	set   []artImage
	store *imageStore
	// decoded is the panel's cache, by hash; LoadImage adds to it.
	decoded map[string]imagedecode.Thumbnail
}

func (inst *artImageResolver) LoadImage(ref string) (pixels []uint32, widthPx uint32, heightPx uint32, ok bool) {
	name, _, _ := strings.Cut(ref, "#")
	i := findImage(inst.set, name)
	if i < 0 || inst.set[i].purged {
		return
	}
	hash := inst.set[i].hash
	t, has := inst.decoded[hash]
	if !has {
		data, err := inst.store.read(hash)
		if err != nil {
			return
		}
		if t, err = imagedecode.DecodeThumbnailRGBA8(data, int(inst.store.limits.pixels), imageInlineSide); err != nil {
			return
		}
		inst.decoded[hash] = t
	}
	return t.Pixels, t.WidthPx, t.HeightPx, len(t.Pixels) > 0
}

// setNamesHash says whether an entry of set has hash's bytes.
func setNamesHash(set []artImage, hash string) bool {
	for _, e := range set {
		if e.hash == hash && !e.purged {
			return true
		}
	}
	return false
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
