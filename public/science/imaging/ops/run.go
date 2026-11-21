package ops

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
	"lukechampine.com/blake3"
)

// Request is an operation as asked for: an operator by id, its parameters
// and its named inputs.
type Request struct {
	Operator string
	Params   Params
	Inputs   []Input
}

// Options adjust a run. None of them changes the result's samples.
type Options struct {
	// BandRows is how many output rows a band holds; 0 takes [tiles.Size].
	BandRows int
	// Workers is how many goroutines compute a band's rows; 0 takes
	// GOMAXPROCS.
	Workers int
	// Levels is how many reduced levels the result's store builds, as
	// [tiles.Options] takes it.
	Levels int
	// Progress, when not nil, is told the rows done and the total: the rows
	// of every first pass (ADR-0400 §SD7) and then the output rows, so an
	// operator that makes two first passes over an image of h rows counts to
	// 3·h, and one that makes none to h.
	Progress func(done, total int64)
	// OnStore, when not nil, is given the result's store before its first
	// band is computed, so that a viewer shows the result as it fills.
	OnStore func(*tiles.Store)
}

// Result is a finished operation: the store that holds its output and the
// record of what it did.
type Result struct {
	Store  *tiles.Store
	Record Record
}

type prepared struct {
	op        OperatorI
	decl      Declaration
	canonical Params
	descs     []InputDesc
	records   []*Record
	plan      Plan
	kernel    KernelI
}

// prepare validates a request and plans it, reading no sample.
func prepare(req Request) (pr prepared, err error) {
	op, ok := Lookup(req.Operator)
	if !ok {
		return pr, eb.Build().Str("operator", req.Operator).Strs("operators", operatorIds()).
			Errorf("no operator of that id: %w", ErrRefused)
	}
	pr.op, pr.decl = op, op.Declaration()
	if pr.canonical, err = pr.decl.Validate(req.Params); err != nil {
		return pr, err
	}
	d := pr.decl
	if n := len(req.Inputs); n < d.MinInputs || (d.MaxInputs != 0 && n > d.MaxInputs) {
		return pr, eb.Build().Str("operator", d.Id).Int("inputs", n).Int("min", d.MinInputs).Int("max", d.MaxInputs).
			Errorf("the operator takes another number of inputs: %w", ErrRefused)
	}
	functions := ExpressionFunctions()
	pr.descs = make([]InputDesc, len(req.Inputs))
	pr.records = make([]*Record, len(req.Inputs))
	for i, in := range req.Inputs {
		switch {
		case !inputNamePattern.MatchString(in.Name) || slices.Contains(functions, in.Name):
			return pr, eb.Build().Str("input", in.Name).
				Errorf("an input's name is an identifier of at most 64 characters that is no function name: %w", ErrRefused)
		case slices.ContainsFunc(req.Inputs[:i], func(o Input) bool { return o.Name == in.Name }):
			return pr, eb.Build().Str("input", in.Name).Errorf("two inputs have one name: %w", ErrRefused)
		case in.Source == nil || in.Source.Model().PlaneCount() == 0:
			return pr, eb.Build().Str("input", in.Name).Errorf("the input has no image: %w", ErrRefused)
		}
		pr.descs[i] = describe(in)
		first := pr.descs[0]
		if in := pr.descs[i]; in.Width != first.Width || in.Height != first.Height {
			return pr, eb.Build().Str("input", in.Name).Int("width", in.Width).Int("height", in.Height).
				Str("firstInput", first.Name).Int("firstWidth", first.Width).Int("firstHeight", first.Height).
				Errorf("the inputs are not on one grid: their sizes differ: %w", ErrRefused)
		}
	}
	for i, in := range req.Inputs {
		rec := in.Record
		if rec == nil {
			if s, isStore := in.Source.(*tiles.Store); isStore {
				if r, known := RecordOf(s); known {
					rec = &r
				}
			}
		}
		if rec != nil && !pr.descs[i].Digest.IsZero() && rec.Output.Digest != pr.descs[i].Digest {
			return pr, eb.Build().Str("input", in.Name).Stringer("digest", pr.descs[i].Digest).Stringer("recorded", rec.Output.Digest).
				Errorf("the record given for an input is the record of another image: %w", ErrRefused)
		}
		pr.records[i] = rec
	}
	if pr.plan, pr.kernel, err = op.Plan(slices.Clone(pr.descs), pr.canonical); err != nil {
		return pr, err
	}
	p := &pr.plan
	first := pr.descs[0]
	p.Width, p.Height, p.Transform = first.Width, first.Height, imaging.IdentityTransform
	p.Placement = imaging.Placement{}
	shared := true
	for _, in := range pr.descs[1:] {
		shared = shared && samePlacement(in.Placement, first.Placement)
	}
	if shared {
		p.Placement = first.Placement
	} else {
		p.Notes = append(p.Notes, "The inputs are placed differently, so the result has no placement.")
	}
	switch {
	case pr.kernel == nil:
		return pr, eb.Build().Str("operator", d.Id).Errorf("the operator planned no kernel")
	case p.Model.PlaneCount() == 0 || (p.Kind != imaging.SampleKindF32 && p.Kind != imaging.SampleKindF64):
		return pr, eb.Build().Str("operator", d.Id).Stringer("model", p.Model).Stringer("kind", p.Kind).
			Errorf("the operator planned an output that is not float planes of a colour model")
	case p.PlaneLabels != nil && len(p.PlaneLabels) != p.Model.PlaneCount():
		return pr, eb.Build().Str("operator", d.Id).Int("labels", len(p.PlaneLabels)).Errorf("the operator planned a label count that is not the plane count")
	case p.Halo < 0 || (p.Halo > 0 && p.Border == 0) || p.FirstPasses < 0:
		return pr, eb.Build().Str("operator", d.Id).Int("halo", p.Halo).Stringer("border", p.Border).
			Errorf("the operator planned a halo without a border rule")
	}
	return pr, nil
}

