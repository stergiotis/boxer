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
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowopen"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdlint"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/decision"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// artTabE names a view of the document: a tab of the document's own dock,
// whose stable tab id it is.
type artTabE uint64

// artTabReadTitle names the rendered document: Read, the word mdedit's and
// the notebook's switches use for the view that shows a document as it reads
// (keelson-ux N2); the outer tab is the Document.
const artTabReadTitle = "Read"

const (
	artTabDocument artTabE = iota + 1
	artTabSource
	artTabRevisions
	artTabLint
	artTabImages
)

const (
	// artefactPanelW is the width the artefact's previews are drawn for.
	artefactPanelW float32 = 460
	// maxDiffShown bounds the diff lines drawn; the rest is counted.
	maxDiffShown = 400

	tipAccept    = "Make this change the artefact's next revision; the model's call goes on."
	tipReject    = "Turn the change down; the model is told, and asked not to repeat it."
	tipRevert    = "Make this revision current again, as a new revision. A later turn's model is told the artefact moved."
	tipArtMdedit = "Open the artefact as a new, unsaved document in mdedit, where you can type. With screenshots, it is exported first, so mdedit shows them. Nothing comes back to the chat."
)

var (
	atomsRevert     = c.Atoms().Text(icons.PhArrowCounterClockwise + " Revert to this").Keep()
	atomsArtCopy    = c.Atoms().Text(icons.PhCopy + " Copy").Keep()
	atomsArtEndorse = c.Atoms().Text(icons.PhSealCheck + " Endorse").Keep()
	atomsArtMdedit  = c.Atoms().Text(icons.PhMarkdownLogo + " Open in mdedit").Keep()
)

// artefactView is the panel's state, all of it the render goroutine's. The
// parsed document, source job and findings are built once per revision.
type artefactView struct {
	// raised is the question the Read tab was last brought forward
	// for (artefactQuestion).
	raised string
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
	// export is the export waiting for the person's confirmation.
	export *exportPlan
	// inline holds the screenshots the document embeds, decoded at the
	// inline bound, by hash; a revision that keeps an image does not decode
	// it again.
	inline map[string]imagedecode.Thumbnail
}

// ensure builds the views of revision n; set is its screenshots, which the
// document shows where it embeds them.
func (inst *artefactView) ensure(n int, text string, set []artImage, store *imageStore) {
	if inst.built && inst.builtFor == n {
		return
	}
	inst.built, inst.builtFor = true, n
	if inst.inline == nil {
		inst.inline = make(map[string]imagedecode.Thumbnail)
	}
	inst.doc = markdown.ParseWith([]byte(withoutChatProperties(text)), markdown.ParseOptions{
		Resolver:  &artImageResolver{set: set, store: store, decoded: inst.inline},
		ImageMaxW: uint32(artefactPanelW) - 40,
		ImageMaxH: pixelPreviewH,
	})
	// The parsed document holds the pixels it shows; the cache keeps only
	// what this revision names.
	for hash := range inst.inline {
		if !setNamesHash(set, hash) {
			delete(inst.inline, hash)
		}
	}
	inst.srcOk = text != ""
	if inst.srcOk {
		inst.srcJob = codeview.BuildMarkdownLex(text)
	}
	inst.findings = artLinter.LintSource([]byte(text))
}

// diffOf is the diff of two revisions as the person reads them, without the
// chat's properties (withoutChatProperties), built once per key.
func (inst *artefactView) diffOf(key string, old string, new string) []diffLine {
	if inst.diffKey != key {
		inst.diffKey, inst.diff = key, lineDiff(withoutChatProperties(old), withoutChatProperties(new))
	}
	return inst.diff
}

// renderArtefactTab is the Artefact tab; it says how to have one while the
// conversation has none.
func (inst *App) renderArtefactTab() {
	if !inst.artefactOn() {
		c.Label("This conversation has no artefact. Turn Artefact on in Settings before the first message.").Wrap().Send()
		return
	}
	for range c.IdScope(inst.ids.PrepareStr("artefact-body")) {
		inst.renderArtefact()
	}
}

