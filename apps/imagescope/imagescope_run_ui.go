package imagescope

// The Operations section and the Images row (ADR-0400 §M2): a panel built
// from the operators' declarations — no operator is named here — with a
// field per parameter by its type, a row per input, the plan before the
// run and the record after it.

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/science/imaging"
	imgops "github.com/stergiotis/boxer/public/science/imaging/ops"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/bgjobrow"
)

// Hover help of the operations panel.
const (
	tipImages      = "The images of this window: the one opened and the results of operations. Showing one of the same size keeps the view and the marked region; the regions apply to every image of their frame."
	tipDropImage   = "Drop the result shown and free what it holds. Nothing of it is kept elsewhere."
	tipRun         = "Run the operation once at full resolution, as a task: its result is shown as it fills and joins the images. Nothing is computed at a reduced level."
	tipPlan        = "What the operation would make, stated before a sample is read."
	tipExpression  = expressionGrammar
	tipFillAxis    = "Fill the inputs with every position of this axis for the band shown: what a statistic over time takes."
	tipInputPlane  = "One plane of this image as the input, or every plane: an operator taking one plane is given the plane named."
	tipSaveRecord  = "Write the result's record, the versioned JSON document of what its operation did, to a file you pick."
	tipRecord      = "What the operation did: the operator and its version, the parameters as resolved, each input by the digest of its samples and where on a stack it was taken, what its first passes found and the result's digest. A result made from results carries their records."
	tipInputName   = "The input's name, as the expression reads it."
	maxInputRows   = 64
	paramTextWidth = 90
)

// panelState is what the Operations section holds while it is filled in.
type panelState struct {
	op string
	// params holds each parameter's text where its field binds it: the
	// field writes the text it holds at the end of the frame, so the
	// address must outlive the frame.
	params  map[string]*string
	inputs  []RunInputArgs
	name    string
	planKey string
	plan    string
	planErr string
	// names are the inputs' names as the run would give them.
	names []string
	// change is a change to the rows of inputs a button asked for, made
	// before the rows are drawn on the next frame: the rows' fields bind
	// their texts by address until the frame ends.
	change func()
	// exprFor and exprErr memoise the expression field's parse.
	exprFor string
	exprErr string
}

// param is a parameter's text as its field holds it.
func (inst *panelState) param(name string) string {
	if p := inst.params[name]; p != nil {
		return *p
	}
	return ""
}

// setParam sets a parameter's text.
func (inst *panelState) setParam(name, text string) {
	if inst.params == nil {
		inst.params = make(map[string]*string, 4)
	}
	if p := inst.params[name]; p != nil {
		*p = text
		return
	}
	inst.params[name] = &text
}

// panelArgs is the section as run_operation's arguments.
func (inst *App) panelArgs() RunArgs {
	p := &inst.panel
	args := RunArgs{Operator: p.op, Inputs: slices.Clone(p.inputs), Name: p.name}
	if op, ok := imgops.Lookup(p.op); ok {
		for _, q := range op.Declaration().Params {
			args.Params = append(args.Params, ParamArg{Name: q.Name, Value: p.param(q.Name)})
		}
	}
	return args
}

// chooseOperator fills the section for an operator: each parameter at its
// default as text — a required one empty — and the fewest inputs it takes,
// each naming the image shown and, for the stack, a band, so that a result
// shown later does not change what a row takes.
func (inst *App) chooseOperator(id string) {
	op, ok := imgops.Lookup(id)
	if !ok {
		return
	}
	d := op.Declaration()
	inst.panel = panelState{op: id, params: make(map[string]*string, len(d.Params))}
	for _, q := range d.Params {
		inst.panel.setParam(q.Name, paramText(q.Default))
	}
	for range d.MinInputs {
		inst.panel.inputs = append(inst.panel.inputs, inst.nextInput())
	}
}

