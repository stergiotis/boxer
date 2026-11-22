package imagescope

// A stack open in the window (ADR-0398 §SD3, §M4): the planes a file read
// through a registered reader holds along named axes, or a sample stack's,
// of which the window shows one selection — a position on every selector
// axis, and one band or three — as an image. The stack stays resident while it is open; the image shown is a
// store of tiles copied from the selected planes, so every number the window
// states is of the selection shown, at full resolution, as it is of an image
// opened by itself.

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	"github.com/stergiotis/boxer/public/science/imaging/display"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	selectKind  = "imagescope.select"
	selectTitle = "take a selection of a stack"
)

// selection is which image of a stack is shown: a position on every axis,
// the band axis's entry unused, and the bands — nil for the image as stored
// (a stack whose bands are a colour model) or for a stack without a band
// axis, otherwise one band or three, red, green and blue. A selection taken
// for a composite (ADR-0401 §M2) has composite set, and its bands are the
// image's planes: the bands its layers show, each once, the first repeated
// when there are two (see compositePlanes). entry is the file's entry the
// stack is read from (ADR-0398 §M5), empty for a stack that is no file's: a
// selection asking for another entry asks for that entry's stack, whose
// default selection is shown.
type selection struct {
	at        []int
	bands     []int
	composite bool
	entry     string
}

func (inst selection) clone() selection {
	out := selection{at: slices.Clone(inst.at), composite: inst.composite, entry: inst.entry}
	if inst.bands != nil {
		out.bands = slices.Clone(inst.bands)
	}
	return out
}

func (inst selection) equal(o selection) bool {
	return slices.Equal(inst.at, o.at) && slices.Equal(inst.bands, o.bands) && (inst.bands == nil) == (o.bands == nil) &&
		inst.composite == o.composite && inst.entry == o.entry
}

// key names the selection in a job's key.
func (inst selection) key() string {
	var b strings.Builder
	b.WriteString(strconv.Quote(inst.entry))
	for _, a := range inst.at {
		b.WriteString(strconv.Itoa(a))
		b.WriteByte(',')
	}
	b.WriteByte('|')
	if inst.bands == nil {
		b.WriteString("stored")
	}
	if inst.composite {
		b.WriteString("composite:")
	}
	for _, x := range inst.bands {
		b.WriteString(strconv.Itoa(x))
		b.WriteByte(',')
	}
	return b.String()
}

// stackView is a stack open in the window and the selection the store shown
// was taken at. It is never modified once made: a selection that lands
// makes a new one, so a snapshot that holds one reads a consistent pair.
type stackView struct {
	stack imaging.Stack
	// axes are the stack's axes, copied once: Stack.Axes copies them on
	// every call.
	axes []imaging.Axis
	// band is the band axis's index, -1 when there is none.
	band int
	// gen tells this stack apart from the one before it in a job's key.
	gen   uint64
	shown selection
}

func newStackView(st imaging.Stack, shown selection) *stackView {
	out := &stackView{stack: st, axes: st.Axes(), band: -1, shown: shown}
	if b, ok := st.BandAxis(); ok {
		out.band = b
	}
	return out
}

// withShown is the same stack with another selection shown.
func (inst *stackView) withShown(sel selection) *stackView {
	out := *inst
	out.shown = sel.clone()
	return &out
}

// axisIndex is the index of the axis named name, -1 when there is none.
func (inst *stackView) axisIndex(name string) int {
	return slices.IndexFunc(inst.axes, func(a imaging.Axis) bool { return a.Name == name })
}

// selectorNames are the names of the selector axes, in the stack's order.
func (inst *stackView) selectorNames() []string {
	out := make([]string, 0, len(inst.axes))
	for _, a := range inst.axes {
		if a.Role == imaging.AxisRoleSelector {
			out = append(out, a.Name)
		}
	}
	return out
}

// label is what position pos of axis i is called: its label, or its number.
func (inst *stackView) label(i, pos int) string { return inst.stack.Label(i, pos) }

// positionText says where a selector axis stands: the position's label, its
// coordinate with the axis's unit where it has one, and which of how many
// positions it is.
func (inst *stackView) positionText(i, pos int) string {
	a := inst.axes[i]
	s := inst.label(i, pos)
	if a.Coords != nil {
		s += " · " + coordText(a.Coords[pos], a.Unit)
	}
	return s + " · " + strconv.Itoa(pos+1) + " of " + strconv.Itoa(a.Size)
}

