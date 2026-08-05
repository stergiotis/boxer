package play

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_series_fixture.go is ADR-0163 M4: the fixture lab.
//
// It publishes a labelled synthetic series as two ORDINARY ad-hoc datasets
// (ADR-0134) — `fixture_series(t, v)` and `fixture_truth(span_from, span_to,
// kind)` — and then gets out of the way. There is no demo mode, no special
// path, no panel that knows it is looking at a fixture: you query the tables
// with the same SQL you would write against anything else, and every part of
// the workbench behaves exactly as it does on real data. A demo mode would
// have been easier and would have taught the wrong thing, because the one
// question a workbench must answer honestly is what it does on YOUR data.
//
// What the fixture buys is GROUND TRUTH. M3's readout scores a detector
// against a human's adjudication, which is the real target but also a slow
// and arguable one; `fixture_truth` is a second opinion nobody has to earn —
// the spans really were planted, at known positions. Comparing the two is how
// you find out whether the adjudication UI is measuring what you think.
//
// The generator is adscore's, deliberately: its fixtures are built to avoid
// the four flaws that let a trivial detector look good on synthetic data
// (ADR-0150), so a detector that wins here has not simply learned that
// anomalies come last.

const (
	// fixtureSeriesAlias / fixtureTruthAlias are the aliases a buffer writes
	// as keelson('<alias>'). Fixed names, because the point is that a fixture
	// query is an ordinary query someone can read, paste and re-run.
	fixtureSeriesAlias = "fixture_series"
	fixtureTruthAlias  = "fixture_truth"
	// fixtureStepSec is the synthetic grid's spacing. The generator produces
	// values, not times; a minute is the step that makes a default fixture
	// span a readable handful of days rather than a blur or a geological era.
	fixtureStepSec = 60
	// fixturePublishTimeout bounds one publish round.
	fixturePublishTimeout = 30 * time.Second
	// fixturePublisher attributes the datasets in the ad-hoc catalog.
	fixturePublisher = "play/series-fixture"
)

// fixtureEpoch is the synthetic series' first timestamp. A FIXED instant, not
// now(): a fixture is meant to be reproducible from (kind, seed), and a series
// whose timestamps moved every run would give the same data two identities —
// which the M3 labels, keyed on the compiled input, would then read as two
// different series.
var fixtureEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fixtureSpec is what the affordance collects: which anomaly kind, and the
// seed. Everything else takes adscore's defaults, whose density and tail
// exclusion are already set to what real recordings look like.
type fixtureSpec struct {
	kind adscore.AnomalyKindE
	seed uint64
}

// fixtureState is the lab's state: the last publish's outcome, held for the
// chrome to report.
type fixtureState struct {
	mu         sync.Mutex
	publishing bool
	err        error
	// summary describes the last successful publish, for the chrome.
	summary string
	// generation counts successful publishes, so the caller can re-bind and
	// re-run exactly once per publish.
	generation uint64
}

func (inst *fixtureState) status() (publishing bool, summary string, gen uint64, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.publishing, inst.summary, inst.generation, inst.err
}

// buildFixtureArrow renders one generated fixture as the two Arrow IPC
// streams the ad-hoc publish takes.
//
// The time column is DateTime64-shaped (a millisecond UTC timestamp) rather
// than an integer, because that is what a temporal claim can actually read:
// a ClickHouse DateTime arrives as a bare uint32 and cannot be told from a
// count (ADR-0163 Update 2026-08-05). A fixture that published an unusable
// time axis would fail in the one place it exists to work.
func buildFixtureArrow(fixture *adscore.Fixture, kind adscore.AnomalyKindE, alloc memory.Allocator) (series []byte, truth []byte, err error) {
	series, err = encodeFixtureSeries(fixture, alloc)
	if err != nil {
		return
	}
	truth, err = encodeFixtureTruth(fixture, kind, alloc)
	return
}

