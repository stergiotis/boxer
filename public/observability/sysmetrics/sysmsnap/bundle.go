package sysmsnap

// Domain identifies one collector slot in a bundle. Used as the key in
// [BundleSnapshot.Errors] and as the per-domain subject token on the metric
// plane (ADR-0090 SD1).
type Domain string

const (
	DomainCPU       Domain = "cpu"
	DomainMem       Domain = "mem"
	DomainDisk      Domain = "disk"
	DomainNet       Domain = "net"
	DomainBattery   Domain = "battery"
	DomainProc      Domain = "proc"
	DomainSensors   Domain = "sensors"
	DomainContainer Domain = "container"
	DomainGPU       Domain = "gpu"
	DomainPSI       Domain = "psi"
)

// BundleSnapshot is the per-domain union of all configured collectors'
// outputs. Fields are nil / empty when their collector was not wired.
type BundleSnapshot struct {
	SampledAtUnixMs int64

	CPU       *CPUSnapshot
	Mem       *MemSnapshot
	Disk      *DiskSnapshot
	Net       *NetSnapshot
	Battery   *BatterySnapshot
	Container *ContainerInfo
	GPU       *GPUSnapshot
	PSI       *PSISnapshot

	// Procs is the slice form of the process table. Empty when the proc
	// collector was not wired.
	Procs []ProcInfo

	// Sensors is the temperature reading list. Empty when the sensors
	// collector was not wired (or when the cpu collector is wired and
	// already includes per-cpu temperatures inline).
	Sensors []TempReading

	// Topology is the static CPU containment hierarchy (ADR-0090 SD6 fork:
	// published on the metric plane rather than read in-process by the
	// consumer). The scraper reads it once and stamps the same pointer onto
	// every snapshot so a late subscriber still receives it; nil when the
	// topology read failed or was not wired.
	Topology *Topology

	// Errors maps the domain that failed to its error. Empty (not nil)
	// when every wired collector succeeded; consumers can iterate to
	// log per-domain failures without aborting on partial success.
	Errors map[Domain]error
}
