package sysmreplay

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/naturalkey"
)

// One (host, domain) pair is one series, named by its natural key, so the
// tokens below are the read side of the tee's storage layout (ADR-0184 §SD3,
// ADR-0405 §SD2). Ten name a per-tick kind;
// DomainCPUInfo, DomainSockets and DomainTopology name the three that are
// written sparsely and carried forward instead (see the package doc).
const (
	// DomainCPU is the per-tick CPU sample.
	DomainCPU = string(sysmsnap.DomainCPU)
	// DomainCPUInfo is the CPU descriptor, written once per host.
	DomainCPUInfo = DomainCPU + "/info"
	// DomainMem is the per-tick memory sample.
	DomainMem = string(sysmsnap.DomainMem)
	// DomainPSI is the per-tick pressure sample.
	DomainPSI = string(sysmsnap.DomainPSI)
	// DomainNet is the per-tick interface table.
	DomainNet = string(sysmsnap.DomainNet)
	// DomainDiskMnt is the per-tick mount table. Separate from DomainDiskIO
	// because the two lists have independent lengths.
	DomainDiskMnt = string(sysmsnap.DomainDisk) + "/mount"
	// DomainDiskIO is the per-tick block-device table.
	DomainDiskIO = string(sysmsnap.DomainDisk) + "/io"
	// DomainBattery is the per-tick power-supply sample.
	DomainBattery = string(sysmsnap.DomainBattery)
	// DomainGPU is the per-tick device table.
	DomainGPU = string(sysmsnap.DomainGPU)
	// DomainProc is the per-tick process table, minus the sensitive columns.
	DomainProc = string(sysmsnap.DomainProc)
	// DomainProcCmd is the opt-in half of the process table — command line,
	// user and ids (ADR-0184 §SD8). Absent unless the tee was asked for it.
	DomainProcCmd = DomainProc + "/cmd"
	// DomainSockets is the listening-socket table, written on the collector's
	// own slower cadence and dated by its own stamp.
	DomainSockets = string(sysmsnap.DomainSockets)
	// DomainTopology is the static containment tree, written once per host.
	DomainTopology = "topology"
)

// colNaturalKey is the physical name of the facts envelope's natural key.
const colNaturalKey = `"id:naturalKey:y:4::0:"`

// kindOf names the component a domain's rows carry, which is how a read of
// a series names its kind beside its natural key (ADR-0405 §SD2).
var kindOf = map[string]string{
	DomainCPU: "SysCpu", DomainCPUInfo: "SysCpuInfo", DomainMem: "SysMem", DomainPSI: "SysPsi",
	DomainNet: "SysNet", DomainDiskMnt: "SysDiskMount", DomainDiskIO: "SysDiskIo", DomainBattery: "SysBattery",
	DomainGPU: "SysGpu", DomainProc: "SysProc", DomainProcCmd: "SysProcCmd", DomainSockets: "SysSocket",
	DomainTopology: "SysTopology",
}

// EntityNaturalKey is the natural key of one (host, domain) series: the
// pair, in the vocabulary's JSON form. It is what a series is found by —
// a row's id names the row (ADR-0405) — and it is the same for rows written
// before ids were minted, so a read by it spans both.
//
// This restates the tee's own unexported derivation rather than importing
// it: a reader that depends on the writer inverts the dependency for an
// encoding and thirteen string constants. The agreement is pinned by a test
// in package `sysmtee` comparing these keys against the natural keys its row
// builders stamp, so drift fails in the default test lane rather than in
// production as a replay that silently finds no rows.
func EntityNaturalKey(host, domain string) (nk []byte) {
	enc := naturalkey.NewEncoder()
	enc.Begin().AddStr(host).AddStr(domain)
	nk, err := enc.End(naturalkey.SerializationFormatJson)
	if err != nil {
		// Two strings always encode; a failure here is a broken encoder.
		panic("sysmreplay: natural key of (" + host + ", " + domain + "): " + err.Error())
	}
	return
}

// naturalKeyLiteral renders a natural key as a ClickHouse string literal.
func naturalKeyLiteral(nk []byte) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(string(nk)) + "'"
}

// naturalKeyPredicate restricts a read to the rows whose natural key is
// domain's series on host. The CPU descriptor's rows written before
// 2026-11-22 carry the CPU sample series' pair, so its read takes both;
// the kind, which the caller names, tells the two apart.
func naturalKeyPredicate(host, domain string) (pred string) {
	lit := naturalKeyLiteral(EntityNaturalKey(host, domain))
	if domain == DomainCPUInfo {
		return colNaturalKey + " IN (" + lit + ", " + naturalKeyLiteral(EntityNaturalKey(host, DomainCPU)) + ")"
	}
	return colNaturalKey + " = " + lit
}

// seriesPredicate is naturalKeyPredicate with the kind named: for SQL the
// reader composes itself, where no generated scan adds the component's
// filter. A natural key is unique within a writer's kinds, not across the
// table.
func seriesPredicate(host, domain string) (pred string) {
	pred = naturalKeyPredicate(host, domain)
	if kind, ok := kindOf[domain]; ok {
		pred = "(" + sysmfacts.SysmetricsComponentSQL.Kinds[kind].Filter + ") AND " + pred
	}
	return
}
