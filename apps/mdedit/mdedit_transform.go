package mdedit

// The LLM transformation surface (the transform sibling package does the
// network half; nothing in THIS package imports the LLM client).
//
// The flow is preview-then-apply, never rewrite-on-arrival: a whole-buffer
// rebind is invisible to the editor's own undo (M3's standing caveat), so a
// transformation the reader has not looked at must not be one. The result
// lands in a bottom pane rendered as markdown beside the live preview, and
// Apply splices it over exactly the span that was sent — refusing when the
// buffer has moved since, because a splice computed against one buffer must
// not land on another. Discard and Copy are the other two verdicts.
//
// The whole surface is env-gated (transform.ConfigFromEnv): no endpoint or no
// model means no picker, no pane, no probing — ADR-0120 §SD3's "no dead tab".
// The endpoint HOST is shown beside the picker for the same section's reason:
// where the text goes should be readable where it is sent from.
//
// The run itself sits behind a bgjob.Runner: the compute goroutine builds the
// client, runs one completion and never touches a c.* call; the render thread
// polls, sustains repaint, and consumes the result once.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stergiotis/boxer/apps/mdedit/transform"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

const (
	// transformPaneFrac is the result pane's share of the window height, with
	// a fallback for the frames before the window probe reports. Derived every
	// frame, the app's standing rule for pane sizes (see sourceSplitFrac).
	transformPaneFrac       = float32(0.32)
	transformPaneFallbackPx = float32(260)
	transformPaneMinPx      = float32(160)
)

const (
	tipTransformPicker = "Pick a transformation. Each is a prompt document in the embedded corpus; its tooltip says what it does and what it runs over."

	tipTransformRun = "Run the picked transformation over the selection (when it takes one and one exists) or the whole document. The result lands in a pane below for review — nothing is rewritten until Apply."

	tipTransformHost = "Where the text is sent: the transformation endpoint's host, from BOXER_MDEDIT_LLM_ENDPOINT."

	tipTransformApply = "Replace the text that was sent with the result. Refused if the buffer changed since the run started. A rebind from outside the editor — not an edit its undo describes."

	tipTransformDiscard = "Drop the result. The buffer is untouched."

	tipTransformCopy = "Copy the result to the clipboard, leaving the buffer and its dirty marker alone."

	tipTransformTruncated = "The completion hit the token ceiling — this is everything the model produced, and it may stop mid-thought. Raise BOXER_MDEDIT_LLM_MAXTOKENS (or the prompt's max-tokens) for more room."
)

var (
	atomsTransformRun     = c.Atoms().Text("Run").Keep()
	atomsTransformApply   = c.Atoms().Text("Apply").Keep()
	atomsTransformDiscard = c.Atoms().Text("Discard").Keep()
	atomsTransformCopy    = c.Atoms().Text("Copy").Keep()
)

// transformState is everything the surface owns, one field on App (the
// findState shape).
type transformState struct {
	// enabled, cfg and host are resolved once in Mount from the env registry.
	enabled bool
	cfg     transform.Config
	host    string

	// defs is the parsed prompt corpus, loaded on the first gated frame;
	// defsOk separates "loaded, empty" from "never loaded". sel indexes defs.
	defs   []transform.PromptDef
	defsOk bool
	sel    int

	runner bgjob.Runner[transform.Result]

	// The request snapshot: the buffer as it stood when the run started and
	// the byte span that was sent — what Apply must splice against, and the
	// staleness check when it comes back. reqTitle names the run in the
	// progress row and the pane header.
	reqSrc   string
	reqStart int
	reqStop  int
	reqTitle string

	// The verdict-pending result (nil while none), its parsed preview keyed
	// by text (the doc/docSrc shape), or the failure line.
	res       *transform.Result
	resDoc    *markdown.Doc
	resDocSrc string
	errText   string
}

// transformPaneOpen reports whether the result pane renders this frame: there
// is a result awaiting a verdict, or a failure to read.
func (inst *App) transformPaneOpen() (yes bool) {
	yes = inst.xform.res != nil || inst.xform.errText != ""
	return
}

// transformPaneHeight derives the pane's height from the measured window.
func (inst *App) transformPaneHeight() (px float32) {
	if inst.winH <= 0 {
		return transformPaneFallbackPx
	}
	px = inst.winH * transformPaneFrac
	if px < transformPaneMinPx {
		px = transformPaneMinPx
	}
	return
}

// ensureTransformDefs loads the prompt corpus once. Parse failures in a
// contributed book cost their own entries and a log line, never the bar —
// the in-tree corpus is separately held to zero errors by its gate test.
func (inst *App) ensureTransformDefs() {
	x := &inst.xform
	if x.defsOk {
		return
	}
	x.defsOk = true
	defs, errs := transform.All()
	for _, e := range errs {
		inst.logger.Warn().Err(e).Msg("mdedit: transform prompt failed to parse")
	}
	x.defs = defs
}