func (inst *App) renderArtefact() {
	art := inst.conv.art
	v := &inst.artView
	n, text, set := art.headWithImages()
	v.ensure(n, text, set, art.store)
	// The buttons keep their labels on one line (Extend) and carry their
	// tooltips themselves, so in a narrow column the row wraps between them
	// rather than inside a label or past the edge. Before the first revision
	// there is nothing to count or act on, and the row is not drawn: a first
	// proposal heads the view on its own.
	//
	// While the export question is open, the command that raised it is not
	// offered beside it (keelson-ux C8): Export, and Open in mdedit when it
	// asked to export first. The answer is in the question.
	asking := inst.artView.export
	if n > 0 || text != "" {
		for range c.HorizontalWrapped().KeepIter() {
			c.Label("revision " + strconv.Itoa(n) + " · " + plural(lineCount(text), "line")).Selectable(false).Send()
			if text != "" {
				if c.Button(inst.ids.PrepareStr("art-copy"), atomsArtCopy).Small().Extend().SendResp().HasPrimaryClicked() {
					inst.copyCited("the artefact", text, text+citationFoot(text, inst.conv.citationIndex()))
				}
				inst.renderEndorse(n, text)
				exportable := inst.exportable()
				if exportable && asking == nil {
					if c.Button(inst.ids.PrepareStr("art-export"), atomsArtExport).Small().Extend().HoverText(tipArtExport).SendResp().HasPrimaryClicked() {
						inst.planExport(false)
					}
				}
				if asking == nil || !asking.thenMdedit {
					if c.Button(inst.ids.PrepareStr("art-mdedit"), atomsArtMdedit).Small().Extend().HoverText(tipArtMdedit).SendResp().HasPrimaryClicked() {
						if hasLiveImages(set) && exportable {
							inst.planExport(true)
						} else {
							inst.openArtefactInMdedit(text)
						}
					}
				}
			}
		}
	}
	if text != "" {
		if inst.renderCiteHold("art-copy", "copy the artefact", text) {
			inst.copyCited("the artefact", text, text+citationFoot(text, inst.conv.citationIndex()))
		}
		if inst.renderCiteHold("art-mdedit", "open the artefact in mdedit", text) {
			inst.openArtefactInMdedit(text)
		}
	}
	if text != "" && !inst.exportable() {
		weak("An artefact of this window does not export; Open in mdedit takes its text without its screenshots.")
	}
	// A question open in the document says what its answers do; the line
	// on how changes arrive would say it again and push the answers down.
	switch p := art.policyNow(); {
	case inst.artefactQuestion() != "":
	case !p.write:
		weak("The model may read the artefact, not change it: At most in Settings is below Edit.")
	case p.ask:
		weak("Each change the model makes waits for you here.")
	default:
		weak("The model's changes apply at once; the Revisions tab can undo any of them.")
	}
	c.AddSpace(styletokens.PaddingInner(styletokens.ActiveDensity()))
	inst.renderArtefactViews(art, n, text)
}

// renderArtefactDecisions are the questions that wait in the document: a
// proposed change, the export the person asked for, a screenshot the model
// asks to see. They head the Read tab, so the tab strip stays in place
// above however tall they are.
func (inst *App) renderArtefactDecisions(art *artefact, n int, text string) {
	if p := art.pending(); p != nil {
		inst.renderProposal(p, n, text)
	}
	if p := inst.artView.export; p != nil {
		for range c.IdScope(inst.ids.PrepareStr("export-ask")) {
			inst.renderExportAsk(p, n)
		}
	}
	if a := inst.coord.pixelAskNow(); a != nil {
		for range c.IdScope(inst.ids.PrepareStr("pixel-ask")) {
			inst.renderPixelAsk(a)
		}
	}
}

