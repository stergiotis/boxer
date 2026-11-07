package writingstylescope

import (
	"fmt"
	"hash/fnv"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/fsmops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
)

// The operations catalog (ADR-0269): what an agent under a task grant reads
// and sets in a writingstylescope window. The two documents and the floor
// are the resources; the queries answer from a snapshot taken after the
// command stage and are bounded; the commands set values and never toggle
// them. The sweep is a task already, so analyze is asynchronous and settles
// when the sweep lands (ADR-0358); the handover writes outside the window
// and is consequential. The buttons go through the same handlers
// (ADR-0269 §SD8).

const (
	resDocuments = "documents"
	resFloor     = "floor"

	opGetView         = "get_view"
	opGetPairs        = "get_pairs"
	opGetDistribution = "get_distribution"
	opSetDocument     = "set_document"
	opSetFloor        = "set_floor"
	opAnalyze         = "analyze"
	opOpenInPlay      = "open_in_play"

	// Result bounds: what one inline result may carry.
	opsMaxPairs         = 200
	opsDefaultPairs     = maxRankedPairs
	opsMaxQuantiles     = 101
	opsDefaultQuantiles = 21
	opsMinQuantiles     = 2
)

// Document names set_document takes.
const (
	docA = "A"
	docB = "B"
)

// Documents is the documents resource's value: the two drafts as the panes
// hold them.
type Documents struct {
	A string `desc:"document A, the matrix's rows"`
	B string `desc:"document B, the matrix's columns"`
}

// documentRevision names a text: equal texts, equal revisions. A write
// quotes the revision it read, and a text that moved on since is refused.
func documentRevision(src string) (rev string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(src))
	return strconv.FormatUint(h.Sum64(), 16)
}

// View is get_view's result: the phase, the inputs by size and revision,
// and the document-level readings, without the matrix.
type View struct {
	Phase         string  `desc:"the analysis's phase: no result, sweeping, result, stale or failed"`
	BytesA        int     `desc:"document A's length in bytes"`
	BytesB        int     `desc:"document B's length in bytes"`
	RevisionA     string  `desc:"names document A's text; set_document quotes it"`
	RevisionB     string  `desc:"names document B's text; set_document quotes it"`
	Floor         int     `desc:"the section-length floor in bytes"`
	Running       bool    `desc:"a sweep is in flight"`
	Stale         bool    `desc:"the result was computed from other text or another floor"`
	Error         string  `desc:"why the last sweep refused; empty otherwise"`
	HasResult     bool    `desc:"get_pairs and get_distribution have a matrix to read"`
	SectionsA     int     `desc:"A sections that cleared the floor and were swept"`
	SectionsB     int     `desc:"B sections that cleared the floor and were swept"`
	DroppedA      int     `desc:"A sections below the floor"`
	DroppedB      int     `desc:"B sections below the floor"`
	Pairs         int     `desc:"cells in the matrix"`
	ElapsedMs     float64 `desc:"the sweep's wall-clock, milliseconds"`
	ProfileNcd    float64 `desc:"profile-mode NCD of A against B truncated to A's length: the documents as wholes"`
	ProfileCcc    float64 `desc:"conditional complexity of compression, extra compressed bytes B costs once A is known"`
	InstCount     int64   `desc:"B sections the instance sweep measured before it stopped"`
	InstMin       float64 `desc:"instance-mode minimum NCD"`
	InstMean      float64 `desc:"instance-mode mean NCD"`
	InstMax       float64 `desc:"instance-mode maximum NCD"`
	InstStdDev    float64 `desc:"instance-mode standard deviation of NCD"`
	InstConverged bool    `desc:"the instance sweep stopped early because its statistic stabilised"`
	HandoverBusy  bool    `desc:"an Open in play request is in flight"`
	HandoverNote  string  `desc:"what the last handover opened; empty when none"`
	HandoverError string  `desc:"why the last handover failed; empty otherwise"`
	Method        string  `desc:"what the numbers are and what they assume"`
}

