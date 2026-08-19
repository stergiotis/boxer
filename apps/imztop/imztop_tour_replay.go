package imztop

import (
	"context"
	"iter"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Replay-mode capture for the screenshot tour (ADR-0197 M5).
//
// The scene shows the mode's whole surface — the transport, the REPLAY banner,
// the availability strip with its bands and load rug, and the panels drawing
// replayed frames — without a database. Everything comes from the same
// synthetic generator the live scenes use ([tourSampler]), which is what keeps
// the capture reproducible and keeps the tour from needing a network flag.
//
// The session is process-wide (§SD5), and the TestDriver renders every scene in
// one process. So each imztop scene asserts the mode it wants at render time
// rather than inheriting whatever the previous capture left installed — the same
// discipline the dock tab already follows, and for the same reason: the driver
// captures scenes sorted by name, so ordering is not a thing to rely on.

// tourReplaySpan is the window the replay scene shows. Long enough for the
// strip's axis to carry several tick labels, short enough that the synthetic
// feed fills it at a plausible cadence.
const tourReplaySpan = 30 * time.Minute

// tourReplayBundles is how many frames the synthetic window holds. It sits well
// inside the fold's capacity, so the scene captures the ordinary case rather
// than the decimated one.
const tourReplayBundles = 120

// tourReplaySource is a [BundleSourceI] over synthetic bundles: the tour's
// generator, stamped across the requested window instead of ticking in real
// time.
type tourReplaySource struct{}

var _ BundleSourceI = (*tourReplaySource)(nil)

// All yields tourReplayBundles evenly spaced across the window.
//
// The stamps are the window's, so the replayed frames carry historical times
// and the plots' axis reads as the recorded run rather than as the capture —
// the property §SD3 rests on, visible in the capture itself.
func (inst *tourReplaySource) All(_ context.Context, w sysmreplay.Window) iter.Seq2[*sysmsnap.BundleSnapshot, error] {
	return func(yield func(*sysmsnap.BundleSnapshot, error) bool) {
		from, to := w.From, w.To
		if from.IsZero() || !to.After(from) {
			to = time.Now().UTC()
			from = to.Add(-tourReplaySpan)
		}
		gen := newTourSampler()
		step := to.Sub(from) / tourReplayBundles
		for i := range tourReplayBundles {
			snap, err := gen.Sample(context.Background())
			if err != nil {
				yield(nil, err)
				return
			}
			// Restamp onto the window: the generator dates its bundles now,
			// and a replay's frames must carry the times they were recorded at.
			snap.SampledAtUnixMs = from.Add(time.Duration(i) * step).UnixMilli()
			// Drop what the tee does not store (§SD8), so the capture shows
			// the honest replay surface: a "not recorded" host badge and an
			// empty Sensors tab that says why. A synthetic source that kept
			// these would teach a replay that cannot exist.
			snap.Container = nil
			snap.Sensors = nil
			snap.Errors = map[sysmsnap.Domain]error{}
			if !yield(&snap, nil) {
				return
			}
		}
	}
}

// ensureTourReplay installs a synthetic replay session and the availability it
// draws, and returns the sampler the scene should render.
//
// Idempotent: a scene rendered over several settle frames must not restart the
// transport on each one, or the capture would catch it at the first frame every
// time.
func ensureTourReplay() (s SamplerI) {
	if session := ActiveReplay(); session != nil {
		return session
	}
	// The replay window sits inside the later recorded stretch, and the strip
	// is seeded over its own context span — otherwise the bands land in a
	// sliver at the right edge of a six-hour axis and the capture shows a
	// feature nobody could read.
	ctxTo := time.Now().UTC()
	ctxFrom := ctxTo.Add(-availabilityContextSpan)
	to := ctxTo.Add(-availabilityContextSpan / 12)
	from := to.Add(-tourReplaySpan)
	session, err := NewReplaySampler(ReplayOptions{
		Source: &tourReplaySource{},
		Window: sysmreplay.Window{From: from, To: to},
		// Fast enough that the fold has filled by the time the driver settles,
		// so the capture shows populated plots rather than a first frame.
		Speed: 2000,
	})
	if err != nil {
		return nil
	}
	session.Start(context.Background())

	// The store source is nil — there is no database here — so the coverage
	// pass would find nothing to query. Seed the strip directly instead, with
	// the shape a real host produces: two recorded stretches with a gap
	// between them, which is what makes the bands worth drawing at all.
	if !beginOpening() {
		_ = session.Close()
		return ActiveReplay()
	}
	if !installReplay(session, nil, "tour-host", "synthetic", func() {}) {
		_ = session.Close()
		return ActiveReplay()
	}
	setTourCoverage(ctxFrom, ctxTo)
	return session
}

// ensureTourLive takes the process back to live data, so a live scene captures
// live chrome whatever ran before it.
func ensureTourLive() {
	if ActiveReplay() != nil || CurrentReplayStatus().State != ReplayOff {
		_ = LeaveReplay()
	}
}

// setTourCoverage seeds the availability bands and the load rug across the
// strip's whole context span, with the shape a real host produces: two
// stretches with an outage between them, and a load curve that rises and falls
// so the rug's intensity encoding has something to encode.
func setTourCoverage(from, to time.Time) {
	span := to.Sub(from)
	runs := []sysmreplay.CoverageRun{
		{
			StartMS: from.Add(span / 12).UnixMilli(),
			EndMS:   from.Add(span * 5 / 12).UnixMilli(),
			Rows:    14400,
		},
		{
			StartMS: from.Add(span * 7 / 12).UnixMilli(),
			EndMS:   to.UnixMilli(),
			Rows:    17800,
		},
	}
	const bins = 90
	points := make([]sysmreplay.PreviewPoint, 0, bins)
	step := span / bins
	for i := range bins {
		at := from.Add(time.Duration(i) * step)
		ms := at.UnixMilli()
		// Only where the tee was running: a load reading outside a covered
		// stretch would claim a measurement nobody took.
		covered := false
		for _, r := range runs {
			if ms >= r.StartMS && ms < r.EndMS {
				covered = true
				break
			}
		}
		if !covered {
			continue
		}
		points = append(points, sysmreplay.PreviewPoint{
			StartMS: ms,
			Value:   tourLoadCurve(i, bins),
		})
	}
	seedCoverage(runs, points, from, to)
}

// tourLoadCurve is a synthetic busy-percent shape: a broad hump with a second
// smaller one, so the rug shows structure rather than a flat band.
func tourLoadCurve(i, n int) (pct float64) {
	x := float64(i) / float64(n)
	hump := func(centre, width, height float64) float64 {
		d := (x - centre) / width
		return height / (1 + d*d)
	}
	pct = 8 + hump(0.22, 0.10, 62) + hump(0.72, 0.07, 38)
	return min(pct, 100)
}