// coordText writes an axis coordinate with its unit.
func coordText(v float64, unit string) string {
	s := strconv.FormatFloat(v, 'g', 7, 64)
	if unit != "" {
		s += " " + unit
	}
	return s
}

// describe says in one line which selection sel is: the selector axes'
// positions, then the bands.
func (inst *stackView) describe(sel selection) string {
	parts := make([]string, 0, len(inst.axes)+1)
	for i, a := range inst.axes {
		if a.Role == imaging.AxisRoleSelector && i < len(sel.at) {
			parts = append(parts, a.Name+" "+inst.label(i, sel.at[i]))
		}
	}
	switch {
	case inst.band < 0:
	case sel.composite:
		names := make([]string, 0, len(sel.bands))
		for _, b := range sel.bands {
			if l := inst.label(inst.band, b); !slices.Contains(names, l) {
				names = append(names, l)
			}
		}
		parts = append(parts, inst.axes[inst.band].Name+"s "+strings.Join(names, ", ")+" for a composite")
	case sel.bands == nil:
		parts = append(parts, "as stored")
	case len(sel.bands) == 1:
		parts = append(parts, inst.axes[inst.band].Name+" "+inst.label(inst.band, sel.bands[0]))
	default:
		names := make([]string, len(sel.bands))
		for k, b := range sel.bands {
			names[k] = inst.label(inst.band, b)
		}
		parts = append(parts, inst.axes[inst.band].Name+"s "+strings.Join(names, ", ")+" as red, green, blue")
	}
	return strings.Join(parts, " · ")
}

// defaultSelection is what a stack opens on: position 0 on every axis, and
// the image as stored when the stack's bands are a colour model, otherwise
// the first band.
func defaultSelection(st imaging.Stack) selection {
	sel := selection{at: make([]int, len(st.Axes()))}
	if _, ok := st.BandAxis(); ok && st.Model() == 0 {
		sel.bands = []int{0}
	}
	return sel
}

// imageOf takes a selection's image from the stack; it shares the stack's
// planes.
func imageOf(st imaging.Stack, sel selection) (img imaging.Image, err error) {
	if sel.composite {
		return compositeImage(st, sel)
	}
	if sel.bands == nil {
		if st.Model() != 0 {
			img, _, err = st.AsStored(sel.at)
			return
		}
		return st.Image(sel.at, nil)
	}
	return st.Image(sel.at, sel.bands)
}

// storeOf takes a selection into a store of tiles: the selected planes,
// copied, with their reduced levels. n is the plane's pixels times the
// planes taken, one or three or as many as the colour model has: time and
// memory are linear in it, which for a selection is what opening an image of
// that size costs. Nothing is kept of a selection once another is shown.
func storeOf(ctx context.Context, st imaging.Stack, sel selection, report bgjob.Reporter) (*tiles.Store, error) {
	img, err := imageOf(st, sel)
	if err != nil {
		return nil, err
	}
	report(0, uint64(img.Height()), "copying the selection")
	return tiles.FromImage(ctx, img, tiles.Options{Progress: func(done, total int64) {
		report(uint64(done), uint64(total), "copying the selection")
	}})
}

// selected is what taking a selection leaves; stk is the stack of another
// entry of the file, when the selection asked for one.
type selected struct {
	pic *tiles.Store
	sel selection
	stk *stackView
}

// demandSelection keeps the store shown on the selection asked for: when
// the two differ the selection is taken off the frame (W2), a newer one
// superseding a run in flight, and the store shown is replaced when it
// lands. A selection that fails — or that the person cancels — is dropped
// and the one shown stays, the reason in the status row.
func (inst *App) demandSelection() {
	stk := inst.stk
	if stk == nil || inst.want.equal(stk.shown) {
		inst.selBusy = false
		return
	}
	want := inst.want.clone()
	key := strconv.FormatUint(stk.gen, 10) + "|" + want.key()
	job := func(ctx context.Context, report bgjob.Reporter) (*selected, error) {
		pic, err := storeOf(ctx, stk.stack, want, report)
		if err != nil {
			return nil, err
		}
		return &selected{pic: pic, sel: want}, nil
	}
	if file := inst.file; want.entry != stk.shown.entry && file.switchable() {
		job = func(ctx context.Context, report bgjob.Reporter) (*selected, error) {
			return readEntry(ctx, file, want.entry, report)
		}
	}
	val, done, err, busy := inst.selectJob.DemandReporting(key, job)
	inst.selBusy = busy || !done
	if inst.selBusy {
		return
	}
	inst.selectJob.Invalidate()
	inst.selBusy = false
	if err != nil || val == nil {
		reason := "the selection was cancelled"
		switch {
		case err == nil || errorsIsCancelled(err):
		case want.entry != stk.shown.entry:
			reason = "the entry " + strconv.Quote(want.entry) + " could not be read: " + refusalText(err)
			inst.selectRefusal = decode.RefusalOf(err)
		default:
			reason = "the selection could not be taken: " + refusalText(err)
		}
		inst.selectErr = reason
		inst.compAsk = nil
		inst.want = stk.shown.clone()
		inst.syncSliders()
		return
	}
	inst.showSelection(val)
}

