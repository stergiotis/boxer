package damp_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/timeseries/damp"
)

func TestScanMethodsAgree(t *testing.T) {
	// The transform must be a pure optimization: same readings, bit-comparable
	// scores within ordinary rounding. Cross-correlation by transform
	// accumulates differently from a direct dot product, so exact equality is
	// not on offer, but the reported distance is recomputed from materialized
	// z-normalized values either way and only neighbour *selection* is at risk.
	for _, window := range []int32{8, 32, 64, 256} {
		for _, exact := range []bool{false, true} {
			name := fmt.Sprintf("window=%d/exact=%v", window, exact)
			t.Run(name, func(t *testing.T) {
				values := quasiPeriodic(int(window)*10+600, float64(window)*0.8, 0.05)
				base := damp.Config{
					Window:      window,
					TrainLength: window * 6,
					Exact:       exact,
				}

				direct := base
				direct.ScanMethod = damp.ScanMethodDirect
				want, err := damp.ScoreE(values, direct)
				require.NoError(t, err)
				require.NotEmpty(t, want)

				transform := base
				transform.ScanMethod = damp.ScanMethodTransform
				got, err := damp.ScoreE(values, transform)
				require.NoError(t, err)
				require.Len(t, got, len(want))

				for i := range want {
					assert.Equal(t, want[i].Start, got[i].Start, "reading %d", i)
					assert.InDelta(t, want[i].Score, got[i].Score,
						tolerance(window, want[i].Score)+1.0e-6, "score at %d", i)
				}
				assert.Equal(t, argmaxStart(want), argmaxStart(got), "the discord must not move")
			})
		}
	}
}

func TestScanMethodStrings(t *testing.T) {
	assert.Equal(t, "auto", damp.ScanMethodAuto.String())
	assert.Equal(t, "direct", damp.ScanMethodDirect.String())
	assert.Equal(t, "transform", damp.ScanMethodTransform.String())
}

func benchmarkScan(b *testing.B, window int32, method damp.ScanMethodE) {
	b.Helper()
	values := quasiPeriodic(12000, float64(window)*0.8, 0.05)
	cfg := damp.Config{
		Window:       window,
		TrainLength:  window * 8,
		HistoryLimit: 8000,
		ScanMethod:   method,
	}
	b.ReportAllocs()
	for b.Loop() {
		inst, err := damp.NewDetectorE(cfg)
		if err != nil {
			b.Fatal(err)
		}
		for _, v := range values {
			inst.Push(v)
		}
	}
	b.ReportMetric(float64(b.N*len(values))/b.Elapsed().Seconds(), "samples/s")
}

func BenchmarkScanDirect16(b *testing.B)     { benchmarkScan(b, 16, damp.ScanMethodDirect) }
func BenchmarkScanTransform16(b *testing.B)  { benchmarkScan(b, 16, damp.ScanMethodTransform) }
func BenchmarkScanDirect50(b *testing.B)     { benchmarkScan(b, 50, damp.ScanMethodDirect) }
func BenchmarkScanTransform50(b *testing.B)  { benchmarkScan(b, 50, damp.ScanMethodTransform) }
func BenchmarkScanDirect128(b *testing.B)    { benchmarkScan(b, 128, damp.ScanMethodDirect) }
func BenchmarkScanTransform128(b *testing.B) { benchmarkScan(b, 128, damp.ScanMethodTransform) }
func BenchmarkScanDirect256(b *testing.B)    { benchmarkScan(b, 256, damp.ScanMethodDirect) }
func BenchmarkScanTransform256(b *testing.B) { benchmarkScan(b, 256, damp.ScanMethodTransform) }
func BenchmarkScanDirect512(b *testing.B)    { benchmarkScan(b, 512, damp.ScanMethodDirect) }
func BenchmarkScanTransform512(b *testing.B) { benchmarkScan(b, 512, damp.ScanMethodTransform) }

func BenchmarkScanAuto50(b *testing.B)  { benchmarkScan(b, 50, damp.ScanMethodAuto) }
func BenchmarkScanAuto512(b *testing.B) { benchmarkScan(b, 512, damp.ScanMethodAuto) }