func samePlacement(a, b imaging.Placement) bool {
	return a.Affine == b.Affine && a.Frame == b.Frame && a.Source == b.Source && slices.Equal(a.Unapplied, b.Unapplied)
}

// PlanOf states what a request would make, reading no sample (ADR-0400
// §SD3); a request that cannot run is refused with the reason.
func PlanOf(req Request) (Plan, error) {
	pr, err := prepare(req)
	return pr.plan, err
}

// RequestIdentity is the digest of what decides a request's output
// (ADR-0400 §SD8): the operator's id and version, the canonical parameters,
// and each input's name, digest, value transforms and declared invalid
// value. It is known before the request runs, once every input's samples
// are; an input whose digest is not known yet is refused.
func RequestIdentity(req Request) (id Digest, err error) {
	pr, err := prepare(req)
	if err != nil {
		return id, err
	}
	return pr.identity()
}

func (inst *prepared) identity() (id Digest, err error) {
	for _, in := range inst.descs {
		if in.Digest.IsZero() {
			return id, eb.Build().Str("input", in.Name).Errorf("the input's samples are not all there yet, so it has no digest: %w", ErrRefused)
		}
	}
	return identityOf(inst.decl, inst.canonical, inst.descs)
}

func blake3Sum(data []byte) [32]byte { return blake3.Sum256(data) }