// errorsIsCancelled reports a run that ended because it was cancelled.
func errorsIsCancelled(err error) bool {
	return errors.Is(err, bgjob.ErrCancelled) || errors.Is(err, context.Canceled)
}

// viewKind is what is shown and how, kept across a selection change: of a
// composite, its layers by band.
type viewKind struct {
	model  imaging.ColorModelE
	plane  int
	single bool
	recipe display.Recipe
	region image.Rectangle
	comp   *CompositeArgs
}

func (inst *App) viewKindNow() (k viewKind) {
	k.model = inst.pic.Model()
	k.plane, k.single = inst.viewer.Plane()
	k.recipe = inst.viewer.Recipe()
	k.region, _ = inst.viewer.Region()
	if args, ok := inst.compositeArgsNow(); ok {
		k.comp = &args
	}
	return
}

// name says what kind of view k is, for a person.
func (inst viewKind) name() string {
	if inst.comp != nil {
		return "a composite"
	}
	if inst.single || !inst.model.IsColor() {
		return "a single plane"
	}
	return "a colour image"
}

// showSelection replaces the store shown with a selection's, keeping the
// view, the marked region and — when the view is of the same kind — the
// plane and the recipe; limits taken from the data are taken again from the
// new samples, and the status row says so.
func (inst *App) showSelection(res *selected) {
	if res.stk != nil {
		// Another entry of the file: it replaces the image opened as an
		// open does, fitted under its default recipe, the regions and the
		// results kept.
		inst.stackGen++
		res.stk.gen = inst.stackGen
		inst.setShown(res.pic, res.stk)
		inst.file = inst.file.withShown(res.sel.entry, res.stk.stack.Provenance().Variant)
		if i := inst.file.index(res.sel.entry); i >= 0 {
			inst.selectNote = "the entry " + inst.file.entries[i].Title + " is shown, fitted under its default recipe, as an open shows an image"
		}
		return
	}
	old := inst.viewKindNow()
	if inst.shownId != openedId {
		// A result is shown: the stack's new selection is shown in its
		// place, under its own default view.
		inst.adopt(res.pic, true)
		inst.setOpenedStore(res.pic)
		inst.shownId = openedId
		inst.stk = inst.stk.withShown(res.sel)
		inst.selectNote = "the selection of the stack opened is shown in place of the result that was"
		return
	}
	inst.adopt(res.pic, true)
	inst.setOpenedStore(res.pic)
	inst.stk = inst.stk.withShown(res.sel)
	if inst.takeCompositeAsk() {
		inst.viewer.SetRegion(old.region)
		return
	}
	inst.selectNote = inst.carryView(old)
}