// fillOverAxis makes the inputs every position of a selector axis for
// the band shown, one row each.
func (inst *App) fillOverAxis(axis int) {
	stk := inst.stk
	band := ""
	if stk.band >= 0 && len(stk.shown.bands) == 1 {
		band = stk.label(stk.band, stk.shown.bands[0])
	}
	a := stk.axes[axis]
	inst.panel.inputs = inst.panel.inputs[:0]
	for p := range min(a.Size, maxInputRows) {
		inst.panel.inputs = append(inst.panel.inputs, RunInputArgs{Name: inputName(a.Name + "_" + strconv.Itoa(p)), Image: openedId, Band: band,
			Positions: []AnchorPosition{{Axis: a.Name, Position: p}}})
	}
}

// renderImagesRow is the Images row: a button per image of the window,
// and Drop for a result shown.
func (inst *App) renderImagesRow() {
	if len(inst.images) < 2 && inst.shownId != runningId {
		return
	}
	for range c.Horizontal().KeepIter() {
		c.Label("Images").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("images")) {
			for _, e := range inst.images {
				if inst.button(e.id, e.id, describeEntry(e), e.id == inst.shownId) && e.id != inst.shownId {
					id := e.id
					inst.viewGesture(opShowImage, ImageIdArgs{Id: id}, func() error { return inst.showImage(id) })
				}
			}
		}
		if e, ok := entryOf(inst.images, inst.shownId); ok && e.isResult() && inst.button("drop-image", "Drop", tipDropImage, false) {
			id := e.id
			inst.viewGesture(opDropImage, ImageIdArgs{Id: id}, func() error { return inst.dropImage(id) })
		}
		metric("results hold "+formatBytes(heldResultBytes(inst.images))+" of "+formatBytes(maxResultBytes), tipImages)
	}
	if e, ok := entryOf(inst.images, inst.shownId); ok && e.record != nil {
		inst.renderRecord(e)
	}
}

// renderRecord states a result's record, readably, with its JSON a button
// away.
func (inst *App) renderRecord(e imageEntry) {
	sum := recordSummaryOf(*e.record)
	for range c.HoverText(tipRecord).KeepIter() {
		for range c.CollapsingHeader(inst.ids.PrepareStr("record"), c.WidgetText().Text("Record of "+e.id+": "+sum.Operator+" · digest "+sum.Digest+"…").Keep()).KeepIter() {
			c.Label("parameters: " + strings.Join(sum.Params, ", ")).Send()
			for k, in := range sum.Inputs {
				s := "input " + in.Name + ": samples " + in.Digest + "…"
				if len(in.Positions) > 0 {
					s += " at " + strings.Join(in.Positions, ", ")
				}
				if in.Result != "" {
					s += " — a result of " + in.Result + ", whose record it carries"
				}
				// The whole digest is the hover, the short one the line.
				metric(s, "BLAKE3 digest of the input's samples: "+e.record.Inputs[k].Digest.String())
			}
			for _, q := range sum.Quantities {
				c.Label("found: " + q).Send()
			}
			for _, n := range sum.Notes {
				c.Label(n).Send()
			}
			c.Label("result digest " + e.record.Output.Digest.String()).Send()
			if inst.button("save-record", "Save record…", tipSaveRecord, false) {
				if doc, err := e.record.Encode(); err != nil {
					inst.runErr = refusalText(err)
				} else if err = inst.saveDocument(e.id+"-record.json", doc, &inst.recordOut); err != nil {
					inst.runErr = refusalText(err)
				}
			}
			if busy, note, errText := inst.recordOut.state(); busy || note != "" || errText != "" {
				c.Label(strings.TrimSpace(note + " " + errText)).Send()
			}
		}
	}
}

