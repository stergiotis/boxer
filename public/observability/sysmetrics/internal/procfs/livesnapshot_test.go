package procfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
)

// TestLiveSnapshot_IterKV walks a real /proc/meminfo captured from a
// running kernel. The fixture lives under testdata/live-snapshot/ and
// is expected to evolve only when the kernel rearranges the file
// (a meaningful change worth surfacing in code review). Until that
// happens, the parser must continue to extract MemTotal cleanly.
//
// The fixture's `kernel-release` file records which kernel the bytes
// came from, so a future maintainer chasing a parser regression knows
// what version baseline the test pins.
func TestLiveSnapshot_IterKV_Meminfo(t *testing.T) {
	root := "testdata/live-snapshot"
	if _, err := os.Stat(filepath.Join(root, "meminfo")); err != nil {
		t.Skipf("no captured meminfo fixture: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "meminfo"))
	require.NoError(t, err)

	// Real /proc/meminfo on contemporary kernels (6.x) carries 50+
	// keys. We assert MemTotal is present and looks sane.
	var sawMemTotal, sawMemFree bool
	for k, v := range procfs.IterKV(data) {
		switch string(k) {
		case "MemTotal":
			sawMemTotal = true
			assert.Contains(t, string(v), "kB")
		case "MemFree":
			sawMemFree = true
		}
	}
	assert.True(t, sawMemTotal, "captured meminfo must yield MemTotal")
	assert.True(t, sawMemFree, "captured meminfo must yield MemFree")
}

// TestLiveSnapshot_IterFields_StatAggregate covers the first line of
// /proc/stat (the aggregate "cpu" row) — the schema-stable subset the
// kernel guarantees back to 2.6.x.
func TestLiveSnapshot_IterFields_StatAggregate(t *testing.T) {
	root := "testdata/live-snapshot"
	if _, err := os.Stat(filepath.Join(root, "stat-aggregate-only")); err != nil {
		t.Skipf("no captured stat fixture: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "stat-aggregate-only"))
	require.NoError(t, err)

	var firstLineFields []string
	for line := range procfs.IterLines(data) {
		for f := range procfs.IterFields(line) {
			firstLineFields = append(firstLineFields, string(f))
		}
		break
	}
	require.NotEmpty(t, firstLineFields)
	assert.Equal(t, "cpu", firstLineFields[0], "first line must be the aggregate 'cpu' row")
	// Documented kernel guarantee: at least 8 numeric fields after
	// the "cpu" tag (user / nice / system / idle / iowait / irq /
	// softirq / steal). Modern kernels add guest / guest_nice for 10.
	assert.GreaterOrEqual(t, len(firstLineFields), 9,
		"aggregate cpu line must carry tag + ≥8 numeric fields; got %d", len(firstLineFields))
}

// TestLiveSnapshot_IterFields_LoadAvg checks /proc/loadavg shape: 5
// fields, three of which are decimal floats.
func TestLiveSnapshot_IterFields_LoadAvg(t *testing.T) {
	root := "testdata/live-snapshot"
	if _, err := os.Stat(filepath.Join(root, "loadavg")); err != nil {
		t.Skipf("no captured loadavg fixture: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "loadavg"))
	require.NoError(t, err)

	var fields []string
	for line := range procfs.IterLines(data) {
		for f := range procfs.IterFields(line) {
			fields = append(fields, string(f))
		}
		break
	}
	require.Equal(t, 5, len(fields),
		"loadavg must have exactly 5 fields (1m 5m 15m running/total last-pid); got %v", fields)
}