// transformSpan resolves a scope against the buffer and caret: the selection
// when the scope wants one and one exists — in a mode where the editor (and
// so the selection) is visible — else the whole document. Byte offsets.
func (inst *App) transformSpan(scope transform.ScopeE) (start, stop int) {
	if scope == transform.ScopeSelection && inst.editorVisible() {
		a, b := c.UnpackCursorRange(inst.cursor)
		if a != b {
			if a > b {
				a, b = b, a
			}
			return charToByte(inst.src, a), charToByte(inst.src, b)
		}
	}
	return 0, len(inst.src)
}

// startTransform snapshots the request and launches the run. The snapshot is
// taken on the render thread and the compute closure touches only its copies
// — the bgjob contract.
func (inst *App) startTransform(def transform.PromptDef) {
	x := &inst.xform
	start, stop := inst.transformSpan(def.Scope)
	input := inst.src[start:stop]
	if strings.TrimSpace(input) == "" {
		inst.status = "nothing to transform"
		return
	}
	x.reqSrc, x.reqStart, x.reqStop = inst.src, start, stop
	x.reqTitle = def.Title
	x.res, x.errText = nil, ""
	x.resDoc, x.resDocSrc = nil, ""

	cfg := x.cfg
	ok := x.runner.StartReporting(nil,
		bgjob.Spec{Kind: "mdedit-transform", Title: def.Title},
		func(ctx context.Context, _ bgjob.Reporter) (res *transform.Result, err error) {
			client, err := transform.NewClient(cfg)
			if err != nil {
				return
			}
			defer func() { _ = client.Close() }()
			var r transform.Result
			r, err = transform.Run(ctx, client, cfg, def, input)
			if err != nil {
				return
			}
			res = &r
			return
		})
	if !ok {
		inst.status = "a transformation is already running"
	}
}

// drainTransform moves a finished run onto the render thread, once per frame
// (called from renderBody beside drainAsync). A cancel is not a failure: it
// was the reader's own gesture, so it lands in the status line rather than
// opening the pane to explain itself.
func (inst *App) drainTransform() {
	x := &inst.xform
	if res, _, ok := x.runner.TakeResult(); ok {
		x.res = res
		x.errText = ""
		x.resDoc, x.resDocSrc = nil, ""
		return
	}
	snap := x.runner.Snapshot()
	if snap.State != bgjob.StateFailed {
		return
	}
	x.runner.Invalidate() // consume the failure; the pane holds it from here
	if errors.Is(snap.Err, context.Canceled) {
		inst.status = "transformation cancelled"
		return
	}
	x.res = nil
	x.errText = transform.FailureLine(snap.Err)
	if snap.Err != nil {
		x.errText += " — " + snap.Err.Error()
	}
}

// ---------------------------------------------------------------------------
// Bar surface
// ---------------------------------------------------------------------------

// renderTransformPicker draws the picker, the Run button and the endpoint
// host, at the end of the bar's first row. Rendered only when the env gate is
// open; the Run click is dropped while a run is in flight (the house rule for
// in-flight gestures).
func (inst *App) renderTransformPicker() {
	x := &inst.xform
	inst.ensureTransformDefs()
	if len(x.defs) == 0 {
		return
	}
	if x.sel < 0 || x.sel >= len(x.defs) {
		x.sel = 0
	}
	cur := x.defs[x.sel]

	for range c.HoverText(tipTransformPicker).KeepIter() {
		for range c.ComboBox(inst.ids.PrepareStr("xform-pick"),
			c.WidgetText().Text("Transform").Keep(),
			c.WidgetText().Text(transformEntryLabel(cur)).Keep()).
			KeepIter() {
			for i, def := range x.defs {
				clicked := false
				for range c.HoverText(def.Summary).KeepIter() {
					clicked = c.Button(inst.ids.PrepareStr("xf-"+def.Slug),
						c.Atoms().Text(transformEntryLabel(def)).Keep()).
						Frame(false).
						Selected(i == x.sel).
						SendResp().HasPrimaryClicked()
				}
				if clicked {
					x.sel = i
				}
			}
		}
	}

	run := false
	for range c.HoverText(tipTransformRun).KeepIter() {
		run = c.Button(inst.ids.PrepareStr("xform-run"), atomsTransformRun).
			SendResp().HasPrimaryClicked()
	}
	if run && !x.runner.Running() {
		inst.startTransform(cur)
	}

	for range c.HoverText(tipTransformHost).KeepIter() {
		c.Label("→ " + x.host).Selectable(false).Send()
	}
}

// transformEntryLabel is a definition's one-line face: icon and title.
func transformEntryLabel(def transform.PromptDef) (s string) {
	if def.Icon == "" {
		return def.Title
	}
	return def.Icon + " " + def.Title
}

// renderTransformProgress draws the transient progress block under the bar
// rows while a run is in flight. Indeterminate — a completion has no
// meaningful fraction — with the cancel affordance the timeout story leans
// on.
func (inst *App) renderTransformProgress() {
	x := &inst.xform
	if !x.runner.Running() {
		return
	}
	// The worker cannot wake the frame loop; polling is the contract, so the
	// render thread keeps itself running while the job does.
	c.RequestRepaint()
	snap := x.runner.Snapshot()
	if jobprogress.Render(jobprogress.Input{
		Title:    x.reqTitle,
		Fraction: snap.Fraction,
		EtaMs:    snap.EtaMs,
		Note:     snap.Note,
		CancelId: inst.ids.PrepareStr("xform-cancel"),
	}) {
		x.runner.Cancel()
	}
}

