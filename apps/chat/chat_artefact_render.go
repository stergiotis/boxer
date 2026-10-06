package chat

// The Artefact panel (ADR-0282 §SD6): the document as rendered or as
// source, a change waiting under Ask first with Accept and Reject, the
// revisions with their diffs and Revert, and the lint findings. The person
// reviews here and does not type; Open in mdedit hands the text over.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdlint"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// artTabE is what the panel shows.
type artTabE uint8

const (
	artTabDocument artTabE = iota
	artTabSource
	artTabRevisions
	artTabLint
	artTabImages
)

const (
	artefactPanelW float32 = 460
	// maxDiffShown bounds the diff lines drawn; the rest is counted.
	maxDiffShown = 400

	tipArtefactPanel = "The conversation's markdown document, which the model edits through tools"
	tipAccept        = "Make this change the artefact's next revision; the model's call goes on."
	tipReject        = "Turn the change down; the model is told, and asked not to repeat it."
	tipRevert        = "Make this revision current again, as a new revision. A later turn's model is told the artefact moved."
	tipArtMdedit     = "Open the artefact as a new, unsaved document in mdedit, where you can type. Nothing comes back to the chat."
)

var (
	atomsArtefact  = c.Atoms().Text(icons.PhFileText + " Artefact").Keep()
	atomsAccept    = c.Atoms().Text(icons.PhCheck + " Accept").Keep()
	atomsReject    = c.Atoms().Text(icons.PhX + " Reject").Keep()
	atomsRevert    = c.Atoms().Text(icons.PhArrowCounterClockwise + " Revert to this").Keep()
	atomsArtCopy   = c.Atoms().Text(icons.PhCopy + " Copy").Keep()
	atomsArtMdedit = c.Atoms().Text(icons.PhMarkdownLogo + " Open in mdedit").Keep()
)

// artefactView is the panel's state, all of it the render goroutine's. The
// parsed document, source job and findings are built once per revision.
type artefactView struct {
	tab artTabE
	// selected is the revision the Revisions tab shows the diff of, 0 for
	// none.
	selected int

	builtFor int
	built    bool
	doc      *markdown.Doc
	srcJob   typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	srcOk    bool
	findings []mdlint.Finding

	// diff caches the selected revision's or the proposal's diff, keyed.
	diffKey string
	diff    []diffLine
	// thumbs records which screenshot thumbnails the host holds.
	thumbs *c.ImageVersionTracker[uint64]
}

// ensure builds the views of revision n.
func (inst *artefactView) ensure(n int, text string) {
	if inst.built && inst.builtFor == n {
		return
	}
	inst.built, inst.builtFor = true, n
	inst.doc = markdown.Parse([]byte(text))
	inst.srcOk = text != ""
	if inst.srcOk {
		inst.srcJob = codeview.BuildMarkdownLex(text)
	}
	inst.findings = artLinter.LintSource([]byte(text))
}

func (inst *artefactView) diffOf(key string, old string, new string) []diffLine {
	if inst.diffKey != key {
		inst.diffKey, inst.diff = key, lineDiff(old, new)
	}
	return inst.diff
}

// renderArtefactToggle is the bar's Artefact button, shown for a
// conversation that has one.
func (inst *App) renderArtefactToggle() {
	if !inst.artefactOn() {
		return
	}
	label := atomsArtefact
	if inst.conv.art.pending() != nil || inst.coord.pixelAskNow() != nil {
		label = c.Atoms().Text(icons.PhFileText + " Artefact · waiting").Keep()
	}
	for range c.HoverText(tipArtefactPanel).KeepIter() {
		if c.Button(inst.ids.PrepareStr("artefact-toggle"), label).Selected(inst.showArtefact).SendResp().HasPrimaryClicked() {
			inst.showArtefact = !inst.showArtefact
		}
	}
}

// renderArtefactPanel is the panel, beside the transcript.
func (inst *App) renderArtefactPanel() {
	if !inst.showArtefact || !inst.artefactOn() {
		return
	}
	for range c.PanelRightInside(inst.ids.PrepareStr("artefact")).DefaultSize(artefactPanelW).Resizable(true).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("artefact-body")) {
			inst.renderArtefact()
		}
	}
}

