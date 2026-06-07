//go:build llm_generated_opus47

package widgets

import (
	"math/rand"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/distsummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// =============================================================================
// distsummary widget demo — two-level distribution summary
//
// Six synthetic distributions are pre-fed into TDigests at init,
// deliberately spanning scales from microseconds to gigabytes so the
// level-1 label's humanized formatting is visible: in-band values print
// as plain decimals while large / small ones take SI metric prefixes
// (400M, 2G, 50µ) instead of scientific notation. Each row renders a
// labelled distsummary: level 1 is the in-flow 5-number-summary line;
// hover any row to reveal the level-2 boxenplot rendered inside an egui
// tooltip via c.HoverUi.
// =============================================================================

type distsumDemoRow struct {
	name     string
	subject  string
	digest   *tdigest.TDigest
	extremes []float64
	// idPrefix scopes the per-row distsummary.Renderer. Distinct per
	// row so the toggle id, window id, bezier rect-capture seqs, and
	// pinned-state map slot don't collide — every row carries the
	// AnchorToggle by default and can be pinned open independently.
	idPrefix string
}

var distsumDemoRows = buildDistsumDemoRows()

// distsumDemoTasks is the keelson task API captured from the gallery
// host's bus in BusInit. The demo's confidence-band warm-up (an O(n²)
// solve at n=10 000) runs as a background job through it so it never
// blocks the render thread and shows in the supervisor / taskmonitor.
// nil when the host supplies no bus (tour/headless): the band still
// warms off-thread via the in-process registry, just without audit.
var distsumDemoTasks task.TaskApiI

func init() {
	registry.Register(registry.Demo{
		Name:     "distsummary",
		Category: "Charts & plots",
		Title:    icons.IconChartLine + " distsummary",
		Stage:    [2]float32{720, 500},
		Kind:     registry.DemoKindUX,
		Description: "Two-level summarisation of a statistical distribution. " +
			"Level 1 is a compact monospace 5-number summary paired with the " +
			"standard inspector.AnchorToggle (ADR-0046). Click the accent-" +
			"coloured arrow-square-out glyph on any row to open its inspector " +
			"window — a bezier connector tethers the toggle to the open " +
			"window. The window body is a two-tab surface: ECDF + " +
			"simultaneous confidence band — an instant DKW preview band with " +
			"a Compute-exact-band affordance that warms the tighter Berk-Jones " +
			"band on a cancellable background job (via ecdfbands) — " +
			"as the default tab, plus the scientifically correct letter-value " +
			"boxenplot in the second tab — both reading the same caller-owned " +
			"tdigest per the ADR-0046 shared-sketch rule. Composes tdigest + " +
			"letterval + boxenplot + ecdf + ecdfbands.",
		BusInit: func(_ *c.WidgetIdStack, bus runtimeapp.BusI) (state any) {
			// Build a task API from the host bus so the band warm-up is a
			// keelson background job (ADR-0038). nil bus (tour) leaves
			// distsumDemoTasks unset — the in-process registry still runs
			// the solve off the render thread.
			if bus != nil {
				distsumDemoTasks = task.NewBusApi(task.ApiConfig{Bus: bus})
			}
			return nil
		},
		Render: demoDistsummary,
	})
}

func demoDistsummary(ids *c.WidgetIdStack) {
	c.Label("Click any row's arrow-square-out glyph to open its inspector:").Send()
	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())

	now := time.Now()
	for i, row := range distsumDemoRows {
		// Per-row Renderer with a distinct idPrefix so each row owns its
		// own toggle / window / tether identities — required because
		// distsummary derives those ids from idPrefix and the pinned
		// state lives in a package-level map keyed by the same string.
		r := distsummary.New(row.idPrefix).
			Tasks(distsumDemoTasks).
			// Cap the exact band's effective n so the opt-in Berk-Jones solve
			// at the demo's n=10 000 stays a ~30 s, cancellable job (it would
			// otherwise run ~14 min) — the instant DKW preview band is always
			// drawn at the true n meanwhile. See distsummary.ExactBandMaxN.
			ExactBandMaxN(2000).
			Provenance(inspector.Provenance{
				Subject:   row.subject,
				SampledAt: now,
			})
		for range c.Horizontal().KeepIter() {
			// Fixed-width label column keeps every distsummary cell at
			// the same x — visually aligned across rows.
			c.UiSetMinWidth(160)
			c.Label(row.name).Send()
			c.AddSpace(gapSections())
			r.Render(ids.PrepareSeq(uint64(0xDD5000+i)), row.digest, row.extremes)
		}
		c.AddSpace(padInner())
	}

	c.Separator().Horizontal().Send()
	c.AddSpace(padInner())
	c.LabelAtoms(
		c.Atoms().BeginRichText("Tip: every distsummary carries the inspector by " +
			"default — there is no opt-in. The toggle's accent hue matches the " +
			"bezier tether so the visual link between source row and floating " +
			"inspector window reads at a glance.").
			Small().Weak().End().Keep(),
	).Send()
}