func encodeFixtureSeries(fixture *adscore.Fixture, alloc memory.Allocator) (out []byte, err error) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "t", Type: tsTimeType()},
		{Name: "v", Type: arrow.PrimitiveTypes.Float64},
	}, nil)
	tb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	vb := array.NewFloat64Builder(alloc)
	defer tb.Release()
	defer vb.Release()
	for i, v := range fixture.Values {
		tb.Append(arrow.Timestamp(fixtureSampleMS(i)))
		vb.Append(v)
	}
	ta, va := tb.NewArray(), vb.NewArray()
	defer ta.Release()
	defer va.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{ta, va}, int64(len(fixture.Values)))
	defer rec.Release()
	return encodeArrowStream(rec)
}

// encodeFixtureTruth renders the planted extents. Their columns are the
// Timeline band contract's `_tl_band_*` names plus `kind`, so the ground truth
// draws as bands with no translation — the same contract tsAnomalySpans emits,
// which is what lets a reader put the two pictures side by side.
func encodeFixtureTruth(fixture *adscore.Fixture, kind adscore.AnomalyKindE, alloc memory.Allocator) (out []byte, err error) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: timelineSlotBandFrom, Type: tsTimeType()},
		{Name: timelineSlotBandTo, Type: tsTimeType()},
		{Name: timelineSlotBandLabel, Type: arrow.BinaryTypes.String},
		{Name: timelineSlotBandColor, Type: arrow.BinaryTypes.String},
		{Name: "kind", Type: arrow.BinaryTypes.String},
	}, nil)
	fb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	tb := array.NewTimestampBuilder(alloc, tsTimeType().(*arrow.TimestampType))
	lb := array.NewStringBuilder(alloc)
	cb := array.NewStringBuilder(alloc)
	kb := array.NewStringBuilder(alloc)
	defer fb.Release()
	defer tb.Release()
	defer lb.Release()
	defer cb.Release()
	defer kb.Release()
	runs := fixtureTruthRuns(fixture.Labels)
	for i, run := range runs {
		fb.Append(arrow.Timestamp(fixtureSampleMS(run[0])))
		tb.Append(arrow.Timestamp(fixtureSampleMS(run[1])))
		lb.Append(fmt.Sprintf("planted #%d", i+1))
		// A token NAME, never a hex literal: the band reader resolves names
		// against a fixed map and draws nothing for one it cannot resolve.
		cb.Append("success.default")
		kb.Append(kind.String())
	}
	fa, ta, la, ca, ka := fb.NewArray(), tb.NewArray(), lb.NewArray(), cb.NewArray(), kb.NewArray()
	defer fa.Release()
	defer ta.Release()
	defer la.Release()
	defer ca.Release()
	defer ka.Release()
	rec := array.NewRecordBatch(schema, []arrow.Array{fa, ta, la, ca, ka}, int64(len(runs)))
	defer rec.Release()
	return encodeArrowStream(rec)
}

// fixtureTruthRuns collapses the per-position truth flags into inclusive
// [lo, hi] runs — the planted extents as extents, which is the form the band
// contract and the M3 labels both speak.
func fixtureTruthRuns(labels []bool) (runs [][2]int) {
	return warmUpRuns(labels)
}

// fixtureSampleMS is sample i's timestamp, in epoch milliseconds.
func fixtureSampleMS(i int) int64 {
	return fixtureEpoch.UnixMilli() + int64(i)*fixtureStepSec*1000
}

// encodeArrowStream serialises one record as an Arrow IPC stream, which is
// what PublishInput carries.
func encodeArrowStream(rec arrow.RecordBatch) (out []byte, err error) {
	buf := &bytes.Buffer{}
	w := ipc.NewWriter(buf, ipc.WithSchema(rec.Schema()))
	if err = w.Write(rec); err != nil {
		return nil, eh.Errorf("play: fixture: arrow write: %w", err)
	}
	if err = w.Close(); err != nil {
		return nil, eh.Errorf("play: fixture: arrow close: %w", err)
	}
	return buf.Bytes(), nil
}

