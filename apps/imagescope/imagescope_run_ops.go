package imagescope

// The operations' and the images' operations (ADR-0400 §M2, ADR-0269):
// what an agent reads of the operators, plans and runs, and how it shows,
// drops and reads the records of the window's images. The panel's Run,
// the image buttons and Drop call the same handlers as gestures.

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	imgops "github.com/stergiotis/boxer/public/science/imaging/ops"
)

const (
	resImages = "images"

	opListOperators   = "list_operators"
	opPlanOperation   = "plan_operation"
	opRunOperation    = "run_operation"
	opCancelOperation = "cancel_operation"
	opListImages      = "list_images"
	opShowImage       = "show_image"
	opDropImage       = "drop_image"
	opGetRecord       = "get_record"
)

// runOperations declares the images' resource and the operations of
// operators and images.
func runOperations(s *appops.Set[*App, opsSnap]) {
	s.Resource(resImages, "the window's images: the one opened and the results of operations, each with an id, and the one shown", func(inst *App) any {
		return imageListOf(inst.images, inst.shownId)
	})

	appops.Query(s, app.OperationSpec{Name: opListOperators, Version: 1,
		Summary: "list the operators an operation can apply, as each is declared: id, version, summary, the inputs it takes, and every parameter with its type, unit, range, choices, default and whether it is required; and the expression grammar's functions",
		Agents:  true, Fork: app.OperationForkReadShared},
		func(_ opsSnap, _ appops.None) (out OperatorList, err error) {
			for op := range imgops.All() {
				out.Operators = append(out.Operators, operatorInfoOf(op.Declaration()))
			}
			out.Functions, out.Grammar, out.ValueRules = imgops.ExpressionFunctions(), expressionGrammar, valueRules
			return
		})

	appops.Query(s, app.OperationSpec{Name: opPlanOperation, Version: 1,
		Summary: "state what an operation would make before a sample is read — size, kind, planes, the neighbourhood and border, the first passes, what it would take in memory — or why it cannot run",
		Reads:   []string{resImages, resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in RunArgs) (out PlanInfo, err error) {
			req, decl, err := sn.runSource().request(context.Background(), in)
			if err != nil {
				return out, err
			}
			plan, bytes, err := planOf(req)
			if err != nil {
				return out, err
			}
			return planInfoOf(decl, req, plan, bytes, heldResultBytes(sn.images)), nil
		})

	appops.Command(s, app.OperationSpec{Name: opRunOperation, Version: 1,
		Summary: "run an operation once at full resolution — an operator applied to images of the window, or planes of the stack opened at stated positions — as a task whose result is shown as it fills and joins list_images; the call settles with the result's id and a summary of its record",
		Effect:  app.OperationEffectDocument, Reads: []string{resImage}, Writes: []string{resImages, resView}, Agents: true, Async: true, DryRun: opPlanOperation,
		Gesture: "Run in the Operations section", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"one run at a time per window; cancel_operation stops one, and a cancelled or failed run leaves nothing",
			"the results held may take 1 GiB; a run past it is refused, and drop_image frees one",
			"what plan_operation refuses, this refuses with the same reason"}},
		func(inst *App, _ app.OperationCall, in RunArgs) (out appops.None, err error) {
			err = inst.startRun(in)
			return
		})
	appops.Settles(s, opRunOperation, func(inst *App, _ app.OperationCall) (out RunOutcome, settled bool, err error) {
		if inst.runJob.Running() || inst.runLanded != inst.runCallGen {
			return out, false, nil
		}
		if inst.runErr != "" || inst.lastResult == "" {
			out.Error = inst.runErr
			if out.Error == "" {
				out.Error = inst.runNote
			}
			return out, true, nil
		}
		e, ok := entryOf(inst.images, inst.lastResult)
		if !ok {
			return RunOutcome{Error: "the result was dropped"}, true, nil
		}
		sum := recordSummaryOf(*e.record)
		return RunOutcome{Image: e.id, Description: describeEntry(e), Record: &sum}, true, nil
	})
	s.Available(opRunOperation, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case len(sn.images) == 0:
			return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
		case sn.running:
			return false, "a run is under way; cancel_operation stops it"
		}
		return true, ""
	})

	appops.Command(s, app.OperationSpec{Name: opCancelOperation, Version: 1,
		Summary: "cancel the run under way; it leaves nothing, and what was shown before it is shown again",
		Effect:  app.OperationEffectView, Writes: []string{resImages}, Agents: true,
		Gesture: "Cancel on the run's row", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			if !inst.runJob.Running() {
				return out, app.RefuseOperation("no run is under way")
			}
			inst.runJob.Cancel()
			return
		})

	appops.Query(s, app.OperationSpec{Name: opListImages, Version: 1,
		Summary: "list the window's images — the one opened, and the results of operations with the operator that made each — with the one shown and what the results hold in memory",
		Reads:   []string{resImages}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out ImageList, err error) {
			return imageListOf(sn.images, sn.shownId), nil
		})

	appops.Command(s, app.OperationSpec{Name: opShowImage, Version: 1,
		Summary: "show an image of the window; one of the same size keeps the view and the marked region",
		Effect:  app.OperationEffectView, Writes: []string{resView, resImages}, Agents: true,
		Gesture: "the buttons of the Images row", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"the regions apply to every image of their frame; get_view and list_regions say which are drawn on the one shown"}},
		func(inst *App, _ app.OperationCall, in ImageIdArgs) (out appops.None, err error) {
			err = inst.showImage(in.Id)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opDropImage, Version: 1,
		Summary: "drop a result from the window and free what it holds; the image opened is not dropped",
		Effect:  app.OperationEffectDocument, Writes: []string{resImages, resView}, Agents: true,
		Gesture: "Drop beside the Images row", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"a result shown is replaced by the image opened", "a result made from the one dropped keeps its record, which names it"}},
		func(inst *App, _ app.OperationCall, in ImageIdArgs) (out appops.None, err error) {
			err = inst.dropImage(in.Id)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetRecord, Version: 1,
		Summary: "read a result's record, the versioned JSON document of what its operation did — operator, parameters, inputs by digest and position, first-pass quantities, the result's digest and the records of inputs that are results — at most 256 KiB at a time",
		Reads:   []string{resImages}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetRecordArgs) (out RecordPage, err error) {
			e, ok := entryOf(sn.images, in.Id)
			switch {
			case !ok:
				return out, app.RefuseOperation("no image " + quotedList([]string{in.Id}) + "; list_images lists them")
			case e.record == nil:
				return out, app.RefuseOperation("image " + in.Id + " was opened, not made by an operation, so it has no record; its provenance is in get_view's Image")
			}
			doc, err := e.record.Encode()
			if err != nil {
				return out, refuse(err)
			}
			from := min(max(in.From, 0), len(doc))
			to := min(len(doc), from+recordPageMax)
			return RecordPage{Image: e.id, Bytes: len(doc), From: from, Record: string(doc[from:to]), Truncated: to < len(doc)}, nil
		})
	for _, op := range []string{opShowImage, opDropImage} {
		s.Available(op, func(sn opsSnap) (ok bool, reason string) {
			if len(sn.images) == 0 {
				return false, "there is no image; open_sample opens one, or the person opens a file with Open…"
			}
			return true, ""
		})
	}
}