// buildDistsumDemoRows seeds six TDigests with synthetic samples
// covering distinct shapes (narrow, wide, skewed, heavy-tailed) and a
// wide span of magnitudes (sub-millisecond to gigabyte) so both the
// hover popup and the level-1 label's SI formatting visibly differ from
// row to row. K=8 matches letterval.MinTailCount; the extremes feed
// OutlierModeAuto when n is small (synthetic data here is large so Count
// mode wins).
func buildDistsumDemoRows() (out []*distsumDemoRow) {
	type variant struct {
		name string
		mu   float64
		sig  float64
		seed int64
		// skewed swaps the symmetric Gaussian for a right-skewed burst shape —
		// a tight cluster at mu±sig punctuated by periodic high spikes — so one
		// row exercises the long-tailed ECDF the inspector is built to read.
		skewed bool
	}
	variants := []variant{
		{name: "p50 ≈ 3 ms", mu: 3.0, sig: 0.5, seed: 11},
		{name: "p50 ≈ 12 ms", mu: 12.0, sig: 2.0, seed: 22},
		{name: "heavy-tailed", mu: 60.0, sig: 1.5, seed: 33, skewed: true},
		{name: "narrow", mu: 0.10, sig: 0.01, seed: 44},
		// Large- and small-scale rows exercise the humanized formatter's SI
		// metric prefixes (up: 400M / 2G; down: ~10–90µ) — the in-band rows
		// above stay plain, so the two groups read side by side.
		{name: "payload ≈ 2 GB", mu: 2.0e9, sig: 4.0e8, seed: 55},
		{name: "jitter ≈ 50 µs", mu: 5.0e-5, sig: 1.0e-5, seed: 66},
	}
	// Per-row provenance subjects — exercise the inspector.ProvenanceChip
	// migration on the level-2 hover popup. Subjects shaped like the
	// ADR-0026 §SD3 app-event convention so the chip's monospace subject
	// reads naturally; demo rows otherwise carry no real bus binding.
	subjects := []string{
		"app.play.event.latency.p50.ms",
		"app.play.event.latency.median.ms",
		"app.spinnaker.event.dist.bursty",
		"app.imztop.event.cpu.idle.frac",
		"app.store.event.payload.bytes",
		"app.play.event.jitter.s",
	}
	// Per-row idPrefix — required for distsummary.Pinned's absolute-id
	// derivation contract. Unique per row so multi-row pinning would
	// not collide (today only row 0 opts in).
	idPrefixes := []string{
		"ds-demo-row0",
		"ds-demo-row1",
		"ds-demo-row2",
		"ds-demo-row3",
		"ds-demo-row4",
		"ds-demo-row5",
	}
	const n = 10_000
	const k = 8
	out = make([]*distsumDemoRow, 0, len(variants))
	for vi, v := range variants {
		rnd := rand.New(rand.NewSource(v.seed))
		data := make([]float64, n)
		for i := range data {
			data[i] = v.mu + v.sig*rnd.NormFloat64()
			// Skewed variant: replace every 40th draw with a high burst
			// (≈200–735) so the tight cluster at mu grows a long upper tail.
			if v.skewed && i%40 == 0 {
				data[i] = 200 + 535*rnd.Float64()
			}
		}
		d := tdigest.NewTDigest()
		for _, x := range data {
			d.Push(x)
		}
		sorted := make([]float64, len(data))
		copy(sorted, data)
		sort.Float64s(sorted)
		extremes := make([]float64, 0, 2*k)
		extremes = append(extremes, sorted[:k]...)
		extremes = append(extremes, sorted[len(sorted)-k:]...)
		out = append(out, &distsumDemoRow{
			name:     v.name,
			subject:  subjects[vi],
			idPrefix: idPrefixes[vi],
			digest:   d,
			extremes: extremes,
		})
	}
	return
}