// carryView puts back what a selection change keeps of the view before it,
// and says what it could not keep.
func (inst *App) carryView(old viewKind) (note string) {
	defer inst.syncChecks()
	inst.viewer.SetRegion(old.region)
	if old.comp != nil {
		if stk := inst.compositeStack(); stk != nil && (stk.shown.composite || stk.shown.bands == nil) && holdsBands(stk, stk.shown, layerBands(*old.comp)) {
			return inst.carryComposite(*old.comp)
		}
	}
	now := inst.viewKindNow()
	if old.model != now.model || old.comp != nil {
		if pal := old.recipe.Palette; pal != "" && inst.viewer.IsSinglePlane() {
			r := inst.viewer.Recipe()
			r.Palette = pal
			_ = inst.viewer.SetRecipe(r)
		}
		note = "the view is " + now.name() + " where it was " + old.name() + ": it opens under that view's default recipe"
		if extra := inst.threeBandDefault(); extra != "" {
			note += "; " + extra
		}
		return note
	}
	if old.single {
		if err := inst.viewer.SetPlane(old.plane); err != nil {
			return "the plane shown could not be kept: " + refusalText(err)
		}
	}
	r := old.recipe
	i := max(old.plane, 0)
	label := inst.pic.PlaneLabels()[i]
	t := inst.pic.Transform(i)
	switch r.Limits.Source {
	case display.LimitsSourceData:
		lo, hi, ok := inst.pic.StoredRange(i)
		if a, b := t.Apply(lo), t.Apply(hi); ok && a != b {
			r.Limits = display.Limits{Lo: min(a, b), Hi: max(a, b), Source: display.LimitsSourceData}
			note = "limits taken again from the data: the range of " + label + ", " + formatFigure(r.Limits.Lo) + " to " + formatFigure(r.Limits.Hi)
		} else {
			r.Limits = inst.viewer.Recipe().Limits
			note = "limits: " + label + " has no range of valid samples, so its default limits are shown"
		}
	case display.LimitsSourceNominal:
		if l, ok := display.NominalLimits(inst.pic.Kind(), t); ok {
			r.Limits = l
		}
	case display.LimitsSourcePercentile:
		note = fmt.Sprintf("limits taken again from the data: the %g–%g %% percentiles of %s, once its histogram is counted", r.Limits.PercentLo, r.Limits.PercentHi, label)
	case display.LimitsSourceZScale:
		note = "limits taken again from the data: zscale of " + label + ", once it is sampled"
	}
	if err := inst.viewer.SetRecipe(r); err != nil {
		return "the recipe could not be kept (" + refusalText(err) + "); the view's default recipe is shown"
	}
	return note
}

// errNoStack is the refusal of a stack operation when the image open is
// none's selection.
var errNoStack = app.RefuseOperation(noStackReason)

// noStackReason says where a stack comes from.
const noStackReason = "the image open is not a stack's: open_sample with bands opens one, or the person opens a file with Open… whose format holds stacks — list_formats lists what this host reads"

// setWant asks for a selection: the store shown follows it off the frame.
// A composite waiting for another selection is dropped.
func (inst *App) setWant(sel selection) {
	inst.want = sel
	inst.compAsk = nil
	inst.selectErr, inst.selectNote, inst.selectRefusal = "", "", 0
	inst.syncSliders()
}

// setPosition sets the position on a selector axis, by the axis's name.
// The same position twice asks for nothing more.
func (inst *App) setPosition(axis string, pos int) error {
	if inst.pic == nil {
		return errNoImage
	}
	stk := inst.stk
	if stk == nil {
		return errNoStack
	}
	if inst.entryPending() {
		return errEntryPending
	}
	i := stk.axisIndex(axis)
	switch {
	case i < 0:
		return app.RefuseOperation("no axis named " + strconv.Quote(axis) + "; the selector axes are " + quotedList(stk.selectorNames()))
	case stk.axes[i].Role == imaging.AxisRoleBand:
		return app.RefuseOperation(strconv.Quote(axis) + " is the band axis; set_bands chooses what is shown of it")
	case pos < 0 || pos >= stk.axes[i].Size:
		return app.RefuseOperation(fmt.Sprintf("position %d is outside %q, whose positions are 0 to %d", pos, axis, stk.axes[i].Size-1))
	}
	want := inst.want.clone()
	want.at[i] = pos
	inst.setWant(want)
	return nil
}

// quotedList writes names as a list of quoted names; "none" when empty.
func quotedList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = strconv.Quote(n)
	}
	return strings.Join(q, ", ")
}