// renderOperationsPane is the Operations section.
func (inst *App) renderOperationsPane() {
	if inst.panel.op == "" {
		inst.chooseOperator("expr")
	}
	for range c.Horizontal().KeepIter() {
		c.Label("Operator").Selectable(false).Send()
		for range c.IdScope(inst.ids.PrepareStr("operator")) {
			for op := range imgops.All() {
				d := op.Declaration()
				if inst.button(d.Id, d.Id, d.Summary, inst.panel.op == d.Id) && inst.panel.op != d.Id {
					inst.chooseOperator(d.Id)
				}
			}
		}
	}
	op, ok := imgops.Lookup(inst.panel.op)
	if !ok {
		return
	}
	d := op.Declaration()
	c.Label(d.Summary).Send()
	for range c.CollapsingHeader(inst.ids.PrepareStr("operator-help"), c.WidgetText().Text("Help — "+d.Ref()).Keep()).KeepIter() {
		c.Label(d.Help()).Send()
		c.Label(valueRules).Send()
	}
	inst.renderInputRows(d)
	for range c.IdScope(inst.ids.PrepareStr("params")) {
		for _, q := range d.Params {
			inst.renderParamField(d, q)
		}
	}
	for range c.Horizontal().KeepIter() {
		c.TextEdit(inst.ids.PrepareStr("result-name"), inst.panel.name, false).AccessibleName("Result name").HintText("result name (optional)").DesiredWidth(200).SendRespVal(&inst.panel.name)
	}
	reason := inst.refreshPlan()
	if reason == "" {
		metric("Plan: "+inst.panel.plan, tipPlan)
	} else {
		badge.New(inst.ids.PrepareStr("plan-refused"), "cannot run: "+reason).Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	}
	for range c.Horizontal().KeepIter() {
		running := inst.runJob.Running()
		for range c.EnabledUi(reason == "" && !running).KeepIter() {
			for range c.HoverText(tipRun).KeepIter() {
				if c.Button(inst.ids.PrepareStr("run"), c.Atoms().Text("Run").Keep()).Kind(c.ButtonKindPrimary).SendResp().HasPrimaryClicked() {
					args := inst.panelArgs()
					gestureOr(inst, opRunOperation, args, func() { inst.runErr = refusalText(inst.startRun(args)) })
				}
			}
		}
		if running {
			bgjobrow.Render(bgjobrow.Input{Ids: inst.ids, ScopeKey: "run-row", Job: &inst.runJob, Note: "running…", Cancel: true, Inline: true})
			c.RequestRepaint()
		}
	}
	switch {
	case inst.runErr != "":
		badge.New(inst.ids.PrepareStr("run-err"), inst.runErr).Tone(badge.ToneError).Variant(badge.VariantSoft).Send()
	case inst.runNote != "" && !inst.runJob.Running():
		c.Label(inst.runNote).Send()
	}
}

// renderInputRows draws a row per input: its name, its image and, for the
// stack opened, its band and positions; and the ways to add one, to remove
// one, and to fill them over an axis.
func (inst *App) renderInputRows(d imgops.Declaration) {
	if f := inst.panel.change; f != nil {
		inst.panel.change = nil
		f()
	}
	c.Label(d.InputsDoc).Send()
	remove := -1
	for k := range inst.panel.inputs {
		for range c.IdScope(inst.ids.PrepareSeq(uint64(k))) {
			for range c.Horizontal().KeepIter() {
				in := &inst.panel.inputs[k]
				for range c.HoverText(tipInputName).KeepIter() {
					c.TextEdit(inst.ids.PrepareStr("name"), in.Name, false).AccessibleName("Input " + strconv.Itoa(k+1) + " name").HintText("name").DesiredWidth(70).SendRespVal(&in.Name)
				}
				inst.imageCombo(in)
				if inst.inputIsStack(in) {
					inst.bandPicker(in)
					inst.positionPickers(in)
				} else {
					inst.planePicker(in)
				}
				if len(inst.panel.inputs) > d.MinInputs && inst.button("remove", "−", "Remove this input.", false) {
					remove = k
				}
			}
		}
	}
	if remove >= 0 {
		inst.panel.change = func() { inst.panel.inputs = slices.Delete(inst.panel.inputs, remove, remove+1) }
	}
	for range c.Horizontal().KeepIter() {
		if (d.MaxInputs == 0 || len(inst.panel.inputs) < d.MaxInputs) && len(inst.panel.inputs) < maxInputRows &&
			inst.button("add-input", "Add input", "Add an input: for a stack, its next band not taken yet; otherwise the image shown.", false) {
			inst.panel.change = func() { inst.panel.inputs = append(inst.panel.inputs, inst.nextInput()) }
		}
		if stk := inst.stk; stk != nil && d.MaxInputs == 0 {
			for i, a := range stk.axes {
				if a.Role == imaging.AxisRoleSelector && inst.button("fill-"+a.Name, "every "+a.Name, tipFillAxis, false) {
					inst.panel.change = func() { inst.fillOverAxis(i) }
				}
			}
		}
	}
}