// artefactQuestion names the question waiting in the document, "" for
// none: the model's, or the export the person asked for.
func (inst *App) artefactQuestion() string {
	if a := inst.artefactAsks(); a != "" {
		return a
	}
	if p := inst.artView.export; p != nil {
		return "export-" + strconv.Itoa(p.rev) + "-" + strconv.FormatBool(p.thenMdedit)
	}
	return ""
}

// renderArtefactViews are the document's own views, as tabs of a dock of
// their own (ADR-0404 §SD7): one way to switch views in the window, and
// Source can be dragged beside Read. Each tab's body scrolls in its
// leaf. A question that arrives brings the Read tab, where it is
// drawn, to the front, once.
func (inst *App) renderArtefactViews(art *artefact, n int, text string) {
	v := &inst.artView
	q := inst.artefactQuestion()
	lintLabel := "Lint"
	if len(v.findings) > 0 {
		lintLabel += " (" + strconv.Itoa(len(v.findings)) + ")"
	}
	imagesLabel := "Images"
	if k := len(art.headImages()); k > 0 {
		imagesLabel += " (" + strconv.Itoa(k) + ")"
	}
	for dock := range c.DockArea(inst.ids.PrepareStr("art-dock")) {
		dock.InitRoot(uint64(artTabDocument), uint64(artTabSource), uint64(artTabRevisions), uint64(artTabLint), uint64(artTabImages))
		if q != "" && q != v.raised {
			dock.ActivateTab(uint64(artTabDocument))
		}
		v.raised = q
		for range dock.Tab(uint64(artTabDocument), artTabReadTitle) {
			inst.renderArtefactDecisions(art, n, text)
			if text == "" {
				weak("Empty. The model writes here once it is asked to.")
				break
			}
			inst.followLinks(markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "art-doc", Doc: v.doc, Frontmatter: true, LinkClaims: inst.linkClaims(), LinkHover: inst.linkHover(artefactAuthor)}), artefactAuthor)
			for _, line := range citationLines(text, inst.conv.citationIndex()) {
				weak(line)
			}
		}
		for range dock.Tab(uint64(artTabSource), "Source") {
			if !v.srcOk {
				weak("Empty.")
				break
			}
			c.CodeView(inst.ids.PrepareStr("art-src"), v.srcJob).Send()
		}
		for range dock.Tab(uint64(artTabRevisions), "Revisions") {
			inst.renderRevisions(art, n)
		}
		for range dock.Tab(uint64(artTabLint), lintLabel) {
			inst.renderFindings(v.findings)
		}
		for range dock.Tab(uint64(artTabImages), imagesLabel) {
			inst.renderImages(art)
		}
	}
}

// renderProposal is a change waiting for the person, drawn as a decision
// (ADR-0404 §SD3, §SD4). The diff against the head follows the options.
func (inst *App) renderProposal(p *artProposal, head int, text string) {
	if p.ownImages {
		inst.renderImageProposal(p, head)
		return
	}
	d := inst.artView.diffOf("proposal-"+strconv.FormatUint(p.seq, 10), text, p.text)
	add, del := diffCounts(d)
	subject := plural(add, "line") + " added, " + strconv.Itoa(del) + " removed"
	if p.title != "" {
		subject = p.title + " · " + subject
	}
	in := decision.Input{
		Ids: inst.ids, ScopeKey: "proposal", Origin: decision.OriginRequest,
		Title:   "The model proposes a change to the artefact",
		Subject: subject,
		Follows: []string{
			"Accept writes this change into the artefact as revision " + strconv.Itoa(head+1) + "; the model goes on with its turn.",
			"Reject discards it, and the model is told you turned it down and not to propose it again.",
		},
		Options: proposalOptions(p.tool),
		Detail: func() {
			for range c.ScrollArea().Vscroll(true).Hscroll(true).MaxHeight(240).KeepIter() {
				renderDiff(d)
			}
		},
	}
	if p.base != head {
		in.Notes = []decision.Note{{Text: noteStaleProposal, Tone: styletokens.ToneWarning}}
	}
	inst.decideProposal(p, decision.Render(in))
}