// setBands sets the bands shown: one, three as red, green and blue, or none
// for the image as stored. It sets, never toggles.
func (inst *App) setBands(bands []int) error {
	if inst.pic == nil {
		return errNoImage
	}
	stk := inst.stk
	if stk == nil {
		return errNoStack
	}
	if inst.entryPending() {
		return errEntryPending
	}
	if stk.band < 0 {
		return app.RefuseOperation("this stack has no band axis: each of its planes is a position of a selector axis, which set_position chooses")
	}
	a := stk.axes[stk.band]
	want := inst.want.clone()
	want.composite = false
	switch len(bands) {
	case 0:
		if stk.stack.Model() == 0 {
			return app.RefuseOperation("this stack's bands are no colour model, so there is no image as stored; name one band, or three for red, green and blue")
		}
		want.bands = nil
		if want.equal(stk.shown) && !inst.selectionPending() && inst.viewer.IsComposite() {
			// The selection shown is the image as stored, under a composite:
			// show it as stored (a colour image of a binned kind has no other
			// colour view than its composite, which stays).
			inst.setWant(want)
			err := refuse(inst.viewer.SetAsStored())
			inst.showColourComposite()
			inst.syncChecks()
			return err
		}
	case 1, 3:
		for _, b := range bands {
			if b < 0 || b >= a.Size {
				return app.RefuseOperation(fmt.Sprintf("band %d is outside %q, whose positions are 0 to %d", b, a.Name, a.Size-1))
			}
		}
		if len(bands) == 3 {
			inst.lastTriple = slices.Clone(bands)
			if !hasCodes(stk.stack.Kind()) {
				return inst.binnedTriple(bands)
			}
		}
		want.bands = slices.Clone(bands)
	default:
		return app.RefuseOperation(fmt.Sprintf("%d bands: name one band, three for red, green and blue, or none for the image as stored", len(bands)))
	}
	inst.setWant(want)
	return nil
}

// defaultTriple is the three bands a switch to three bands starts from: the
// last three chosen of this stack, else its first three.
func (inst *App) defaultTriple() []int {
	if len(inst.lastTriple) == 3 {
		return slices.Clone(inst.lastTriple)
	}
	n := inst.stk.axes[inst.stk.band].Size
	return []int{0, min(1, n-1), min(2, n-1)}
}

// syncSliders sets the position sliders to the selection asked for, after
// anything but them changed it.
func (inst *App) syncSliders() {
	if inst.stk == nil {
		inst.posSlider = nil
		return
	}
	if len(inst.posSlider) != len(inst.want.at) {
		inst.posSlider = make([]float64, len(inst.want.at))
	}
	for i, p := range inst.want.at {
		inst.posSlider[i] = float64(p)
	}
}

// Hover help of the stack controls (keelson-app A5).
const (
	tipAxisPrev     = "The previous position on this axis. Each position is a plane of the stack; the image shown is taken from it, and the readout, the histogram and every statistic follow."
	tipAxisNext     = "The next position on this axis. Each position is a plane of the stack; the image shown is taken from it, and the readout, the histogram and every statistic follow."
	tipAxisSlider   = "The position on this axis. The view, the marked region and the recipe stay; limits taken from the data are taken again from the new plane."
	tipBandsStored  = "The image as the file stores it: its bands are a colour model, and this is that colour image."
	tipBandsOne     = "One band by itself, through a colour map."
	tipBandsThree   = "Three bands as red, green and blue under one shared recipe, over the range of their data together. Signed 32-bit and float bands are shown as the linked composite of the three in red, green and blue, which is the same picture."
	tipBandPick     = "Which band. A band may be chosen more than once."
	maxComboEntries = 64
)

// renderStackControls draws the controls of a stack: a stepper and a slider
// per selector axis, and the band choice. They show the selection asked
// for; the image follows when it is taken.
func (inst *App) renderStackControls() {
	stk := inst.stk
	if stk == nil {
		return
	}
	for i, a := range stk.axes {
		if a.Role != imaging.AxisRoleSelector || i >= len(inst.want.at) {
			continue
		}
		for range c.Horizontal().KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("axis-" + a.Name)) {
				inst.renderAxisRow(stk, i)
			}
		}
	}
	if stk.band >= 0 {
		for range c.Horizontal().KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("bands")) {
				inst.renderBandsRow(stk)
			}
		}
	}
}

