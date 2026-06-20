package sysmetricsbus

import (
	"errors"
	"testing"

	"github.com/stergiotis/boxer/public/observability/sysmetrics"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/cpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/mem"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
	"github.com/stretchr/testify/require"
)

// TestCBORCodec_RoundTrip is the P2 fidelity guard: the interim wire codec
// must reproduce a populated BundleSnapshot field-for-field, and carry the
// per-domain Errors (which the error interface cannot encode directly)
// across as messages.
func TestCBORCodec_RoundTrip(t *testing.T) {
	codec := NewCBORCodec()
	orig := &sysmetrics.BundleSnapshot{
		SampledAtUnixMs: 1_700_000_000_123,
		CPU: &cpu.Snapshot{
			SampledAtUnixMs:     1_700_000_000_123,
			TotalPercent:        42,
			PerCorePercent:      []uint8{10, 20, 30, 40},
			PerCoreFreqMHz:      []uint32{3200, 3100, 3000, 2900},
			LoadAvg1:            1.25,
			UsageWatts:          12.5,
			UsageWattsAvailable: true,
			ActiveCPUs:          []int32{0, 1, 2, 3},
			ModelName:           "Test CPU",
			LogicalCores:        4,
		},
		Mem: &mem.Snapshot{
			TotalBytes: 16 << 30,
			UsedBytes:  8 << 30,
		},
		Procs: []proc.Info{
			{PID: 1, PPID: 0, Name: "init", Cmd: "/sbin/init", CPUPercent: 1.5, RSSBytes: 4096},
			{PID: 4242, PPID: 1, Name: "imztop", Cmd: "imztop --headless", CPUPercent: 12.25, RSSBytes: 1 << 20},
		},
		Errors: map[sysmetrics.Domain]error{
			sysmetrics.DomainGPU: errors.New("no gpu present"),
		},
	}

	payload, err := codec.Encode(orig)
	require.NoError(t, err)
	require.NotEmpty(t, payload)

	got, err := codec.Decode(payload)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.Equal(t, orig.SampledAtUnixMs, got.SampledAtUnixMs)
	require.Equal(t, orig.CPU, got.CPU)
	require.Equal(t, orig.Mem, got.Mem)
	require.Equal(t, orig.Procs, got.Procs)

	// Errors round-trip as messages: CBORCodec carries .Error() strings and
	// rebuilds them with errors.New, so the concrete type differs but the
	// message is preserved.
	require.Len(t, got.Errors, 1)
	require.EqualError(t, got.Errors[sysmetrics.DomainGPU], "no gpu present")
}

// TestCBORCodec_NilPaths exercises the empty-snapshot and nil-Errors paths.
func TestCBORCodec_NilPaths(t *testing.T) {
	codec := NewCBORCodec()
	orig := &sysmetrics.BundleSnapshot{SampledAtUnixMs: 99}

	payload, err := codec.Encode(orig)
	require.NoError(t, err)

	got, err := codec.Decode(payload)
	require.NoError(t, err)
	require.Equal(t, int64(99), got.SampledAtUnixMs)
	require.Nil(t, got.CPU)
	require.Empty(t, got.Errors)
}

// TestCBORCodec_EncodeNil rejects a nil snapshot rather than panicking.
func TestCBORCodec_EncodeNil(t *testing.T) {
	_, err := NewCBORCodec().Encode(nil)
	require.Error(t, err)
}