// generateFixture builds the labelled series from a spec. Pure and
// deterministic in (kind, seed) — which is what makes a fixture something two
// people can talk about rather than something one of them saw once.
func generateFixture(spec fixtureSpec) (fixture *adscore.Fixture, err error) {
	// The kind is checked against the roster rather than trusted. adscore
	// accepts an out-of-range value and generates SOMETHING, which would make
	// a fixture whose (kind, seed) no longer describes it — and a fixture that
	// does not match its own label is worse than no fixture at all.
	known := false
	for _, k := range adscore.AllAnomalyKinds {
		if k == spec.kind {
			known = true
			break
		}
	}
	if !known {
		return nil, eh.Errorf("play: fixture: unknown anomaly kind %d", spec.kind)
	}
	gen := adscore.DefaultFixtureSpec(spec.kind, spec.seed)
	fixture, err = adscore.GenerateE(gen)
	if err != nil {
		return nil, eh.Errorf("play: fixture: generate: %w", err)
	}
	return
}

// publishFixture generates and publishes, off the render thread. Single-flight
// like the other write paths here: a second click while one is in flight is
// dropped rather than queued.
func (inst *PlayApp) publishFixture(spec fixtureSpec) {
	if inst.bus == nil || inst.fixtures == nil {
		return
	}
	st := inst.fixtures
	st.mu.Lock()
	if st.publishing {
		st.mu.Unlock()
		return
	}
	st.publishing = true
	st.err = nil
	st.mu.Unlock()

	go func() {
		summary, err := doPublishFixture(inst.bus, spec)
		st.mu.Lock()
		st.publishing = false
		st.err = err
		if err == nil {
			st.summary = summary
			st.generation++
		}
		st.mu.Unlock()
	}()
}

// doPublishFixture is one round: generate, encode, publish both datasets.
func doPublishFixture(bus busPublisherI, spec fixtureSpec) (summary string, err error) {
	fixture, err := generateFixture(spec)
	if err != nil {
		return
	}
	alloc := memory.NewGoAllocator()
	seriesIPC, truthIPC, err := buildFixtureArrow(fixture, spec.kind, alloc)
	if err != nil {
		return
	}
	seriesRes, err := adhocdata.PublishRequest(bus, adhocdata.PublishInput{
		Alias: fixtureSeriesAlias, ArrowIPCStream: seriesIPC, Publisher: fixturePublisher,
	})
	if err != nil {
		return "", eh.Errorf("play: fixture: publish %s: %w", fixtureSeriesAlias, err)
	}
	truthRes, err := adhocdata.PublishRequest(bus, adhocdata.PublishInput{
		Alias: fixtureTruthAlias, ArrowIPCStream: truthIPC, Publisher: fixturePublisher,
	})
	if err != nil {
		return "", eh.Errorf("play: fixture: publish %s: %w", fixtureTruthAlias, err)
	}
	return fmt.Sprintf("%s: %d samples · %s: %d planted extent(s) · %.1f%% anomalous",
		fixtureSeriesAlias, seriesRes.Rows, fixtureTruthAlias, truthRes.Rows,
		fixture.AnomalyFraction()*100), nil
}

// busPublisherI is the bus a publish rides. Named here so the publish round
// reads as "needs a bus" rather than "needs the app".
type busPublisherI = app.BusI

// syncFixtures binds the aliases and offers the scaffold once per publish.
// Called from the tab body, on the render thread, which is where BindDataset
// and the delivery ops both belong.
func (inst *PlayApp) syncFixtures() {
	if inst.fixtures == nil {
		return
	}
	_, _, gen, _ := inst.fixtures.status()
	if gen == inst.fixturesSeen {
		return
	}
	inst.fixturesSeen = gen
	// The aliases resolve to the newest dataset published under them, so a
	// republish under the same alias is picked up by the same binding — which
	// is what makes "generate again with another seed" a one-click act rather
	// than a re-wiring.
	for _, alias := range []string{fixtureSeriesAlias, fixtureTruthAlias} {
		if bErr := inst.BindDataset(alias, alias); bErr != nil {
			continue
		}
	}
	inst.InsertSqlAtCaret(fixtureScaffold())
}