// nextInput is the input a new row takes: for a stack shown with a band
// axis, the band shown if no row takes it yet, else its first band no row
// takes — so two rows make a ratio of two bands without picking; otherwise
// the image shown, by its id.
func (inst *App) nextInput() RunInputArgs {
	stk := inst.stackShown()
	if stk == nil || stk.band < 0 {
		if inst.shownId == "" || inst.shownId == runningId {
			return RunInputArgs{}
		}
		return RunInputArgs{Image: inst.shownId}
	}
	taken := make(map[string]bool, len(inst.panel.inputs))
	for _, in := range inst.panel.inputs {
		b := in.Band
		if b == "" && (in.Image == "" || in.Image == openedId) && len(stk.shown.bands) == 1 {
			b = stk.label(stk.band, stk.shown.bands[0])
		}
		taken[b] = true
	}
	order := make([]int, 0, stk.axes[stk.band].Size+1)
	if len(stk.shown.bands) == 1 {
		order = append(order, stk.shown.bands[0])
	}
	for b := range stk.axes[stk.band].Size {
		order = append(order, b)
	}
	for _, b := range order {
		if l := stk.label(stk.band, b); !taken[l] {
			return RunInputArgs{Image: openedId, Band: l}
		}
	}
	return RunInputArgs{Image: openedId}
}

// inputIsStack reports an input taken from the stack opened.
func (inst *App) inputIsStack(in *RunInputArgs) bool {
	id := in.Image
	if id == "" {
		id = inst.shownId
	}
	return inst.stk != nil && (id == openedId || id == "")
}

// imageCombo picks an input's image from the list.
func (inst *App) imageCombo(in *RunInputArgs) {
	current := in.Image
	if current == "" {
		current = "shown"
	}
	for range c.ComboBox(inst.ids.PrepareStr("image"), c.WidgetText().Text("").Keep(), c.WidgetText().Text(current).Keep()).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("image-items")) {
			if c.Button(inst.ids.PrepareStr("shown"), c.Atoms().Text("shown").Keep()).Selected(in.Image == "").SendResp().HasPrimaryClicked() {
				in.Image = ""
			}
			for _, e := range inst.images {
				if c.Button(inst.ids.PrepareStr(e.id), c.Atoms().Text(e.id).Keep()).Selected(in.Image == e.id).SendResp().HasPrimaryClicked() {
					in.Image = e.id
				}
			}
		}
	}
}

// bandPicker picks an input's band of the stack opened.
func (inst *App) bandPicker(in *RunInputArgs) {
	stk := inst.stk
	if stk.band < 0 {
		return
	}
	current := in.Band
	if current == "" {
		current = "band shown"
	}
	for range c.ComboBox(inst.ids.PrepareStr("band"), c.WidgetText().Text("").Keep(), c.WidgetText().Text(current).Keep()).KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("band-items")) {
			for b := range min(stk.axes[stk.band].Size, maxComboEntries) {
				l := stk.label(stk.band, b)
				if c.Button(inst.ids.PrepareSeq(uint64(b)), c.Atoms().Text(l).Keep()).Selected(in.Band == l).SendResp().HasPrimaryClicked() {
					in.Band = l
				}
			}
		}
	}
}

