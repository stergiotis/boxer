package imagescope

// Operations on images (ADR-0400 §M2): an operator of
// public/science/imaging/ops applied to images of the window — the image
// opened, a stack's planes taken at stated positions, or earlier results —
// run once at full resolution as a task, shown as it fills, and added to
// the window's images with the record of what it did. One run at a time
// per window; a cancelled or failed run leaves nothing.

import (
	"context"
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/imaging"
	imgops "github.com/stergiotis/boxer/public/science/imaging/ops"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

const (
	runKind  = "imagescope.run"
	runTitle = "run an image operation"

	// runningId is the id the result of a run under way is shown under
	// while it fills; it is not in the list until it finishes.
	runningId = "running"

	// recordPageMax bounds the bytes of a record get_record returns at once.
	recordPageMax = 256 << 10
)

// ParamArg is one parameter's value.
type ParamArg struct {
	Name  string `desc:"the parameter's name, from list_operators"`
	Value string `desc:"its value as text: a number for a number or integer parameter, a choice's name, an expression in the grammar; empty takes the default"`
}

// RunInputArgs is one input of an operation, or for Over one per position
// of a stack's axis.
type RunInputArgs struct {
	Name      string           `json:",omitzero" desc:"the input's name, as the expression reads it: a letter or underscore, then letters, digits and underscores, no function's name; empty takes the band's label, or the image's id"`
	Image     string           `json:",omitzero" desc:"the image's id from list_images; empty is the image shown"`
	Band      string           `json:",omitzero" desc:"for the stack opened: the band by its label, empty for the one band shown; for any other image one plane by its name, empty for every plane"`
	Positions []AnchorPosition `json:",omitzero" desc:"for the stack opened: positions on its selector axes; an axis not named takes the position shown"`
	Over      string           `json:",omitzero" desc:"for the stack opened: a selector axis; this entry then stands for one input per position of the axis, named <Name>_<position>, as a composite over time needs"`
}

// RunArgs is an operation as plan_operation and run_operation take it.
type RunArgs struct {
	Operator string         `desc:"the operator's id, from list_operators: expr, axis-stat, gaussian, median-filter or standardize"`
	Params   []ParamArg     `json:",omitzero" desc:"the parameters given; one not given takes its default, and a required one has none"`
	Inputs   []RunInputArgs `desc:"the inputs, in order; their number is within the operator's MinInputs and MaxInputs"`
	Name     string         `json:",omitzero" desc:"the result's name in list_images; empty takes the operation's own"`
}

// runSource is what an operation's inputs are taken from: the window's
// images and the stack opened.
type runSource struct {
	images  []imageEntry
	shownId string
	stk     *stackView
}

// inputName makes a label an input's name: characters outside an
// identifier become underscores, and a name that would start with a digit
// or be a function's starts with in_.
func inputName(label string) string {
	var b strings.Builder
	for _, r := range label {
		if r == '_' || r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" || !imgops.IsInputName(s) {
		s = "in_" + s
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// operator is the operator of an id, refused naming the ones there are.
func operatorOf(id string) (imgops.OperatorI, error) {
	op, ok := imgops.Lookup(id)
	if !ok {
		var ids []string
		for o := range imgops.All() {
			ids = append(ids, o.Declaration().Id)
		}
		return nil, app.RefuseOperation("no operator " + strconv.Quote(id) + "; the operators are " + strings.Join(ids, ", "))
	}
	return op, nil
}

// paramsOf reads the parameters given as text into values of their
// declared types; an empty value is left out, to take the default.
func paramsOf(d imgops.Declaration, given []ParamArg) (imgops.Params, error) {
	out := make(imgops.Params, len(given))
	for _, g := range given {
		p, ok := d.Param(g.Name)
		if !ok {
			names := make([]string, len(d.Params))
			for i, q := range d.Params {
				names[i] = q.Name
			}
			return nil, app.RefuseOperation(d.Id + " has no parameter " + strconv.Quote(g.Name) + "; its parameters are " + quotedList(names))
		}
		v, err := paramValue(p, g.Value)
		if err != nil {
			return nil, err
		}
		if !v.IsZero() {
			out[g.Name] = v
		}
	}
	return out, nil
}

// paramValue is a parameter's text as a value of its type; the zero value
// for empty text.
func paramValue(p imgops.Param, text string) (v imgops.Value, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return v, nil
	}
	switch p.Type {
	case imgops.ParamTypeNumber, imgops.ParamTypeInteger:
		x, perr := strconv.ParseFloat(text, 64)
		if perr != nil {
			return v, app.RefuseOperation("parameter " + p.Name + " takes a number; " + strconv.Quote(text) + " is not one")
		}
		return imgops.Number(x), nil
	}
	return imgops.Text(text), nil
}

// request builds an operation's request from the window's images:
// resolving each input to a plane of the stack, a plane of an image, or a
// whole image, and expanding an input over an axis. It reads no sample but
// to copy one plane out of an image that is no stack's.
func (inst runSource) request(ctx context.Context, args RunArgs) (req imgops.Request, decl imgops.Declaration, err error) {
	op, err := operatorOf(args.Operator)
	if err != nil {
		return req, decl, err
	}
	decl = op.Declaration()
	params, err := paramsOf(decl, args.Params)
	if err != nil {
		return req, decl, err
	}
	req = imgops.Request{Operator: decl.Id, Params: params}
	for k, in := range args.Inputs {
		ins, ierr := inst.inputs(ctx, in, k)
		if ierr != nil {
			return req, decl, ierr
		}
		req.Inputs = append(req.Inputs, ins...)
	}
	return req, decl, nil
}

// inputs resolves one input argument.
func (inst runSource) inputs(ctx context.Context, in RunInputArgs, k int) ([]imgops.Input, error) {
	id := in.Image
	if id == "" {
		id = inst.shownId
		if id == runningId || id == "" {
			id = openedId
		}
	}
	e, ok := entryOf(inst.images, id)
	if !ok || e.store == nil {
		return nil, app.RefuseOperation(fmt.Sprintf("input %d names no image %q; list_images lists them", k+1, id))
	}
	if !e.store.Finished() {
		return nil, app.RefuseOperation(fmt.Sprintf("input %d: image %s is still being read; wait until it is", k+1, id))
	}
	if id == openedId && inst.stk != nil {
		return inst.stackInputs(in, k)
	}
	if in.Over != "" || len(in.Positions) > 0 {
		return nil, app.RefuseOperation(fmt.Sprintf("input %d: image %s is no stack's selection, so it has no positions; leave Over and Positions empty", k+1, id))
	}
	name := in.Name
	if name == "" {
		name = inputName(id)
	}
	if in.Band == "" {
		return []imgops.Input{{Name: name, Source: e.store, Record: e.record}}, nil
	}
	src, err := planeSource(ctx, e.store, in.Band)
	if err != nil {
		return nil, err
	}
	if in.Name == "" {
		name = inputName(in.Band)
	}
	return []imgops.Input{{Name: name, Source: src, Positions: []imgops.Position{{Axis: "plane", Index: slices.Index(e.store.PlaneLabels(), in.Band), Label: in.Band}}, Record: e.record}}, nil
}

// planeSource is one named plane of an image as an image of its own: its
// samples copied out of the store, with its transform, its declared invalid
// value and the image's placement.
func planeSource(ctx context.Context, s *tiles.Store, band string) (imgops.SourceI, error) {
	labels := s.PlaneLabels()
	i := slices.Index(labels, band)
	if i < 0 {
		return nil, app.RefuseOperation("no plane named " + strconv.Quote(band) + "; the planes are " + quotedList(labels))
	}
	img, err := s.Read(ctx, image.Rect(0, 0, s.Width(), s.Height()))
	if err != nil {
		return nil, refuse(err)
	}
	one, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{img.Plane(i)}, img.Provenance())
	if err != nil {
		return nil, refuse(err)
	}
	if one, err = one.WithPlaneLabels([]string{band}); err != nil {
		return nil, refuse(err)
	}
	return imgops.FromImage(one.WithPlacement(s.Placement())), nil
}

// stackInputs resolves an input taken from the stack opened.
func (inst runSource) stackInputs(in RunInputArgs, k int) ([]imgops.Input, error) {
	stk := inst.stk
	at := slices.Clone(stk.shown.at)
	for _, p := range in.Positions {
		i := stk.axisIndex(p.Axis)
		switch {
		case i < 0 || stk.axes[i].Role != imaging.AxisRoleSelector:
			return nil, app.RefuseOperation(fmt.Sprintf("input %d: no selector axis %q; the selector axes are %s", k+1, p.Axis, quotedList(stk.selectorNames())))
		case p.Position < 0 || p.Position >= stk.axes[i].Size:
			return nil, app.RefuseOperation(fmt.Sprintf("input %d: position %d is outside %q, whose positions are 0 to %d", k+1, p.Position, p.Axis, stk.axes[i].Size-1))
		}
		at[i] = p.Position
	}
	band := 0
	if stk.band >= 0 {
		switch {
		case in.Band != "":
			band = -1
			for b := range stk.axes[stk.band].Size {
				if stk.label(stk.band, b) == in.Band {
					band = b
					break
				}
			}
			if band < 0 {
				names := make([]string, stk.axes[stk.band].Size)
				for b := range names {
					names[b] = stk.label(stk.band, b)
				}
				return nil, app.RefuseOperation(fmt.Sprintf("input %d: no band %q; the bands are %s", k+1, in.Band, quotedList(names)))
			}
		case len(stk.shown.bands) == 1:
			band = stk.shown.bands[0]
		default:
			return nil, app.RefuseOperation(fmt.Sprintf("input %d: more than one band is shown; name the band by its label", k+1))
		}
	}
	name := in.Name
	if name == "" {
		name = inputName(stk.label(max(stk.band, 0), band))
		if stk.band < 0 {
			name = "plane"
		}
	}
	if in.Over == "" {
		input, err := imgops.StackInput(name, stk.stack, at, band)
		if err != nil {
			return nil, refuse(err)
		}
		return []imgops.Input{input}, nil
	}
	ax := stk.axisIndex(in.Over)
	if ax < 0 || stk.axes[ax].Role != imaging.AxisRoleSelector {
		return nil, app.RefuseOperation(fmt.Sprintf("input %d: Over names no selector axis %q; the selector axes are %s", k+1, in.Over, quotedList(stk.selectorNames())))
	}
	out := make([]imgops.Input, 0, stk.axes[ax].Size)
	for p := range stk.axes[ax].Size {
		at[ax] = p
		input, err := imgops.StackInput(name+"_"+strconv.Itoa(p), stk.stack, at, band)
		if err != nil {
			return nil, refuse(err)
		}
		out = append(out, input)
	}
	return out, nil
}

// opsReason is an operation's refusal as the window states it: the
// sentence without the marker every refusal carries, and the facts the
// engine attached that say which parameter, input or choice it is about.
func opsReason(err error) string {
	s := strings.TrimSuffix(refusalText(err), ": "+imgops.ErrRefused.Error())
	var facts []string
	if v, ok := eh.LookupFact[string](err, "param"); ok {
		facts = append(facts, "parameter "+v)
	}
	if v, ok := eh.LookupFact[string](err, "input"); ok {
		facts = append(facts, "input "+v)
	}
	if v, ok := eh.LookupFact[[]string](err, "choices"); ok {
		facts = append(facts, "choices "+strings.Join(v, ", "))
	}
	lo, okLo := eh.LookupFact[float64](err, "min")
	hi, okHi := eh.LookupFact[float64](err, "max")
	if okLo && okHi {
		facts = append(facts, "range "+strconv.FormatFloat(lo, 'g', -1, 64)+" to "+strconv.FormatFloat(hi, 'g', -1, 64))
	}
	if len(facts) > 0 {
		s += " (" + strings.Join(facts, "; ") + ")"
	}
	return s
}

// planOf is an operation's plan, or why it cannot run, and what its result
// would take in memory: the samples of its planes and about a third more
// for the levels.
func planOf(req imgops.Request) (plan imgops.Plan, bytes int64, err error) {
	plan, err = imgops.PlanOf(req)
	if err != nil {
		return plan, 0, app.RefuseOperation("the operation cannot run: " + opsReason(err))
	}
	per := int64(4)
	if plan.Kind == imaging.SampleKindF64 {
		per = 8
	}
	bytes = int64(plan.Width) * int64(plan.Height) * int64(plan.Model.PlaneCount()) * per * 4 / 3
	return plan, bytes, nil
}

// ranResult is what a run leaves.
type ranResult struct {
	res  imgops.Result
	name string
}

// runSourceNow is the window's images as a run takes them.
func (inst *App) runSourceNow() runSource {
	return runSource{images: inst.images, shownId: inst.shownId, stk: inst.stk}
}

// startRun runs an operation as a task: refused while another runs, when
// its plan is refused, and when its result would take the results held
// past the cap. Its result is shown as it fills.
func (inst *App) startRun(args RunArgs) error {
	if inst.pic == nil && len(inst.images) == 0 {
		return errNoImage
	}
	if inst.runJob.Running() {
		return app.RefuseOperation("a run is under way, one at a time per window; wait for it, or cancel it with cancel_operation")
	}
	req, decl, err := inst.runSourceNow().request(inst.ioCtx, args)
	if err != nil {
		return err
	}
	_, bytes, err := planOf(req)
	if err != nil {
		return err
	}
	if held, limit := heldResultBytes(inst.images), inst.resultCap(); held+bytes > limit {
		return app.RefuseOperation(fmt.Sprintf("the results held take %s and this one would take about %s, past the %s the window holds; drop a result with drop_image",
			formatBytes(held), formatBytes(bytes), formatBytes(limit)))
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = decl.Id + " of " + inputNames(req)
	}
	inst.runErr, inst.runNote, inst.lastResult = "", "", ""
	inst.runPrevShown = inst.shownId
	inst.runArriving.Store(nil)
	inst.runCallGen++
	started := inst.runJob.StartReporting(inst.tasks, bgjob.Spec{Kind: runKind, Title: runTitle + ": " + decl.Ref()},
		func(ctx context.Context, report bgjob.Reporter) (*ranResult, error) {
			res, err := imgops.Run(ctx, req, imgops.Options{
				Progress: func(done, total int64) { report(uint64(done), uint64(total), "computing "+decl.Id) },
				OnStore:  func(s *tiles.Store) { inst.runArriving.Store(s) },
			})
			if err != nil {
				return nil, err
			}
			return &ranResult{res: res, name: name}, nil
		})
	if !started {
		inst.runCallGen--
		return app.RefuseOperation("a run is under way; wait for it, or cancel it with cancel_operation")
	}
	inst.runActive = true
	return nil
}

// inputNames lists a request's inputs by name.
func inputNames(req imgops.Request) string {
	names := make([]string, len(req.Inputs))
	for i, in := range req.Inputs {
		names[i] = in.Name
	}
	if len(names) > 4 {
		return strings.Join(names[:3], ", ") + " … " + names[len(names)-1]
	}
	return strings.Join(names, ", ")
}

// takeRun moves a run's progress and outcome onto the window, at the top
// of the frame: the result's store is shown as soon as it is made, and on
// success it joins the images under the next id; on failure or cancel what
// was shown before comes back and nothing is kept.
func (inst *App) takeRun() {
	if s := inst.runArriving.Swap(nil); s != nil && s != inst.pic && inst.runJob.Running() {
		keep := inst.pic != nil && inst.pic.Width() == s.Width() && inst.pic.Height() == s.Height()
		region, _ := inst.viewer.Region()
		inst.adopt(s, keep)
		inst.shownId = runningId
		if keep && !region.Empty() {
			inst.viewer.SetRegion(region)
		}
	}
	if r, _, ok := inst.runJob.TakeResult(); ok {
		e := inst.addResult(r.res, r.name)
		inst.lastResult = e.id
		if inst.shownId == runningId && inst.pic == e.store {
			inst.shownId = e.id
		} else {
			inst.showEntry(e)
		}
		inst.runNote = "made " + describeEntry(e)
		inst.runLanded = inst.runCallGen
		inst.runActive = false
		return
	}
	snap := inst.runJob.Snapshot()
	cancelled := inst.runActive && snap.State == bgjob.StateIdle
	if snap.State == bgjob.StateFailed || cancelled {
		inst.runActive = false
		if cancelled || errorsIsCancelled(snap.Err) {
			inst.runNote = "the run was cancelled; nothing was kept"
		} else {
			inst.runErr = "the run failed: " + opsReason(snap.Err)
		}
		if inst.shownId == runningId {
			if e, ok := entryOf(inst.images, inst.runPrevShown); ok {
				inst.showEntry(e)
			} else if e, ok := entryOf(inst.images, openedId); ok {
				inst.showEntry(e)
			}
		}
		inst.runLanded = inst.runCallGen
		inst.runJob.Invalidate()
	}
}

// OperatorParam is one parameter of an operator, as declared.
type OperatorParam struct {
	Name     string   `desc:"the parameter's name, as ParamArg.Name takes it"`
	Type     string   `desc:"number, integer, choice, text or expression"`
	Unit     string   `json:",omitzero" desc:"the unit of a number: px for pixels of the input grid"`
	HasRange bool     `json:",omitzero" desc:"the value lies in [Min, Max]"`
	Min      float64  `json:",omitzero" desc:"the least value, when HasRange"`
	Max      float64  `json:",omitzero" desc:"the greatest value, when HasRange"`
	Choices  []string `json:",omitzero" desc:"the values of a choice, each with what it means after a colon"`
	Default  string   `json:",omitzero" desc:"the value taken when none is given"`
	Required bool     `json:",omitzero" desc:"a request must give it: it has no default"`
	Doc      string   `desc:"what the parameter does"`
}

// OperatorInfo is one operator, as declared once (ADR-0400 §SD1).
type OperatorInfo struct {
	Id               string          `desc:"the operator's id, as RunArgs.Operator takes it"`
	Version          uint32          `desc:"changes whenever the same request would give other samples"`
	Summary          string          `desc:"what it computes"`
	MinInputs        int             `desc:"the fewest inputs it takes"`
	MaxInputs        int             `json:",omitzero" desc:"the most inputs it takes; 0 is no bound"`
	InputsDoc        string          `desc:"what its inputs are"`
	LeavesInvalidOut bool            `json:",omitzero" desc:"an output sample is computed from the valid samples it depends on, so an invalid input sample does not make it invalid"`
	Params           []OperatorParam `desc:"its parameters"`
	Help             string          `desc:"the same as text"`
}

// OperatorList is list_operators's result.
type OperatorList struct {
	Operators  []OperatorInfo `desc:"the operators, in the order of their ids"`
	Functions  []string       `desc:"the functions an expression may call; no input may be named after one"`
	Grammar    string         `desc:"the expression grammar in brief"`
	ValueRules string         `desc:"how values, invalid samples and the border are treated"`
}

// expressionGrammar is the grammar in brief, for list_operators and the
// field's hover.
const expressionGrammar = "Arithmetic + - * /, comparisons < <= > >= == != (giving 1 or 0, not chained), && || !, parentheses, numbers, the inputs' names, and the functions abs, sqrt, exp, log, log10 (one argument), pow, min, max (two), clamp(x, lo, hi) and where(condition, a, b), isvalid(x). An invalid operand makes the result invalid, except in isvalid and in the branch where does not take."

// valueRules is how an operation treats values, for list_operators and the
// method page.
const valueRules = "Every operation reads values — the stored samples through their value transform — and writes a float plane: 32-bit where every input is u8, u16, i16 or f32, 64-bit otherwise; nothing wraps or saturates. " +
	"An output sample that depends on an invalid input sample is invalid, except where the operator leaves invalid samples out (axis-stat); a result that is not finite is invalid. " +
	"A neighbourhood operator takes a border with no default: invalid makes every output sample whose neighbourhood reaches outside the image invalid; nearest lets the nearest edge sample stand in. " +
	"Nothing is computed at a reduced level: the result is computed once, whole, and its levels are reduced from it."

// operatorInfoOf is an operator's declaration as the catalog gives it.
func operatorInfoOf(d imgops.Declaration) OperatorInfo {
	out := OperatorInfo{Id: d.Id, Version: d.Version, Summary: d.Summary, MinInputs: d.MinInputs, MaxInputs: d.MaxInputs,
		InputsDoc: d.InputsDoc, LeavesInvalidOut: d.LeavesInvalidOut, Help: d.Help()}
	for _, p := range d.Params {
		op := OperatorParam{Name: p.Name, Type: p.Type.String(), Unit: p.Unit, HasRange: p.HasRange, Min: p.Min, Max: p.Max,
			Required: p.Required, Doc: p.Doc}
		if !p.Default.IsZero() {
			op.Default = paramText(p.Default)
		}
		for _, ch := range p.Choices {
			op.Choices = append(op.Choices, ch.Name+": "+ch.Doc)
		}
		out.Params = append(out.Params, op)
	}
	return out
}

// paramText is a value as a parameter field holds it.
func paramText(v imgops.Value) string {
	if s, ok := v.Text(); ok {
		return s
	}
	if x, ok := v.Number(); ok {
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return ""
}

// PlanInfo is plan_operation's result: what a run would make, stated
// before a sample is read (ADR-0400 §SD3).
type PlanInfo struct {
	Operator    string   `desc:"the operator and its version, operator@version"`
	Inputs      []string `desc:"the inputs' names, in order, as the run would name them"`
	Width       int      `desc:"columns of the result"`
	Height      int      `desc:"rows of the result"`
	Model       string   `desc:"its colour model"`
	Kind        string   `desc:"its sample kind: f32 or f64"`
	Planes      []string `desc:"its planes' names"`
	Halo        int      `json:",omitzero" desc:"rows and columns of input a band needs on each side"`
	Border      string   `json:",omitzero" desc:"what stands in for samples outside the image: invalid or nearest"`
	FirstPasses int      `json:",omitzero" desc:"passes over the whole input before any output"`
	Notes       []string `json:",omitzero" desc:"what the plan decided, in sentences"`
	Bytes       int64    `desc:"about what the result would take in memory, levels included"`
	HeldBytes   int64    `desc:"what the results held take"`
	CapBytes    int64    `desc:"what the results may take; a run past it is refused"`
}

func planInfoOf(decl imgops.Declaration, req imgops.Request, plan imgops.Plan, bytes, held int64) PlanInfo {
	out := PlanInfo{Operator: decl.Ref(), Width: plan.Width, Height: plan.Height, Model: plan.Model.String(), Kind: plan.Kind.String(),
		Planes: plan.PlaneLabels, Halo: plan.Halo, FirstPasses: plan.FirstPasses, Notes: plan.Notes, Bytes: bytes, HeldBytes: held, CapBytes: maxResultBytes}
	if plan.Border != 0 {
		out.Border = plan.Border.String()
	}
	for _, in := range req.Inputs {
		out.Inputs = append(out.Inputs, in.Name)
	}
	return out
}

// RecordInputSummary is one input of a record, shortened.
type RecordInputSummary struct {
	Name      string   `desc:"the input's name"`
	Digest    string   `desc:"the first 16 hex digits of its samples' digest; get_record has the whole"`
	Positions []string `json:",omitzero" desc:"where on a stack it was taken: axis label (index)"`
	Result    string   `json:",omitzero" desc:"the operator of the run that made it, when it is a result"`
}

// RecordSummary is a record as a result's description gives it.
type RecordSummary struct {
	Operator   string               `desc:"operator@version"`
	Params     []string             `desc:"the parameters as resolved, name=value"`
	Inputs     []RecordInputSummary `desc:"the inputs"`
	Quantities []string             `json:",omitzero" desc:"what the first passes found, name[plane]=value"`
	Notes      []string             `json:",omitzero" desc:"what the plan decided"`
	Digest     string               `desc:"the first 16 hex digits of the result's digest"`
}

// short is a digest's first 16 hex digits.
func short(d imgops.Digest) string { return d.String()[:16] }

func recordSummaryOf(rec imgops.Record) RecordSummary {
	out := RecordSummary{Operator: rec.Operator + "@" + strconv.FormatUint(uint64(rec.Version), 10), Notes: rec.Notes, Digest: short(rec.Output.Digest)}
	names := make([]string, 0, len(rec.Canonical))
	for n := range rec.Canonical {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		out.Params = append(out.Params, n+"="+paramText(rec.Canonical[n]))
	}
	for _, in := range rec.Inputs {
		s := RecordInputSummary{Name: in.Name, Digest: short(in.Digest)}
		for _, p := range in.Positions {
			s.Positions = append(s.Positions, fmt.Sprintf("%s %s (%d)", p.Axis, p.Label, p.Index))
		}
		if in.Record != nil {
			s.Result = in.Record.Operator + "@" + strconv.FormatUint(uint64(in.Record.Version), 10)
		}
		out.Inputs = append(out.Inputs, s)
	}
	for _, q := range rec.Quantities {
		out.Quantities = append(out.Quantities, fmt.Sprintf("%s[%d]=%s", q.Name, q.Plane, strconv.FormatFloat(q.Value, 'g', -1, 64)))
	}
	return out
}

// RunOutcome is what run_operation settles with.
type RunOutcome struct {
	Image       string         `json:",omitzero" desc:"the result's id in list_images; empty when the run made nothing"`
	Description string         `json:",omitzero" desc:"the result in one line"`
	Record      *RecordSummary `json:",omitzero" desc:"what the operation did, shortened; get_record has the whole"`
	Error       string         `json:",omitzero" desc:"why the run made nothing: failed, or cancelled"`
}

// RecordPage is get_record's result.
type RecordPage struct {
	Image     string `desc:"the result's id"`
	Bytes     int    `desc:"the record's size in bytes"`
	From      int    `desc:"the byte the text starts at"`
	Record    string `desc:"the record's JSON document from From, at most 256 KiB; the whole when Truncated is false and From is 0"`
	Truncated bool   `json:",omitzero" desc:"bytes follow; ask again from From plus the length of Record"`
}

// GetRecordArgs names a result's record and where to read it from.
type GetRecordArgs struct {
	Id   string `desc:"the result's id from list_images"`
	From int    `json:",omitzero" desc:"the byte to start at"`
}

// ImageIdArgs names an image.
type ImageIdArgs struct {
	Id string `desc:"the image's id from list_images"`
}