// renderFixtureLab is the M4 affordance: kind, seed, generate. Deliberately
// two knobs — the generator's other parameters have defaults chosen to match
// what real recordings look like, and exposing them would invite tuning a
// fixture until a detector passes it, which is the opposite of the point.
func (inst *SeriesDriver) renderFixtureLab() {
	if inst.publishFixtures == nil {
		return
	}
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("fixture lab:") {
			rt.Small().Weak()
		}
		for i, kind := range adscore.AllAnomalyKinds {
			selected := inst.fixture.kind == kind
			if c.Button(inst.ids.PrepareSeq(uint64(0x5e21e5f100+i)),
				c.Atoms().Text(kind.String()).Keep()).
				Selected(selected).Small().FrameWhenInactive(false).Frame(true).
				SendResp().HasPrimaryClicked() {
				inst.fixture.kind = kind
			}
		}
		c.Label(fmt.Sprintf("seed %d", inst.fixture.seed)).Send()
		if c.Button(inst.ids.PrepareStr("fixture-seed"), c.Atoms().Text("↻").Keep()).
			Small().SendResp().HasPrimaryClicked() {
			// A counter, not a random draw: a fixture must be reproducible
			// from what the affordance shows, and "seed 7" is something two
			// people can both arrive at.
			inst.fixture.seed++
		}
		label := "generate"
		if inst.fixturePublishing {
			label = "generating…"
		}
		if c.Button(inst.ids.PrepareStr("fixture-generate"), c.Atoms().Text(label).Keep()).
			Small().SendResp().HasPrimaryClicked() && !inst.fixturePublishing {
			inst.publishFixtures(inst.fixture)
		}
	}
	switch {
	case inst.fixtureErr != nil:
		for rt := range c.RichTextLabel("The fixture did not publish: " + inst.fixtureErr.Error()) {
			rt.Small().Weak()
		}
	case inst.fixtureSummary != "":
		for rt := range c.RichTextLabel("published " + inst.fixtureSummary +
			" — queried as keelson('" + fixtureSeriesAlias + "'), like any other table") {
			rt.Small().Weak()
		}
	}
}

// noteFixtures hands the driver this frame's lab state and the publish seam.
func (inst *SeriesDriver) noteFixtures(spec fixtureSpec, publish func(fixtureSpec),
	publishing bool, summary string, err error) {
	// The spec is the DRIVER's — the app holds it only so it survives a
	// driver rebuild — so the incoming value seeds it once and the driver
	// owns it after.
	if !inst.fixtureSeeded {
		inst.fixture = spec
		inst.fixtureSeeded = true
	}
	inst.publishFixtures = publish
	inst.fixturePublishing = publishing
	inst.fixtureSummary = summary
	inst.fixtureErr = err
}

// fixtureScaffold is the query the affordance writes into the buffer after a
// publish. It is the WHOLE workbench on the fixture: the series, the detector,
// the flagged extents — ordinary SQL over ordinary tables, which is the claim
// the fixture lab exists to make good on.
func fixtureScaffold() string {
	return "-- ADR-0163 M4: the fixture is ordinary data. Nothing below knows it is synthetic.\n" +
		"WITH\n" +
		"  base AS (SELECT t, v FROM keelson('" + fixtureSeriesAlias + "') ORDER BY t),\n" +
		"  scores AS (SELECT tsAnomalyScores(t, v, 64) FROM base),\n" +
		"  spans  AS (SELECT tsAnomalySpans(t, v, 64, 5) FROM base)\n" +
		"SELECT * FROM base\n" +
		"-- The planted truth is a table too:\n" +
		"--   SELECT * FROM keelson('" + fixtureTruthAlias + "')\n"
}
