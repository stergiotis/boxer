package sysmreplay

import (
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/zeebo/xxh3"
)

// One (host, domain) pair is one entity key, so the tokens below are the read
// side of the tee's storage layout (ADR-0184 §SD3). Ten name a per-tick kind;
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

// EntityKey is the store key for one (host, domain) series: xxh3 over the
// tokens joined by "/", per ADR-0184 §SD3. Host tokens are sanitized to exclude
// the separator, so no pair can collide with another by concatenation.
//
// This restates the tee's own unexported derivation rather than importing it: a
// reader that depends on the writer inverts the dependency for a hash and
// thirteen string constants. The agreement is pinned by a test in package
// `sysmtee` comparing these keys against the ids its row builders stamp, so
// drift fails in the default test lane rather than in production as a replay
// that silently finds no rows.
func EntityKey(host, domain string) (key uint64) {
	key = xxh3.HashString(host + "/" + domain)
	return
}