// planeChoices is the planes an input of an image that is no stack's can
// take by name: every plane of its image when it has more than one; ok is
// false for an image of one plane or none.
func (inst *App) planeChoices(in *RunInputArgs) (labels []string, ok bool) {
	id := in.Image
	if id == "" {
		id = inst.shownId
	}
	e, found := entryOf(inst.images, id)
	if !found || e.store == nil || e.store.Model().PlaneCount() < 2 {
		return nil, false
	}
	return e.store.PlaneLabels(), true
}

// planePicker picks one plane of a multi-plane input that is no stack's —
// the catalog's Band names it — or every plane.
func (inst *App) planePicker(in *RunInputArgs) {
	labels, ok := inst.planeChoices(in)
	if !ok {
		return
	}
	current := in.Band
	if current == "" {
		current = "every plane"
	}
	for range c.HoverText(tipInputPlane).KeepIter() {
		for range c.ComboBox(inst.ids.PrepareStr("plane"), c.WidgetText().Text("").Keep(), c.WidgetText().Text(current).Keep()).KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("plane-items")) {
				if c.Button(inst.ids.PrepareStr("every"), c.Atoms().Text("every plane").Keep()).Selected(in.Band == "").SendResp().HasPrimaryClicked() {
					in.Band = ""
				}
				for p, l := range labels {
					if c.Button(inst.ids.PrepareSeq(uint64(p)), c.Atoms().Text(l).Keep()).Selected(in.Band == l).SendResp().HasPrimaryClicked() {
						in.Band = l
					}
				}
			}
		}
	}
}

// positionPickers pick an input's position on each selector axis; an axis
// left alone takes the position shown.
func (inst *App) positionPickers(in *RunInputArgs) {
	stk := inst.stk
	for i, a := range stk.axes {
		if a.Role != imaging.AxisRoleSelector || a.Size <= 1 {
			continue
		}
		cur := -1
		for _, p := range in.Positions {
			if p.Axis == a.Name {
				cur = p.Position
			}
		}
		text := a.Name + " shown"
		if cur >= 0 {
			text = stk.label(i, cur)
		}
		for range c.ComboBox(inst.ids.PrepareStr("pos-"+a.Name), c.WidgetText().Text("").Keep(), c.WidgetText().Text(text).Keep()).KeepIter() {
			for range c.IdScope(inst.ids.PrepareStr("pos-items-" + a.Name)) {
				for p := range min(a.Size, maxComboEntries) {
					if c.Button(inst.ids.PrepareSeq(uint64(p)), c.Atoms().Text(stk.label(i, p)).Keep()).Selected(cur == p).SendResp().HasPrimaryClicked() {
						in.Positions = slices.DeleteFunc(in.Positions, func(q AnchorPosition) bool { return q.Axis == a.Name })
						in.Positions = append(in.Positions, AnchorPosition{Axis: a.Name, Position: p})
					}
				}
			}
		}
	}
}

// renderParamField draws one parameter's field by its type, and says at
// once whether its value will be taken.
func (inst *App) renderParamField(d imgops.Declaration, q imgops.Param) {
	text := inst.panel.param(q.Name)
	for range c.Horizontal().KeepIter() {
		label := q.Name
		if q.Unit != "" {
			label += " (" + q.Unit + ")"
		}
		c.Label(label).Selectable(false).Send()
		tip := q.Doc
		if q.HasRange {
			tip += fmt.Sprintf(" From %g to %g.", q.Min, q.Max)
		}
		switch q.Type {
		case imgops.ParamTypeChoice:
			for range c.IdScope(inst.ids.PrepareStr(q.Name)) {
				for _, ch := range q.Choices {
					if inst.button(ch.Name, ch.Name, ch.Doc, text == ch.Name) && text != ch.Name {
						inst.panel.setParam(q.Name, ch.Name)
					}
				}
			}
		default:
			width := float32(paramTextWidth)
			hint := q.Type.String()
			if q.Type == imgops.ParamTypeExpression {
				width, hint, tip = 260, "expression over the inputs' names", q.Doc+" "+tipExpression
			}
			if inst.panel.params[q.Name] == nil {
				inst.panel.setParam(q.Name, "")
			}
			for range c.HoverText(tip).KeepIter() {
				c.TextEdit(inst.ids.PrepareStr(q.Name), text, false).AccessibleName("Parameter " + q.Name).HintText(hint).DesiredWidth(width).SendRespVal(inst.panel.params[q.Name])
			}
		}
	}
	if msg := inst.fieldMessage(d, q, text); msg != "" {
		c.Label(msg).Send()
	}
}

