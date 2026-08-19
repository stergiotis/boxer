//go:build integration

package loadstudy_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/analytics/timeseries/damp"
	"github.com/stergiotis/boxer/public/analytics/timeseries/loadstudy"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
)

// The study is run per gap-free span rather than over a fixed window, because
// this host records intermittently: a fixed 48-hour grid came back 73%
// forward-filled, which is 73% invented.
//
// Two grids, because they trade span length against event density in opposite
// directions. The coarse grid reaches further in wall-clock time; the fine grid
// puts more bins under a window and keeps the label prevalence lower.
const (
	studyLookback = 6 * 24 * time.Hour
	minSpanEvents = 10
	labelTolBin   = 1 // the event's own bin plus one either side
	maxSpans      = 3
)

type grid struct {
	step    int32
	minBins int32
	windows []int32
}

// The window sweep per grid is bounded by what a span can support: a window
// needs many multiples of itself in bins before a left-discord search has any
// history to compare against.
var grids = []grid{
	{step: 10, minBins: 600, windows: []int32{8, 16, 32, 64}},
	{step: 60, minBins: 200, windows: []int32{4, 8, 16, 32}},
}

func newClient(t *testing.T) (client *loadstudy.Client) {
	t.Helper()
	if clickhouseenv.Endpoint.Get() == "" && clickhouseenv.URL.Get() == "" {
		t.Skip("CLICKHOUSE_ENDPOINT unset; skipping the live load study")
	}
	client = loadstudy.NewClientFromEnv()
	return
}

func extractSpan(t *testing.T, client *loadstudy.Client, span loadstudy.Span, step int32) (series *loadstudy.Series) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	series, err := loadstudy.ExtractE(ctx, client, loadstudy.Spec{
		From:        span.From,
		To:          span.To,
		StepSeconds: step,
	})
	require.NoError(t, err)
	return
}

func findSpans(t *testing.T, client *loadstudy.Client, g grid) (spans []loadstudy.Span) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	spans, err := loadstudy.FindSpansE(ctx, client, studyLookback, g.step, g.minBins, minSpanEvents, nil)
	require.NoError(t, err)
	if len(spans) > maxSpans {
		spans = spans[:maxSpans]
	}
	return
}

// TestSeriesIsWorthStudying reports what the extraction actually produced before
// anything is concluded from it. A study that does not first describe its input
// is not reporting a measurement, it is reporting a number.
func TestSeriesIsWorthStudying(t *testing.T) {
	client := newClient(t)
	for _, g := range grids {
		spans := findSpans(t, client, g)
		t.Logf("=== %ds grid: %d usable gap-free spans (>=%d bins, >=%d events) in the last %s",
			g.step, len(spans), g.minBins, minSpanEvents, studyLookback)
		for _, span := range spans {
			series := extractSpan(t, client, span, g.step)
			labels := loadstudy.EventLabels(series, labelTolBin)
			t.Logf("  %s .. %s  bins=%d (%s)  events=%d  filled=%d  rejected=%d  prevalence=%.2f%%",
				span.From.Format("01-02 15:04"), span.To.Format("01-02 15:04"),
				series.Len(), time.Duration(series.Len())*series.Step,
				span.Events, series.Gaps, series.Rejected,
				loadstudy.LabelledFraction(labels)*100.0)

			// The invariant that catches a shifted or mis-detected span. The span
			// was found on OSUserTimeNormalized, so cpu_user must come back with
			// nothing forward-filled. A timezone round-trip through the server's
			// local-time rendering, and a run detector that over-reported, both
			// showed up here as a channel that was entirely invented.
			cpuGaps, ok := series.GapsFor("cpu_user")
			require.True(t, ok)
			require.Zero(t, cpuGaps,
				"span reported gap-free but cpu_user needed %d filled bins", cpuGaps)
			require.Equal(t, span.Bins, series.Len(), "extracted grid does not match the span")
			for _, name := range series.SortedNames() {
				values, ok := series.Channel(name)
				require.True(t, ok)
				d := loadstudy.Describe(values)
				t.Logf("      %-14s min=%-11.4g max=%-11.4g mean=%-11.4g sd=%-11.4g constant=%v",
					name, d.Min, d.Max, d.Mean, d.StdDev, d.Constant)
			}
		}
	}
}