// ---------------------------------------------------------------------------
// The result pane
// ---------------------------------------------------------------------------

// ensureTransformDoc parses the result for preview, text-keyed like the main
// preview's doc/docSrc gate. Render goroutine only (markdown.Parse lowers
// through FFI).
func (inst *App) ensureTransformDoc() {
	x := &inst.xform
	if x.res == nil {
		return
	}
	if x.resDoc != nil && x.resDocSrc == x.res.Content {
		return
	}
	x.resDoc = markdown.Parse([]byte(x.res.Content))
	x.resDocSrc = x.res.Content
}

// renderTransformPane draws the verdict pane: header (what ran, against what,
// the verdict buttons), then the result rendered as markdown — beside the
// live preview, which is the comparison Apply is judged on. Callers own the
// enclosing panel.
func (inst *App) renderTransformPane() {
	x := &inst.xform

	for range c.HorizontalTop().KeepIter() {
		c.Label(x.reqTitle).Selectable(false).Send()
		if x.res != nil {
			c.Label(inst.transformSummaryLine()).Selectable(false).Send()
			if x.res.Truncated {
				badge.New(inst.ids.PrepareStr("xform-trunc"), "truncated").
					Tone(badge.ToneWarning).Variant(badge.VariantSoft).Size(badge.SizeSm).
					Tooltip(tipTransformTruncated).Send()
			}
		}

		apply, discard, copyRes := false, false, false
		if x.res != nil {
			for range c.HoverText(tipTransformApply).KeepIter() {
				apply = c.Button(inst.ids.PrepareStr("xform-apply"), atomsTransformApply).
					SendResp().HasPrimaryClicked()
			}
			for range c.HoverText(tipTransformCopy).KeepIter() {
				copyRes = c.Button(inst.ids.PrepareStr("xform-copy"), atomsTransformCopy).
					SendResp().HasPrimaryClicked()
			}
		}
		for range c.HoverText(tipTransformDiscard).KeepIter() {
			discard = c.Button(inst.ids.PrepareStr("xform-discard"), atomsTransformDiscard).
				SendResp().HasPrimaryClicked()
		}
		switch {
		case apply:
			inst.applyTransform()
		case discard:
			inst.discardTransform()
		case copyRes && !inst.exportInFlight():
			inst.copyToClipboard(x.res.Content, false)
		}
	}

	if x.errText != "" {
		c.Label(x.errText).Send()
		return
	}
	if x.res == nil {
		return
	}
	inst.ensureTransformDoc()
	for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).KeepIter() {
		// Its own IdScope: the main preview renders the same widget kinds in
		// the same window, and markdown.Doc deliberately does not scope itself.
		for range c.IdScope(inst.ids.PrepareStr("xform-preview")) {
			x.resDoc.Render(inst.ids)
		}
	}
}

// transformSummaryLine is the pane header's provenance readout: model, host,
// elapsed, tokens. Everything a reader needs to judge what they are about to
// apply.
func (inst *App) transformSummaryLine() (s string) {
	x := &inst.xform
	var b strings.Builder
	b.WriteString(x.cfg.Model)
	b.WriteString(" · ")
	b.WriteString(x.host)
	b.WriteString(" · ")
	b.WriteString(x.res.Elapsed.Round(100 * time.Millisecond).String())
	if x.res.InputTokens > 0 || x.res.OutputTokens > 0 {
		b.WriteString(" · ")
		b.WriteString(itoa(int(x.res.InputTokens)))
		b.WriteString("→")
		b.WriteString(itoa(int(x.res.OutputTokens)))
		b.WriteString(" tok")
	}
	return b.String()
}

// spliceResult replaces src[start:stop] with content — Apply's one pure step.
func spliceResult(src string, start, stop int, content string) (out string) {
	out = src[:start] + content + src[stop:]
	return
}

// applyTransform splices the result over the span that was sent. The
// staleness check is the simplest sound rule: any edit since the run started
// refuses the apply, because a splice computed against one buffer must not
// land on another — re-anchoring a moved span is a judgment this gesture
// should not make silently.
func (inst *App) applyTransform() {
	x := &inst.xform
	if x.res == nil {
		return
	}
	if inst.src != x.reqSrc {
		inst.status = "buffer changed since the transformation ran — copy or discard instead"
		return
	}
	if inst.rebindBuffer(spliceResult(inst.src, x.reqStart, x.reqStop, x.res.Content)) {
		inst.status = "applied: " + x.reqTitle
	}
	inst.discardTransform()
}

// discardTransform closes the pane and drops the result and its request
// snapshot.
func (inst *App) discardTransform() {
	x := &inst.xform
	x.res = nil
	x.errText = ""
	x.resDoc, x.resDocSrc = nil, ""
	x.reqSrc, x.reqStart, x.reqStop = "", 0, 0
}