// renderAxisRow is one selector axis: the stepper around a slider that
// carries the axis's name, and where it stands. An axis of one position has
// no slider, and its name is a label.
func (inst *App) renderAxisRow(stk *stackView, i int) {
	a := stk.axes[i]
	pos := inst.want.at[i]
	if a.Size <= 1 || i >= len(inst.posSlider) {
		c.Label(a.Name).Selectable(false).Send()
	}
	if inst.button("prev", "‹", tipAxisPrev, false) && pos > 0 {
		inst.positionGesture(a.Name, pos-1)
	}
	if a.Size > 1 && i < len(inst.posSlider) {
		// A slider moved since the last frame shows as a bound value that
		// differs from the selection (the check boxes' pattern).
		if v := int(math.Round(inst.posSlider[i])); v != pos {
			inst.positionGesture(a.Name, min(max(v, 0), a.Size-1))
		}
		for range c.HoverText(tipAxisSlider).KeepIter() {
			c.SliderF64(inst.ids.PrepareStr("slider"), inst.posSlider[i], 0, float64(a.Size-1)).Integer().ShowValue(false).
				Text(a.Name).SendRespVal(&inst.posSlider[i])
		}
	}
	if inst.button("next", "›", tipAxisNext, false) && pos < a.Size-1 {
		inst.positionGesture(a.Name, pos+1)
	}
	c.Label(stk.positionText(i, inst.want.at[i])).Send()
}

// renderBandsRow is the band choice: as stored, one band or three, and the
// pickers of the bands chosen.
func (inst *App) renderBandsRow(stk *stackView) {
	a := stk.axes[stk.band]
	bands := inst.want.bands
	composite := inst.want.composite || inst.compAsk != nil || inst.viewer.IsComposite()
	c.Label("Bands").Selectable(false).Send()
	if stk.stack.Model() != 0 && inst.button("stored", "as stored", tipBandsStored, bands == nil && !composite) {
		inst.bandsGesture(nil)
	}
	if inst.button("one", "one band", tipBandsOne, len(bands) == 1 && !composite) && (len(bands) != 1 || composite) {
		first := 0
		if len(bands) > 0 {
			first = bands[0]
		}
		inst.bandsGesture([]int{first})
	}
	if inst.button("three", "three bands", tipBandsThree, len(bands) == 3 && !composite) && (len(bands) != 3 || composite) {
		inst.bandsGesture(inst.defaultTriple())
	}
	if inst.button("composite", "composite", tipComposite, composite) && !composite {
		inst.compositeGesture(inst.defaultComposite())
	}
	if composite {
		return
	}
	switch len(bands) {
	case 1:
		b := bands[0]
		if inst.button("prev", "‹", tipBandPick, false) && b > 0 {
			inst.bandsGesture([]int{b - 1})
		}
		inst.bandCombo(stk, "band", b, func(x int) { inst.bandsGesture([]int{x}) })
		if inst.button("next", "›", tipBandPick, false) && b < a.Size-1 {
			inst.bandsGesture([]int{b + 1})
		}
		if a.Coords != nil {
			c.Label(coordText(a.Coords[b], a.Unit)).Send()
		}
	case 3:
		for k, ch := range []string{"R", "G", "B"} {
			c.Label(ch).Selectable(false).Send()
			inst.bandCombo(stk, ch, bands[k], func(x int) {
				next := slices.Clone(bands)
				next[k] = x
				inst.bandsGesture(next)
			})
		}
	}
}

// bandCombo draws a picker of one band: a combo box listing the band axis's
// positions — at most maxComboEntries of them, around the one chosen, so a
// frame costs the same for any number of bands. The label before it names
// it; the combo box's own would sit after it.
func (inst *App) bandCombo(stk *stackView, key string, current int, choose func(b int)) {
	a := stk.axes[stk.band]
	from := max(0, min(current-maxComboEntries/2, a.Size-maxComboEntries))
	to := min(a.Size, from+maxComboEntries)
	for range c.HoverText(tipBandPick).KeepIter() {
		for range c.ComboBox(inst.ids.PrepareStr(key), c.WidgetText().Text("").Keep(),
			c.WidgetText().Text(stk.label(stk.band, current)).Keep()).KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr(key + "-items")) {
				for b := from; b < to; b++ {
					if c.Button(inst.ids.PrepareSeq(uint64(b)), c.Atoms().Text(stk.label(stk.band, b)).Keep()).
						Selected(b == current).SendResp().HasPrimaryClicked() && b != current {
						choose(b)
					}
				}
			}
		}
	}
}

// positionGesture is the person's set_position.
func (inst *App) positionGesture(axis string, pos int) {
	args := SetPositionArgs{Axis: axis, Position: pos}
	inst.viewGesture(opSetPosition, args, func() error { return inst.setPosition(axis, pos) })
	inst.syncSliders()
}

// bandsGesture is the person's set_bands.
func (inst *App) bandsGesture(bands []int) {
	args := SetBandsArgs{Bands: bands}
	inst.viewGesture(opSetBands, args, func() error { return inst.setBands(bands) })
}