// noteStaleProposal is the note on a proposal made against an earlier
// revision.
const noteStaleProposal = "The artefact changed since the model proposed this; Accept will be refused as out of date."

// proposalOptions are Accept and Reject; the tool the model called is in
// their hover, for whoever reads the call record.
func proposalOptions(tool string) []decision.Option {
	return []decision.Option{
		{Key: "accept", Label: "Accept", Icon: icons.PhCheck, Role: decision.RoleAffirm, Tooltip: tipAccept + " (" + tool + ")"},
		{Key: "reject", Label: "Reject", Icon: icons.PhX, Role: decision.RoleDecline, Tooltip: tipReject},
	}
}

// decideProposal hands the person's answer to the waiting tool call.
func (inst *App) decideProposal(p *artProposal, res decision.Result) {
	switch res.Chosen {
	case "accept":
		p.decide(true)
	case "reject":
		p.decide(false)
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
						// The head moved between turns: written for a
						// resume now, as a rewind's is.
						if coord := inst.coord; coord != nil {
							coord.syncArtefactJournal(journalCtx(context.Background()), coord.journalNow(), "", artefactFormRevert)
						}
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
	case revSourceResume:
		return "restored from the journal"
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
	if inst.heldByCitations("open the artefact in mdedit", text) {
		return
	}
	text += citationFoot(text, inst.conv.citationIndex())
	name := "Artefact"
	if inst.conv.title != "" {
		name = inst.conv.title + " — artefact"
	}
	l := launchcfg.MdeditLaunch{Text: text, Name: name}
	// Exported as it stands: mdedit gets the snapshot too, so it can say
	// where the document lives (ADR-0342 §SD5).
	if w := inst.coord.journalNow(); w != nil {
		n, _ := inst.conv.art.head()
		if rec, ok := exportOf(w.Entries(), n, text); ok {
			l.Mount, l.SnapNanos, l.Path = rec.Mount, rec.SnapNanos, exportDocPath
		}
	}
	bus := inst.bus
	inst.runAction("chat-open-artefact", "open the artefact in mdedit", func(ctx context.Context) (note string, err error) {
		cfg, err := buscodec.Encode(l)
		if err != nil {
			err = eh.Errorf("encode the mdedit launch config: %w", err)
			return
		}
		if _, err = windowopen.RequestOpen(ctx, bus, launchcfg.AppId, launchcfg.Kind, cfg); err != nil {
			err = eh.Errorf("open mdedit: %w", err)
			return
		}
		return "opened the artefact in mdedit", nil
	}, nil)
}

// tipEndorse is the Endorse button's word.
const tipEndorse = "Record that you reviewed this revision and stand behind it for publishing (ADR-0311): the record names the revision and what changed since you last endorsed one. A later model edit voids it; your own edit does not."

// renderEndorse is the deliberate gesture that endorses the artefact's
// head; it shows whether the head is endorsed.
func (inst *App) renderEndorse(n int, text string) {
	coord := inst.coord
	if coord == nil || coord.journalNow() == nil {
		return
	}
	if coord.endorsedHead() {
		c.Label(icons.PhSealCheck + " endorsed").Selectable(false).Send()
		return
	}
	if c.Button(inst.ids.PrepareStr("art-endorse"), atomsArtEndorse).Small().Extend().HoverText(tipEndorse).SendResp().HasPrimaryClicked() {
		inst.runAction("chat-endorse", "endorse the artefact", func(ctx context.Context) (note string, err error) {
			if err = coord.endorseArtefact(ctx, n, text); err != nil {
				return
			}
			return "endorsed revision " + strconv.Itoa(n), nil
		}, nil)
	}
}

// hasLiveImages says the set names a screenshot whose bytes are held.
func hasLiveImages(set []artImage) bool {
	for _, e := range set {
		if !e.purged {
			return true
		}
	}
	return false
}
