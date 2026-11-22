package sysmreplay

import (
	"strconv"
	"strings"

	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/naturalkey"
)

// One (host, domain) pair is one series under one id, so the tokens below
// are the read side of the tee's storage layout (ADR-0184 §SD3, ADR-0405
// §SD2). Ten name a per-tick kind;
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

// IdScope is the scope a series' natural key is resolved in: the tee's,
// restated here as the natural keys are.
const IdScope = "sysmetrics"

// EntityNaturalKey is the natural key of one (host, domain) series: the
// pair, in the vocabulary's JSON form. The series' id is resolved from it —
// by the tee through the facts table's keyed generator, by this reader
// through a lookup in the same directory (ADR-0405 §SD2).
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

// LegacyEntityKey is the id a series' rows carried before ids were resolved
// through the directory (ADR-0184 §SD3 as it stood): xxh3 over host and
// domain joined by "/". Rows written then keep it (ADR-0405 §SD5), so a
// read of a series takes it beside the series' id.
func LegacyEntityKey(host, domain string) (key uint64) {
	return xxh3.HashString(host + "/" + domain)
}

// idPredicate restricts a read to rows whose id is one of ids.
func idPredicate(ids []uint64) (pred string) {
	lits := make([]string, len(ids))
	for i, id := range ids {
		lits[i] = strconv.FormatUint(id, 10)
	}
	return sysmfacts.SysmetricsColKey + " IN (" + strings.Join(lits, ", ") + ")"
}