func (inst *App) renderArtefact() {
	art := inst.conv.art
	v := &inst.artView
	n, text := art.head()
	v.ensure(n, text)
	heading("Artefact")
	for range c.HorizontalTop().KeepIter() {
		c.Label("revision " + strconv.Itoa(n) + " · " + plural(lineCount(text), "line")).Selectable(false).Send()
		if text != "" {
			if c.Button(inst.ids.PrepareStr("art-copy"), atomsArtCopy).Small().SendResp().HasPrimaryClicked() {
				inst.copyText("the artefact", text)
			}
			for range c.HoverText(tipArtMdedit).KeepIter() {
				if c.Button(inst.ids.PrepareStr("art-mdedit"), atomsArtMdedit).Small().SendResp().HasPrimaryClicked() {
					inst.openArtefactInMdedit(text)
				}
			}
		}
	}
	switch p := art.policyNow(); {
	case !p.write:
		weak("The model may read the artefact, not change it: At most is below Edit.")
	case p.ask:
		weak("Each change the model makes waits for you here.")
	default:
		weak("The model's changes apply directly; Revisions can take any back.")
	}
	if p := art.pending(); p != nil {
		inst.renderProposal(p, n, text)
	}
	if a := inst.coord.pixelAskNow(); a != nil {
		for range c.IdScope(inst.ids.PrepareStr("pixel-ask")) {
			inst.renderPixelAsk(a)
		}
	}
	c.AddSpace(4)
	lintLabel := "Lint"
	if len(v.findings) > 0 {
		lintLabel += " (" + strconv.Itoa(len(v.findings)) + ")"
	}
	imagesLabel := "Images"
	if k := len(art.headImages()); k > 0 {
		imagesLabel += " (" + strconv.Itoa(k) + ")"
	}
	selector.Segmented(inst.ids, "art-tab", &v.tab).
		Option(artTabDocument, "Document").Option(artTabSource, "Source").
		Option(artTabRevisions, "Revisions").Option(artTabLint, lintLabel).Option(artTabImages, imagesLabel).Send()
	c.Separator().Horizontal().Send()
	for range c.ScrollArea().Vscroll(true).Hscroll(false).AutoShrink(false, false).KeepIter() {
		switch v.tab {
		case artTabDocument:
			if text == "" {
				weak("Empty. The model writes here once it is asked to.")
				break
			}
			markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "art-doc", Doc: v.doc, Frontmatter: true})
		case artTabSource:
			if !v.srcOk {
				weak("Empty.")
				break
			}
			c.CodeView(inst.ids.PrepareStr("art-src"), v.srcJob).Send()
		case artTabRevisions:
			inst.renderRevisions(art, n)
		case artTabLint:
			inst.renderFindings(v.findings)
		case artTabImages:
			inst.renderImages(art)
		}
	}
}

// renderProposal is a change waiting for the person: its diff against the
// head, Accept and Reject.
func (inst *App) renderProposal(p *artProposal, head int, text string) {
	section("Proposed change")
	what := p.tool
	if p.title != "" {
		what = p.title + " · " + p.tool
	}
	if p.ownImages {
		inst.renderImageProposal(p, what, head)
		return
	}
	d := inst.artView.diffOf("proposal-"+strconv.FormatUint(p.seq, 10), text, p.text)
	add, del := diffCounts(d)
	c.Label(what + " · +" + strconv.Itoa(add) + " −" + strconv.Itoa(del)).Selectable(false).Send()
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
	for range c.ScrollArea().Vscroll(true).Hscroll(true).MaxHeight(240).KeepIter() {
		renderDiff(d)
	}
}

// renderRevisions lists the revisions newest first; the selected one shows
// its diff against the revision before it, and Revert.
func (inst *App) renderRevisions(art *artefact, head int) {
	revs := art.revisions()
	if len(revs) == 0 {
		weak("No revision yet.")
		return
	}
	v := &inst.artView
	for i := len(revs) - 1; i >= 0; i-- {
		r := revs[i]
		label := "r" + strconv.Itoa(r.n) + " · " + revLabel(r) + " · " + time.UnixMilli(r.atMs).Format("15:04:05")
		if r.n == head {
			label += " · current"
		}
		if c.Button(inst.ids.PrepareStr("rev-"+strconv.Itoa(r.n)), c.Atoms().Text(label).Keep()).Frame(false).
			Selected(v.selected == r.n).SendResp().HasPrimaryClicked() {
			if v.selected == r.n {
				v.selected = 0
			} else {
				v.selected = r.n
			}
		}
		if v.selected != r.n {
			continue
		}
		prev := ""
		if i > 0 {
			prev = revs[i-1].text
		}
		if r.imageNote != "" && prev == r.text {
			weak("screenshots: " + r.imageNote + "; the text is unchanged")
		} else {
			renderDiff(v.diffOf("rev-"+strconv.Itoa(r.n), prev, r.text))
		}
		if r.n != head {
			for range c.HoverText(tipRevert).KeepIter() {
				if c.Button(inst.ids.PrepareStr("rev-revert"), atomsRevert).SendResp().HasPrimaryClicked() {
					if _, err := art.revert(r.n); err != nil {
						inst.setNote(err.Error(), true)
					} else {
						v.selected = 0
					}
				}
			}
		}
		c.Separator().Horizontal().Send()
	}
}