// GetPairsArgs bounds get_pairs.
type GetPairsArgs struct {
	Limit int `json:",omitzero" desc:"pairs to return, closest first; default 25, at most 200"`
}

// PairRow is one cell of the matrix, ranked.
type PairRow struct {
	Rank     int     `desc:"1 for the closest pair"`
	AIndex   int     `desc:"row: the A section's index among the swept sections"`
	BIndex   int     `desc:"column: the B section's index among the swept sections"`
	ASection string  `desc:"the A section's heading, or (preamble)"`
	BSection string  `desc:"the B section's heading, or (preamble)"`
	ABytes   int     `desc:"the A section's length"`
	BBytes   int     `desc:"the B section's length"`
	Ncd      float64 `desc:"normalized compression distance; low is close"`
	Beats    float64 `desc:"fraction of all pairs this one is strictly closer than: 1 minus the fraction at or below its distance, so the closest pair reads highest"`
}

// PairsResult is get_pairs's result.
type PairsResult struct {
	Total     int       `desc:"cells in the matrix"`
	Pairs     []PairRow `desc:"the first Limit pairs, ascending by distance; ties keep row-major order"`
	Truncated bool      `desc:"the matrix has more pairs than Pairs holds"`
	Stale     bool      `desc:"the matrix was computed from other text or another floor"`
	Method    string    `desc:"what Ncd and Beats are and what they assume"`
}

// GetDistributionArgs bounds get_distribution.
type GetDistributionArgs struct {
	Points int `json:",omitzero" desc:"quantiles to return, evenly spaced from 0 to 1; default 21, at least 2, at most 101"`
}

// QuantileRow is one point of the empirical distribution.
type QuantileRow struct {
	Q   float64 `desc:"the fraction of pairs at or below Ncd"`
	Ncd float64 `desc:"the distance at that fraction, by nearest rank"`
}

// Distribution is get_distribution's result.
type Distribution struct {
	N         int           `desc:"finite cells in the matrix"`
	Min       float64       `desc:"the closest pair's distance"`
	Median    float64       `desc:"the median pair's distance"`
	Max       float64       `desc:"the farthest pair's distance"`
	Quantiles []QuantileRow `desc:"Points quantiles, Q ascending"`
	Truncated bool          `desc:"more points were asked for than the cap allows"`
	Stale     bool          `desc:"the matrix was computed from other text or another floor"`
	Method    string        `desc:"what the distribution is and what it does not say"`
}

// SetDocumentArgs is set_document's argument.
type SetDocumentArgs struct {
	Which    string `desc:"A or B"`
	Revision string `desc:"the revision get_view returned for that document"`
	Text     string `desc:"the markdown that replaces it, at most 1 MiB"`
}

// SetFloorArgs is set_floor's argument.
type SetFloorArgs struct {
	Floor float64 `desc:"the section-length floor in bytes, 0 to 4000"`
}

// AnalyzeOutcome is what analyze settles with.
type AnalyzeOutcome struct {
	Phase     string `desc:"result, stale, failed, or no result after a cancel with nothing to keep"`
	Cancelled bool   `desc:"the sweep was cancelled; the result, if any, is the previous one"`
	Error     string `desc:"why the sweep refused; empty otherwise"`
	SectionsA int    `desc:"A sections swept"`
	SectionsB int    `desc:"B sections swept"`
	Pairs     int    `desc:"cells in the matrix"`
}

// methodLine states the method where the numbers are (keelson-app F3).
const methodLine = "Normalized compression distance with zstd: how much less two texts compress together than apart, " +
	"over raw markdown with the front matter removed, one section per heading. It is a distance, not a probability, " +
	"and has no absolute scale across subjects, languages or section lengths; Beats places a pair in this pair of " +
	"documents' own background, which is the only reference the window asserts. Copied or lightly edited text scores low; " +
	"a passage rewritten in other words does not."

// distributionMethod is get_distribution's caveat.
const distributionMethod = "The empirical distribution of every pair's distance; its bulk is what unrelated sections of these two " +
	"documents look like, and a shared section is a point detached from it on the left. The pairs share sections and are not " +
	"independent, so no confidence statement is made here."

