package sysmtee

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/naturalkey"
	"github.com/zeebo/xxh3"
)

// Kind labels. The value is what a human reads; the membership id is what a
// query filters on.
const (
	kindCpu     = "sysCpu"
	kindCpuInfo = "sysCpuInfo"
	kindMem     = "sysMem"
)

// Domain tokens, reusing the plane's own vocabulary
// ([sysmsnap.DomainCPU] and friends) so a subject token and a stored row name
// the same thing.
const (
	domainCpu = string(sysmsnap.DomainCPU)
	domainMem = string(sysmsnap.DomainMem)
)

// entityKey is the store Key for one (host, domain) series: every sample of
// that series shares it, so Latest is the current state and Replay is the
// history. xxh3 over the joined tokens, per ADR-0184 §SD3.
//
// The separator makes the encoding unambiguous — host tokens are sanitized to
// exclude it, so no (host, domain) pair can collide with another by
// concatenation.
func entityKey(host, domain string) uint64 {
	return xxh3.HashString(host + "/" + domain)
}

// entityNaturalKey records the pair the key digests, so a reader need not
// invert it. JSON is the vocabulary's natural-key format.
func entityNaturalKey(host, domain string) (nk []byte, err error) {
	enc := naturalkey.NewEncoder()
	enc.Begin().AddStr(host).AddStr(domain)
	nk, err = enc.End(naturalkey.SerializationFormatJson)
	if err != nil {
		err = eb.Build().Str("host", host).Str("domain", domain).Errorf("sysmtee: natural key failed: %w", err)
	}
	return
}

// cpuRow builds the per-tick CPU row.
func cpuRow(host string, snap *sysmsnap.CPUSnapshot, ts time.Time) (row sysmfacts.SysCpu, err error) {
	nk, err := entityNaturalKey(host, domainCpu)
	if err != nil {
		return
	}
	row = sysmfacts.SysCpu{
		Id:         entityKey(host, domainCpu),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindCpu,
		Host:       host,

		TotalPercent:   snap.TotalPercent,
		PerCorePercent: snap.PerCorePercent,
		PerCoreFreqMHz: snap.PerCoreFreqMHz,
		LoadAvg1:       snap.LoadAvg1,
		LoadAvg5:       snap.LoadAvg5,
		LoadAvg15:      snap.LoadAvg15,
		ActiveCPUs:     snap.ActiveCPUs,
	}
	// The collector reports availability separately; absence carries it here,
	// so a host without RAPL stores no watts rather than storing zero watts.
	if snap.UsageWattsAvailable {
		row.UsageWatts = option.Some(snap.UsageWatts)
	}
	return
}

// cpuInfoRow builds the per-host CPU descriptor. Its fields come from the same
// snapshot as the sample — the collector reads them once at construction and
// restamps them on every tick — so the split is the tee's, not the plane's.
func cpuInfoRow(host string, snap *sysmsnap.CPUSnapshot, ts time.Time) (row sysmfacts.SysCpuInfo, err error) {
	nk, err := entityNaturalKey(host, domainCpu)
	if err != nil {
		return
	}
	row = sysmfacts.SysCpuInfo{
		Id:           entityKey(host, domainCpu+"/info"),
		NaturalKey:   nk,
		Ts:           ts,
		Kind:         kindCpuInfo,
		Host:         host,
		ModelName:    snap.ModelName,
		LogicalCores: snap.LogicalCores,
	}
	return
}

// memRow builds the per-tick memory row.
func memRow(host string, snap *sysmsnap.MemSnapshot, ts time.Time) (row sysmfacts.SysMem, err error) {
	nk, err := entityNaturalKey(host, domainMem)
	if err != nil {
		return
	}
	row = sysmfacts.SysMem{
		Id:         entityKey(host, domainMem),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindMem,
		Host:       host,

		TotalBytes:     snap.TotalBytes,
		FreeBytes:      snap.FreeBytes,
		AvailableBytes: snap.AvailableBytes,
		BuffersBytes:   snap.BuffersBytes,
		CachedBytes:    snap.CachedBytes,
		SwapTotalBytes: snap.SwapTotalBytes,
		SwapFreeBytes:  snap.SwapFreeBytes,
		UsedBytes:      snap.UsedBytes,
		SwapUsedBytes:  snap.SwapUsedBytes,
		ARCSizeBytes:   snap.ARCSizeBytes,
		ARCMinBytes:    snap.ARCMinBytes,
	}
	return
}
