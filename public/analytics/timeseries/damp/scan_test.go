package damp_test

import (
	"fmt"
	"os"
	"strings"
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

func TestThrottleWarnings(t *testing.T) {
	tests := []struct {
		name     string
		governor string
		boost    string
		want     []string
	}{
		{name: "unthrottled", governor: "performance", boost: "1"},
		{name: "cpufreq absent", governor: "", boost: ""},
		{name: "powersave", governor: "powersave", boost: "1", want: []string{"GOVERNOR-POWERSAVE"}},
		{name: "boost off", governor: "performance", boost: "0", want: []string{"BOOST-OFF"}},
		{
			name:     "both",
			governor: "schedutil",
			boost:    "0",
			want:     []string{"GOVERNOR-SCHEDUTIL", "BOOST-OFF"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, throttleWarnings(tt.governor, tt.boost))
		})
	}
}

func TestReadSysfsMissingPathIsEmpty(t *testing.T) {
	assert.Equal(t, "", readSysfs("/proc/definitely-not-a-real-cpufreq-node"))

	path := t.TempDir() + "/governor"
	require.NoError(t, os.WriteFile(path, []byte("powersave\n"), 0o600))
	assert.Equal(t, "powersave", readSysfs(path), "trailing newline must be trimmed")
}

func TestScanMethodStrings(t *testing.T) {
	assert.Equal(t, "auto", damp.ScanMethodAuto.String())
	assert.Equal(t, "direct", damp.ScanMethodDirect.String())
	assert.Equal(t, "transform", damp.ScanMethodTransform.String())
}

// readSysfs returns a trimmed sysfs value, empty when it cannot be read —
// which is the normal case off Linux, or inside a container without cpufreq.
func readSysfs(path string) (value string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	value = strings.TrimSpace(string(raw))
	return
}

// guardThrottling makes a power-saving CPU state visible on the benchmark's own
// result line, not merely in a log message.
//
// This exists because it already went wrong: the scan comparison in ADR-0150
// was first measured under a power-saving governor, the output was read through
// a grep for benchmark lines, and the warning a b.Logf would have printed was
// filtered out with everything else. A custom metric survives that, because it
// is part of the result line itself.
//
// It reports rather than skips. A benchmark that refuses to run on a machine
// whose governor cannot be changed — CI, a container, a laptop on battery — is
// worse than one that runs and says so.
func guardThrottling(b *testing.B) {
	b.Helper()
	for _, w := range throttleWarnings(
		readSysfs("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"),
		readSysfs("/sys/devices/system/cpu/cpufreq/boost"),
	) {
		b.Logf("%s: throughput is understated", w)
		b.ReportMetric(1, w)
	}
}

// throttleWarnings names each power-saving setting in force. An unreadable
// value yields no warning: absent is not the same as throttled, and this must
// stay silent on machines that simply do not expose cpufreq.
func throttleWarnings(governor string, boost string) (warnings []string) {
	if governor != "" && governor != "performance" {
		warnings = append(warnings, "GOVERNOR-"+strings.ToUpper(governor))
	}
	if boost == "0" {
		warnings = append(warnings, "BOOST-OFF")
	}
	return
}

func benchmarkScan(b *testing.B, window int32, method damp.ScanMethodE) {
	b.Helper()
	guardThrottling(b)
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