// revLabel says what made a revision.
func revLabel(r artRevision) string {
	switch r.source {
	case revSourceRevert:
		return "reverted to r" + strconv.Itoa(r.revertedTo)
	case revSourcePerson:
		return "you · " + r.imageNote
	}
	what := strings.TrimPrefix(r.tool, "artefact_")
	if r.title != "" {
		what = r.title
	}
	if r.changedFirst > 0 {
		what += " · " + lineRangeOf(r.changedFirst, r.changedLast)
	}
	if r.imageNote != "" {
		what += " · " + r.imageNote
	}
	return what
}

func lineRangeOf(first int, last int) string {
	if first == last {
		return "line " + strconv.Itoa(first)
	}
	return "lines " + strconv.Itoa(first) + "–" + strconv.Itoa(last)
}

func renderFindings(fs []mdlint.Finding) {
	if len(fs) == 0 {
		weak("No findings.")
		return
	}
	for _, f := range fs {
		text := strconv.Itoa(int(f.Line)) + ":" + strconv.Itoa(int(f.Col)) + "  " + f.Rule + "  " + f.Message
		switch f.Severity {
		case mdlint.SeverityError:
			for rt := range c.RichTextLabelColored(color.Hex(styletokens.ErrorDefault.AsHex()), color.Transparent, text) {
				rt.Small()
			}
		case mdlint.SeverityWarn:
			for rt := range c.RichTextLabelColored(color.Hex(styletokens.WarningDefault.AsHex()), color.Transparent, text) {
				rt.Small()
			}
		default:
			for rt := range c.RichTextLabel(text) {
				rt.Small().Weak()
			}
		}
	}
}

func (inst *App) renderFindings(fs []mdlint.Finding) { renderFindings(fs) }

// renderDiff draws a diff as monospace lines: added and removed in their
// tones, with a sign as well, since colour is never the only channel.
func renderDiff(d []diffLine) {
	add := color.Hex(styletokens.SuccessDefault.AsHex())
	del := color.Hex(styletokens.ErrorDefault.AsHex())
	for i, l := range d {
		if i == maxDiffShown {
			weak("… " + plural(len(d)-maxDiffShown, "more line"))
			return
		}
		switch l.op {
		case diffOpAdd:
			for rt := range c.RichTextLabelColored(add, color.Transparent, "+ "+l.text) {
				rt.Monospace()
			}
		case diffOpDel:
			for rt := range c.RichTextLabelColored(del, color.Transparent, "- "+l.text) {
				rt.Monospace()
			}
		case diffOpGap:
			for rt := range c.RichTextLabel("  …") {
				rt.Monospace().Weak()
			}
		default:
			for rt := range c.RichTextLabel("  " + l.text) {
				rt.Monospace().Weak()
			}
		}
	}
}

// openArtefactInMdedit hands the artefact to a new mdedit window, as the
// transcript is handed over (ADR-0265 §SD4).
func (inst *App) openArtefactInMdedit(text string) {
	name := "Artefact"
	if inst.conv.title != "" {
		name = inst.conv.title + " — artefact"
	}
	l := launchcfg.MdeditLaunch{Text: text, Name: name}
	bus := inst.bus
	inst.runAction("chat-open-artefact", "open the artefact in mdedit", func(ctx context.Context) (note string, err error) {
		cfg, err := buscodec.Encode(l)
		if err != nil {
			err = eh.Errorf("encode the mdedit launch config: %w", err)
			return
		}
		if _, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfg); err != nil {
			err = eh.Errorf("open mdedit: %w", err)
			return
		}
		return "opened the artefact in mdedit", nil
	})
}
