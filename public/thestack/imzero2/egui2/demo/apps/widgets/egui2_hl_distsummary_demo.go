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
// Four synthetic distributions are pre-fed into TDigests at init. Each
// row in the demo renders a labelled distsummary: level 1 is the
// in-flow 5-number-summary line; hover any row to reveal the level-2
// boxenplot rendered inside an egui tooltip via c.HoverUi.
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
			"simultaneous confidence band (Berk-Jones default, via ecdfbands) " +
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

// buildDistsumDemoRows seeds four TDigests with synthetic samples
// covering distinct shapes (narrow, wide, skewed, heavy-tailed) so the
// hover popup visibly differs from row to row. K=8 matches
// letterval.MinTailCount; the extremes feed OutlierModeAuto when n is
// small (synthetic data here is large so Count mode wins).
func buildDistsumDemoRows() (out []*distsumDemoRow) {
	type variant struct {
		name string
		mu   float64
		sig  float64
		seed int64
	}
	variants := []variant{
		{"p50 ≈ 3 ms", 3.0, 0.5, 11},
		{"p50 ≈ 12 ms", 12.0, 2.0, 22},
		{"heavy-tailed", 50.0, 15.0, 33},
		{"narrow", 0.10, 0.01, 44},
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
	}
	// Per-row idPrefix — required for distsummary.Pinned's absolute-id
	// derivation contract. Unique per row so multi-row pinning would
	// not collide (today only row 0 opts in).
	idPrefixes := []string{
		"ds-demo-row0",
		"ds-demo-row1",
		"ds-demo-row2",
		"ds-demo-row3",
	}
	const n = 10_000
	const k = 8
	out = make([]*distsumDemoRow, 0, len(variants))
	for vi, v := range variants {
		rnd := rand.New(rand.NewSource(v.seed))
		data := make([]float64, n)
		for i := range data {
			data[i] = v.mu + v.sig*rnd.NormFloat64()
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