// opsSnap is what queries read: a copy taken after the command stage. The
// result pointer is to an immutable Analysis, replaced and never edited.
type opsSnap struct {
	docs     Documents
	floor    float64
	phase    phaseE
	running  bool
	stale    bool
	err      string
	res      *Analysis
	handover handoverSnap
}

type handoverSnap struct {
	busy bool
	note string
	err  string
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn = opsSnap{docs: Documents{A: inst.docA, B: inst.docB}, floor: inst.minSectionBytes, phase: inst.observePhase(),
		running: inst.sweep.Running(), stale: inst.stale(), res: inst.res}
	if inst.err != nil {
		sn.err = inst.err.Error()
	}
	sn.handover.busy, sn.handover.note, sn.handover.err = inst.handoverState()
	return
}

// checkDocument refuses a write that does not fit: an unknown document, a
// stale revision or a text over the pane's cap.
func (inst *App) checkDocument(in SetDocumentArgs) (target *string, err error) {
	switch in.Which {
	case docA:
		target = &inst.docA
	case docB:
		target = &inst.docB
	default:
		err = app.RefuseOperation("which is A or B")
		return
	}
	if in.Revision != documentRevision(*target) {
		err = app.RefuseOperation("document " + in.Which + " changed since that revision; read it again with get_view")
		return
	}
	if len(in.Text) > maxPaneBytes {
		err = app.RefuseOperation(fmt.Sprintf("document %s is over the pane's cap of %d bytes", in.Which, maxPaneBytes))
		return
	}
	return
}

// setFloor sets the floor, refusing a value outside the slider's range.
func (inst *App) setFloor(f float64) (err error) {
	if !(f >= minMinSectionBytes) || f > maxMinSectionBytes {
		return app.RefuseOperation(fmt.Sprintf("floor is between %d and %d bytes", minMinSectionBytes, maxMinSectionBytes))
	}
	inst.minSectionBytes = float64(clampFloor(f))
	return
}

// analyze is the Analyze button's handler and analyze's: it sweeps, or
// queues the sweep behind the one running, and records the sweep
// generation the asynchronous call waits for.
func (inst *App) analyze() {
	queued := inst.sweep.Running()
	inst.run()
	inst.analyzeWant = inst.sweepSeq
	if queued {
		inst.analyzeWant = inst.sweepSeq + 1
	}
}

// pairsOf renders the matrix as get_pairs's bounded answer, ranked over
// every cell rather than the panel's 25.
func pairsOf(res *Analysis, limit int, stale bool) (out PairsResult) {
	out = PairsResult{Total: len(res.Ncd), Stale: stale, Method: methodLine}
	ranked := rankPairs(res.Ncd, res.Cols(), limit)
	out.Pairs = make([]PairRow, 0, len(ranked))
	for k, pr := range ranked {
		out.Pairs = append(out.Pairs, PairRow{Rank: k + 1, AIndex: pr.I, BIndex: pr.J,
			ASection: res.SecA[pr.I].Label(), BSection: res.SecB[pr.J].Label(),
			ABytes: res.SecA[pr.I].Bytes(), BBytes: res.SecB[pr.J].Bytes(),
			Ncd: pr.Ncd, Beats: 1 - res.Quantile(pr.Ncd)})
	}
	out.Truncated = out.Total > len(out.Pairs)
	return
}