// fieldMessage is what a parameter's field says under it: an expression's
// parse error with its column, with the names it can read; a required
// parameter not chosen; a value the declaration refuses.
func (inst *App) fieldMessage(d imgops.Declaration, q imgops.Param, text string) string {
	if q.Type == imgops.ParamTypeExpression {
		if inst.panel.exprFor != text {
			inst.panel.exprFor, inst.panel.exprErr = text, ""
			if strings.TrimSpace(text) != "" {
				if _, err := imgops.ParseExpression(text); err != nil {
					var pe *imgops.ParseError
					if errors.As(err, &pe) {
						inst.panel.exprErr = pe.Error()
					} else {
						inst.panel.exprErr = refusalText(err)
					}
				}
			}
		}
		reads := "inputs: " + strings.Join(inst.panel.names, ", ") + " · functions: " + strings.Join(imgops.ExpressionFunctions(), ", ")
		if inst.panel.exprErr != "" {
			return inst.panel.exprErr + " — " + reads
		}
		return reads
	}
	if strings.TrimSpace(text) == "" {
		if q.Required {
			return q.Name + " is required: choose one"
		}
		return ""
	}
	v, err := paramValue(q, text)
	if err == nil {
		_, err = d.CheckParam(q.Name, v)
	}
	if err != nil {
		return refusalText(err)
	}
	return ""
}

// refreshPlan states the section's plan, recomputed when the section, the
// images or the stack position change, and returns why it cannot run, or
// nothing.
func (inst *App) refreshPlan() (reason string) {
	args := inst.panelArgs()
	key := fmt.Sprintf("%v|%v|%s|%s|%d", args, len(inst.images), inst.shownId, inst.stackKey(), inst.runLanded)
	if key == inst.panel.planKey {
		return inst.panel.planErr
	}
	inst.panel.planKey = key
	inst.panel.plan, inst.panel.planErr, inst.panel.names = "", "", nil
	req, decl, err := inst.runSourceNow().request(inst.ioCtx, args)
	if err == nil {
		for _, in := range req.Inputs {
			inst.panel.names = append(inst.panel.names, in.Name)
		}
		for _, q := range args.Params {
			if p, has := decl.Param(q.Name); has && p.Required && strings.TrimSpace(q.Value) == "" {
				inst.panel.planErr = q.Name + " is required: choose one"
				return inst.panel.planErr
			}
		}
		var plan imgops.Plan
		var bytes int64
		if plan, bytes, err = planOf(req); err == nil {
			pi := planInfoOf(decl, req, plan, bytes, heldResultBytes(inst.images))
			inst.panel.plan = fmt.Sprintf("%d × %d %s %s, planes %s, about %s", pi.Width, pi.Height, pi.Model, pi.Kind,
				strings.Join(pi.Planes, ", "), formatBytes(pi.Bytes))
			if pi.Border != "" {
				inst.panel.plan += fmt.Sprintf(", a neighbourhood of %d px with border %s", pi.Halo, pi.Border)
			}
			if pi.FirstPasses > 0 {
				inst.panel.plan += fmt.Sprintf(", %d first passes", pi.FirstPasses)
			}
			if len(pi.Notes) > 0 {
				inst.panel.plan += ". " + strings.Join(pi.Notes, " ")
			}
			return ""
		}
	}
	inst.panel.planErr = refusalText(err)
	return inst.panel.planErr
}