// TestWindowSweep is the measurement ADR-0150 wanted: which window length real
// load metrics want, and whether a detector correlates with recorded events any
// better than a one-liner does.
//
// It asserts almost nothing. There is no ground truth here — events are not
// anomaly labels — so a threshold on VUS-PR would be a threshold on a number
// whose correct value nobody knows. What it does assert is that the machinery
// runs end to end on real data and produces finite figures; the findings are in
// the log, and in the ADR.
func TestWindowSweep(t *testing.T) {
	client := newClient(t)
	for _, g := range grids {
		for _, span := range findSpans(t, client, g) {
			series := extractSpan(t, client, span, g.step)
			labels := loadstudy.EventLabels(series, labelTolBin)
			prevalence := loadstudy.LabelledFraction(labels)
			t.Logf("=== %ds grid, %s .. %s, bins=%d, events=%d, prevalence=%.4f",
				g.step, span.From.Format("01-02 15:04"), span.To.Format("01-02 15:04"),
				series.Len(), span.Events, prevalence)

			channels := append(series.SortedNames(), "event_rate")
			for _, name := range channels {
				values := series.EventRate
				if name != "event_rate" {
					var ok bool
					values, ok = series.Channel(name)
					require.True(t, ok)
				}
				if loadstudy.Describe(values).Constant {
					t.Logf("  %-14s constant across the span; nothing to detect", name)
					continue
				}

				_, best := baselines(t, values, labels)
				var line strings.Builder
				line.WriteString(fmt.Sprintf("  %-14s baseline=%.4f |", name, best))
				for _, window := range g.windows {
					if int32(len(values)) < window*12 {
						continue
					}
					readings, err := damp.ScoreE(values, damp.Config{
						Window:      window,
						TrainLength: window * 8,
						Exact:       true,
					})
					require.NoError(t, err)

					scores := damp.PositionScores(readings, int32(len(values)), nil)
					m, err := adscore.EvaluateE(scores, labels, 0)
					require.NoError(t, err)
					require.False(t, math.IsNaN(m.VUSPR), "%s window %d produced NaN", name, window)

					mark := " "
					if m.VUSPR > best {
						mark = "*"
					}
					line.WriteString(fmt.Sprintf(" w%d=%.4f%s", window, m.VUSPR, mark))
				}
				t.Log(line.String())
			}
		}
	}
}

// baselines scores Wu and Keogh's one-liners over the same series and labels.
// Any figure the detector produces is only meaningful above these.
func baselines(t *testing.T, values []float64, labels []bool) (results []adscore.BaselineResult, worst float64) {
	t.Helper()
	for _, b := range adscore.AllBaselines {
		m, err := adscore.EvaluateE(adscore.BaselineScores(values, b, 30), labels, 0)
		require.NoError(t, err)
		results = append(results, adscore.BaselineResult{Baseline: b, Measures: m})
		if m.VUSPR > worst {
			worst = m.VUSPR
		}
	}
	return
}

// TestExtractionIsReproducible guards the part of this package that could
// silently rot: the hard-coded boxer.facts column names, which are leeway
// codegen output and move if the schema is regenerated.
func TestExtractionIsReproducible(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := newClient(t)

	to := time.Now().UTC()
	spec := loadstudy.Spec{From: to.Add(-6 * time.Hour), To: to, StepSeconds: 60}

	first, err := loadstudy.ExtractE(ctx, client, spec)
	require.NoError(t, err)
	second, err := loadstudy.ExtractE(ctx, client, spec)
	require.NoError(t, err)

	require.Equal(t, first.Len(), second.Len())
	require.Equal(t, first.Names, second.Names)
	for i := range first.Values {
		require.InDeltaSlice(t, first.Values[i], second.Values[i], 1.0e-9,
			"channel %s changed between identical extractions", first.Names[i])
	}
	require.NotEmpty(t, first.Names, "no channels came back; the metric names may have moved")
}

func countTrue(flags []bool) (n int) {
	for _, f := range flags {
		if f {
			n++
		}
	}
	return
}