// distributionOf renders the sorted sample as points quantiles.
func distributionOf(res *Analysis, points int, truncated bool, stale bool) (out Distribution) {
	out = Distribution{N: len(res.Sorted), Min: res.Min(), Max: res.Max(), Median: quantileOf(res.Sorted, 0.5),
		Truncated: truncated, Stale: stale, Method: distributionMethod}
	out.Quantiles = make([]QuantileRow, 0, points)
	for k := range points {
		q := float64(k) / float64(points-1)
		out.Quantiles = append(out.Quantiles, QuantileRow{Q: q, Ncd: quantileOf(res.Sorted, q)})
	}
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resDocuments, "the two documents as the panes hold them", func(inst *App) any { return Documents{A: inst.docA, B: inst.docB} })
	s.Restorable(resDocuments, func(inst *App, v any) bool {
		d, ok := v.(Documents)
		if ok {
			inst.docA, inst.docB = d.A, d.B
		}
		return ok
	})
	s.Editing(resDocuments, func(inst *App) bool {
		return appops.WidgetEditing(inst.paneHandles[0]) || appops.WidgetEditing(inst.paneHandles[1])
	})
	s.Resource(resFloor, "the section-length floor in bytes", func(inst *App) any { return inst.minSectionBytes })
	s.Restorable(resFloor, func(inst *App, v any) bool {
		f, ok := v.(float64)
		if ok {
			inst.minSectionBytes = f
		}
		return ok
	})
	s.Editing(resFloor, func(inst *App) bool { return appops.WidgetEditing(inst.floorHandle) })

	fsmops.Mount(s, "analysis", "the analysis's phase, from the first sweep to a stale result", func(inst *App) opfsm.SourceI {
		if inst.phaseMachine == nil {
			return nil
		}
		return inst.phaseMachine
	}, fsmops.Options{History: 16})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the phase, the documents' sizes and revisions, the floor and the document-level readings, without the matrix",
		Reads:   []string{resDocuments, resFloor}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Phase: sn.phase.String(), BytesA: len(sn.docs.A), BytesB: len(sn.docs.B),
				RevisionA: documentRevision(sn.docs.A), RevisionB: documentRevision(sn.docs.B), Floor: clampFloor(sn.floor),
				Running: sn.running, Stale: sn.stale, Error: sn.err,
				HandoverBusy: sn.handover.busy, HandoverNote: sn.handover.note, HandoverError: sn.handover.err, Method: methodLine}
			if r := sn.res; r != nil {
				out.HasResult = true
				out.SectionsA, out.SectionsB, out.DroppedA, out.DroppedB = r.Rows(), r.Cols(), r.DroppedA, r.DroppedB
				out.Pairs = len(r.Ncd)
				out.ElapsedMs = float64(r.Elapsed.Microseconds()) / 1000
				out.ProfileNcd, out.ProfileCcc = r.ProfileNcd, r.ProfileCcc
				out.InstCount, out.InstMin, out.InstMean, out.InstMax, out.InstStdDev, out.InstConverged =
					r.InstCount, r.InstMin, r.InstMean, r.InstMax, r.InstStdDev, r.InstConverged
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetPairs, Version: 1,
		Summary: "read the closest section pairs of the whole matrix, at most 200, with where each sits in the background",
		Reads:   []string{resDocuments, resFloor}, Agents: true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetPairsArgs) (out PairsResult, err error) {
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsDefaultPairs
			case limit > opsMaxPairs:
				limit = opsMaxPairs
			}
			if err = refuseWithoutResult(sn); err != nil {
				return
			}
			out = pairsOf(sn.res, limit, sn.stale)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetDistribution, Version: 1,
		Summary: "read the distribution of every pair's distance as quantiles, at most 101 points",
		Reads:   []string{resDocuments, resFloor}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetDistributionArgs) (out Distribution, err error) {
			points, truncated := in.Points, false
			switch {
			case points == 0:
				points = opsDefaultQuantiles
			case points < opsMinQuantiles:
				err = app.RefuseOperation(fmt.Sprintf("points is at least %d", opsMinQuantiles))
				return
			case points > opsMaxQuantiles:
				points, truncated = opsMaxQuantiles, true
			}
			if err = refuseWithoutResult(sn); err != nil {
				return
			}
			if len(sn.res.Sorted) == 0 {
				err = app.RefuseOperation("the matrix has no finite cell")
				return
			}
			out = distributionOf(sn.res, points, truncated, sn.stale)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetDocument, Version: 1,
		Summary: "replace document A or B, quoting the revision get_view returned for it; the result goes stale until analyze",
		Effect:  app.OperationEffectDocument, Writes: []string{resDocuments}, Agents: true,
		Gesture: "typing or pasting in the pane", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance,
		Follows: []string{"a text over 1 MiB is refused naming the cap; a stale revision is refused, read get_view again"}},
		func(inst *App, _ app.OperationCall, in SetDocumentArgs) (out appops.None, err error) {
			target, err := inst.checkDocument(in)
			if err != nil {
				return
			}
			*target = in.Text
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSetFloor, Version: 1,
		Summary: "set the section-length floor in bytes, 0 to 4000; the result goes stale until analyze",
		Effect:  app.OperationEffectDocument, Writes: []string{resFloor}, Agents: true,
		Gesture: "the floor slider", Rebuild: app.OperationRebuildVolatile, Fork: app.OperationForkPerInstance},
		func(inst *App, _ app.OperationCall, in SetFloorArgs) (out appops.None, err error) {
			err = inst.setFloor(in.Floor)
			return
		})

	appops.Command(s, app.OperationSpec{Name: opAnalyze, Version: 1,
		Summary: "sweep the two documents; the call settles when the sweep lands or refuses",
		Effect:  app.OperationEffectRun, Reads: []string{resDocuments, resFloor}, Agents: true, Async: true,
		Gesture: "the Analyze button", Fork: app.OperationForkPerInstance,
		Follows: []string{"the matrix replaces the previous one when it lands; get_pairs and get_distribution read it",
			"a refusal (a pane over 1 MiB, too few sections over the floor, too many sections) leaves no matrix and names the remedy"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			inst.analyze()
			return
		})
	appops.Settles(s, opAnalyze, func(inst *App, _ app.OperationCall) (out AnalyzeOutcome, settled bool, err error) {
		if inst.landedSeq < inst.analyzeWant || inst.sweep.Running() {
			return out, false, nil
		}
		out = AnalyzeOutcome{Phase: inst.observePhase().String(), Cancelled: inst.cancelled}
		if inst.err != nil {
			out.Error = inst.err.Error()
		}
		if r := inst.res; r != nil {
			out.SectionsA, out.SectionsB, out.Pairs = r.Rows(), r.Cols(), len(r.Ncd)
		}
		return out, true, nil
	})
	s.Available(opAnalyze, func(sn opsSnap) (ok bool, reason string) {
		if sn.running {
			return false, "a sweep is in flight; it settles through the running call"
		}
		return true, ""
	})

	appops.Command(s, app.OperationSpec{Name: opOpenInPlay, Version: 1,
		Summary: "publish the whole matrix as an ad-hoc dataset and open the playground on it, seeded with the ranked query",
		Effect:  app.OperationEffectConsequential, Reads: []string{resDocuments, resFloor}, Agents: true,
		Gesture: "the Open in play button", Fork: app.OperationForkPerInstance,
		Follows: []string{"the request runs off the frame; get_view's handover fields report what opened or why it failed"}},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			if inst.res == nil {
				err = app.RefuseOperation("there is no result to publish; analyze first")
				return
			}
			if inst.bus == nil {
				err = app.RefuseOperation("no bus on this host; the handover needs the app runtime")
				return
			}
			inst.startHandover()
			return
		})
	s.Available(opOpenInPlay, func(sn opsSnap) (ok bool, reason string) {
		switch {
		case sn.res == nil:
			return false, "there is no result to publish; analyze first"
		case sn.handover.busy:
			return false, "a handover is in flight"
		}
		return true, ""
	})
	return
}()

// refuseWithoutResult names the remedy when a matrix query has nothing to
// read.
func refuseWithoutResult(sn opsSnap) (err error) {
	if sn.res != nil {
		return
	}
	switch sn.phase {
	case phaseSweeping:
		return app.RefuseOperation("the sweep is running; read get_view until its phase is result")
	case phaseFailed:
		return app.RefuseOperation("the last sweep refused: " + sn.err + "; change the documents or the floor and analyze again")
	}
	return app.RefuseOperation("there is no result yet; analyze launches a sweep")
}

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)

// gestureOr routes the person's control through the catalog (ADR-0269
// §SD8), so the command log sees it as theirs, and applies it directly where
// no host serves the catalog (the tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}