// Run performs an operation (ADR-0400 §SD9): it waits for the inputs'
// samples, validates and plans the request, makes the first passes, and
// then computes the output top to bottom in bands of rows into a tiled
// store, which OnStore is given before the first band. Cancelling ctx stops
// it between bands; a cancelled or failed run fails the store, so that no
// finished result exists, and returns no record. The finished store's
// provenance names the operation, and [RecordOf] returns its record while
// the store is held.
func Run(ctx context.Context, req Request, opts Options) (res Result, err error) {
	for _, in := range req.Inputs {
		if in.Source == nil {
			continue
		}
		if err = in.Source.Wait(ctx); err != nil {
			return res, eb.Build().Str("input", in.Name).Errorf("the input's samples are not there: %w", err)
		}
	}
	pr, err := prepare(req)
	if err != nil {
		return res, err
	}
	id, err := pr.identity()
	if err != nil {
		return res, err
	}
	srcs := make([]SourceI, len(req.Inputs))
	counts := make([]int, len(req.Inputs))
	for i, in := range req.Inputs {
		srcs[i], counts[i] = in.Source, in.Source.Model().PlaneCount()
	}
	plan := &pr.plan
	bandRows := opts.BandRows
	if bandRows <= 0 {
		bandRows = tiles.Size
	}
	bandRows = min(bandRows, plan.Height)
	// The rows of the first passes come before the output's in what
	// Progress is told.
	passRows := int64(plan.FirstPasses) * int64(plan.Height)
	total := passRows + int64(plan.Height)
	if plan.FirstPasses > 0 {
		first := newBand(plan.Width, plan.Height, 0, bandRows, counts)
		for pass := range plan.FirstPasses {
			for y0 := 0; y0 < plan.Height; y0 += bandRows {
				if err = ctx.Err(); err != nil {
					return res, err
				}
				y1 := min(y0+bandRows, plan.Height)
				if err = first.load(ctx, srcs, y0, y1, 0); err != nil {
					return res, err
				}
				if err = pr.kernel.FirstPass(pass, first); err != nil {
					return res, err
				}
				if opts.Progress != nil {
					opts.Progress(int64(pass)*int64(plan.Height)+int64(y1), total)
				}
			}
			if err = pr.kernel.EndPass(pass); err != nil {
				return res, err
			}
		}
	}
	quantities := pr.kernel.Quantities()
	for i := range quantities {
		if quantities[i].Value == 0 {
			quantities[i].Value = 0
		}
	}
	params := req.Params.Clone()
	if params == nil {
		params = Params{}
	}
	rec := Record{Schema: RecordSchema, Operator: pr.decl.Id, Version: pr.decl.Version, Request: id,
		Params: params, Canonical: pr.canonical, Inputs: make([]RecordInput, len(pr.descs)),
		Quantities: quantities, Notes: slices.Clone(plan.Notes)}
	for i, d := range pr.descs {
		rec.Inputs[i] = recordInput(d, pr.records[i])
	}
	var progress func(done, total int64)
	if opts.Progress != nil {
		progress = func(done, _ int64) { opts.Progress(passRows+done, total) }
	}
	b, err := tiles.NewBuilder(plan.Width, plan.Height, plan.Model, plan.Kind, nil, provenanceOf(&pr, id),
		tiles.Options{Levels: opts.Levels, Progress: progress, PlaneLabels: plan.PlaneLabels, Placement: plan.Placement})
	if err != nil {
		return res, err
	}
	if opts.OnStore != nil {
		opts.OnStore(b.Store())
	}
	if err = produce(ctx, &pr, srcs, counts, bandRows, opts.Workers, b); err != nil {
		b.Fail(err)
		return res, err
	}
	if err = b.Finish(); err != nil {
		return res, err
	}
	s := b.Store()
	rec.Output = recordOutput(plan, Digest(s.Provenance().SampleHash))
	remember(s, &rec)
	return Result{Store: s, Record: rec}, nil
}

// produce computes the output band by band into b.
func produce(ctx context.Context, pr *prepared, srcs []SourceI, counts []int, bandRows, workers int, b *tiles.Builder) error {
	plan := &pr.plan
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = max(1, min(workers, bandRows))
	band := newBand(plan.Width, plan.Height, plan.Halo, bandRows, counts)
	n := plan.Model.PlaneCount()
	ws := make([]WorkerI, workers)
	for i := range ws {
		ws[i] = pr.kernel.Worker()
	}
	values := make([][]float64, n)
	for p := range values {
		values[p] = make([]float64, bandRows*plan.Width)
	}
	for y0 := 0; y0 < plan.Height; y0 += bandRows {
		if err := ctx.Err(); err != nil {
			return err
		}
		y1 := min(y0+bandRows, plan.Height)
		if err := band.load(ctx, srcs, y0, y1, plan.Border); err != nil {
			return err
		}
		rows := y1 - y0
		chunk := (rows + workers - 1) / workers
		var wg sync.WaitGroup
		for k, w := range ws {
			r0, r1 := k*chunk, min((k+1)*chunk, rows)
			if r0 >= r1 {
				break
			}
			wg.Go(func() {
				out := make([][]float64, n)
				for r := r0; r < r1; r++ {
					for p := range out {
						out[p] = values[p][r*plan.Width : (r+1)*plan.Width]
					}
					w.Row(band, y0+r, out)
				}
			})
		}
		wg.Wait()
		planes := make([]imaging.Plane, n)
		for p := range planes {
			var err error
			if planes[p], err = outputPlane(plan, y0, values[p][:rows*plan.Width]); err != nil {
				return err
			}
		}
		if err := b.AddPlanes(planes); err != nil {
			return err
		}
	}
	return nil
}

