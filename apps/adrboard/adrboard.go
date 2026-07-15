// Package adrboard renders the doc/adr corpus as a read-only kanban board: one
// card per ADR, filed in the column of its decision status, carrying its
// sub-item progress as a packed tally of done / not-done dots.
//
// The board is a view, never an editor. Cards do not move: a card's column is
// its ADR's frontmatter `status`, and a dot is a ✓ (or its absence) on a
// sub-item declaration — both facts live in the markdown, which is the source
// of truth. Moving a card would have to rewrite an ADR, and an ADR's status is
// a reviewed decision (doclint DL003 gates `accepted` on reviewed-by), not a
// thing to drag. Edit the markdown and press Reload.
//
// The corpus model, and why sub-item done-ness is declared rather than derived
// from code evidence, live in
// [github.com/stergiotis/boxer/public/gov/adrcorpus]. The design is recorded in
// ADR-0092 §Update 2026-07-15.
package adrboard

import (
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/kanban"
)

// App is one open adrboard window.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call (ADR-0026 §SD9), so
	// every widget id derived from it is unique across concurrently open
	// instances. Captured in Mount; the app must NOT Reset() it.
	ids    *c.WidgetIdStack
	logger zerolog.Logger

	// model is rebuilt from scratch on every load; the board mutates nothing
	// (Input.ReadOnly), so there are no moves to drain.
	model   *kanban.Model
	summary string
	adrDir  string
	root    string
	loadErr error
}

func newApp() (inst *App) { return &App{} }

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.logger = ctx.Log()
	inst.load()
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

// load re-reads the corpus and rebuilds the board. A parse failure is kept and
// shown in place of the board rather than failing Mount — the app is still
// useful once the operator points ADRBOARD_DIR somewhere real and reloads.
//
// The code scan walks the whole source tree (~90ms here) to find which
// sub-items are cited by their §marker, which is what the amber dots read. It
// runs inline: at that cost a background job would buy nothing and cost the
// reload its synchrony. A scan failure degrades to no amber dots rather than no
// board — evidence enriches the corpus, it is not the corpus.
func (inst *App) load() {
	inst.adrDir, inst.root, inst.loadErr = adrcorpus.ResolveCorpus()
	if inst.loadErr != nil {
		inst.model = nil
		return
	}
	adrs, err := adrcorpus.ParseDir(inst.adrDir)
	if err != nil {
		inst.loadErr, inst.model = err, nil
		inst.logger.Warn().Err(err).Str("adrDir", inst.adrDir).Msg("adrboard: unable to read the ADR corpus")
		return
	}
	var refs []adrcorpus.CodeRef
	if inst.root == "" {
		inst.logger.Info().Str("adrDir", inst.adrDir).
			Msgf("adrboard: no source tree resolved; code-evidence dots omitted (set %s)", adrcorpus.EnvAdrRootName)
	} else if refs, err = adrcorpus.ScanCodeRefs(inst.root, inst.adrDir, ""); err != nil {
		refs = nil
		inst.logger.Warn().Err(err).Str("root", inst.root).
			Msg("adrboard: code scan failed; showing the board without code-evidence dots")
	}
	// Aggregate folds evidence onto each sub-item even for a nil refs slice,
	// which zeroes the counts rather than leaving a previous load's behind.
	adrs = adrcorpus.Aggregate(adrs, refs)

	inst.model = buildBoard(adrs)
	inst.summary = corpusSummary(adrs)
	inst.logger.Debug().Int("adrs", len(adrs)).Int("coderefs", len(refs)).
		Str("adrDir", inst.adrDir).Str("root", inst.root).Msg("adrboard: corpus loaded")
}

// Frame renders the app body. The host has already pre-pushed a window-unique
// salt onto inst.ids (windowhost.renderWindowBody, ADR-0026 §SD9), so the app
// must not wrap the body in its own instance salt.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.renderBody()
	return
}

func (inst *App) renderBody() {
	dens := styletokens.DensityFromEnv()
	inst.renderHeader(dens)
	if inst.model == nil {
		inst.renderLoadError()
		return
	}
	c.AddSpace(styletokens.GapInline(dens))
	kanban.RenderLegend(inst.model.DotLegend)
	c.AddSpace(styletokens.GapItems(dens))
	kanban.Render(kanban.Input{
		Ids:      inst.ids,
		ScopeKey: "adrboard",
		Model:    inst.model,
		FillHost: true,
		// The corpus is the source of truth; see the package doc.
		ReadOnly: true,
	})
}

func (inst *App) renderHeader(dens styletokens.DensityE) {
	for range c.Horizontal().KeepIter() {
		if c.Button(inst.ids.PrepareStr("reload"), c.Atoms().Text("Reload").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.load()
		}
		c.AddSpace(styletokens.GapInline(dens))
		if inst.model != nil {
			for rt := range c.RichTextLabel(inst.summary) {
				rt.Weak().Small()
			}
		}
	}
}

func (inst *App) renderLoadError() {
	c.AddSpace(styletokens.GapItems(styletokens.DensityFromEnv()))
	for rt := range c.RichTextLabel("Cannot read the ADR corpus.") {
		rt.Strong()
	}
	c.Label(inst.loadErr.Error()).Wrap().Send()
	for rt := range c.RichTextLabel("Set " + adrcorpus.EnvAdrDirName + " to the ADR markdown directory, then press Reload.") {
		rt.Weak().Small()
	}
}