// provenanceOf is the result's provenance: the operation, named.
func provenanceOf(pr *prepared, id Digest) imaging.Provenance {
	names := make([]string, len(pr.descs))
	for i, in := range pr.descs {
		names[i] = in.Name
	}
	params := make([]string, 0, len(pr.decl.Params))
	for _, p := range pr.decl.Params {
		params = append(params, p.Name+"="+pr.canonical[p.Name].String())
	}
	note := "Operation " + pr.decl.Ref() + " on " + strings.Join(names, ", ")
	if len(params) > 0 {
		note += " with " + strings.Join(params, ", ")
	}
	note += "; request " + id.String()[:16] + "."
	return imaging.Provenance{
		SourceName: pr.decl.Id + " of " + strings.Join(names, ", "),
		Format:     "operation",
		Decoder:    pr.decl.Ref(),
		Notes:      append([]string{note}, pr.plan.Notes...),
	}
}

// Replay runs a record's request again on the given inputs and reports
// whether the output's digest is the recorded one. The inputs are named as
// the record names them; an input whose digest, transforms or declared
// invalid value differ from the record's, and an operator whose version is
// not the recorded one, are refused, since the run could not reproduce the
// record.
func Replay(ctx context.Context, rec Record, inputs []Input, opts Options) (res Result, match bool, err error) {
	inputs = slices.Clone(inputs)
	op, ok := Lookup(rec.Operator)
	if !ok {
		return res, false, eb.Build().Str("operator", rec.Operator).Errorf("no operator of the record's id: %w", ErrRefused)
	}
	if v := op.Declaration().Version; v != rec.Version {
		return res, false, eb.Build().Str("operator", rec.Operator).Uint32("version", v).Uint32("recorded", rec.Version).
			Errorf("the operator is of another version than the record's: %w", ErrRefused)
	}
	if len(inputs) != len(rec.Inputs) {
		return res, false, eb.Build().Int("inputs", len(inputs)).Int("recorded", len(rec.Inputs)).
			Errorf("the replay is given another number of inputs than the record names: %w", ErrRefused)
	}
	for _, in := range inputs {
		if in.Source == nil {
			continue
		}
		if err = in.Source.Wait(ctx); err != nil {
			return res, false, err
		}
	}
	for i, in := range inputs {
		want := rec.Inputs[i]
		if in.Source == nil {
			return res, false, eb.Build().Str("input", want.Name).Errorf("the replay is given no image for an input: %w", ErrRefused)
		}
		got := recordInput(describe(in), nil)
		switch {
		case in.Name != want.Name:
			return res, false, eb.Build().Str("input", in.Name).Str("recorded", want.Name).
				Errorf("the replay's inputs are not named as the record names them: %w", ErrRefused)
		case got.Digest != want.Digest || !slices.Equal(got.Transforms, want.Transforms) || !sameInvalid(got.Invalid, want.Invalid):
			return res, false, eb.Build().Str("input", in.Name).Stringer("digest", got.Digest).Stringer("recorded", want.Digest).
				Errorf("an input of the replay is not the recorded one: %w", ErrRefused)
		}
		if in.Record == nil {
			inputs[i].Record = want.Record
		}
	}
	res, err = Run(ctx, Request{Operator: rec.Operator, Params: rec.Params, Inputs: inputs}, opts)
	if err != nil {
		return res, false, err
	}
	return res, res.Record.Output.Digest == rec.Output.Digest, nil
}

func sameInvalid(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
