package sysmsnap

// NetInterface is the per-NIC sample.
type NetInterface struct {
	Name         string
	Index        int32
	HardwareAddr string // lowercase, colon-separated hex; empty when unavailable

	Up      bool // IFF_UP
	Running bool // IFF_RUNNING

	IPv4 []string
	IPv6 []string

	// Cumulative byte counters as reported by /sys/class/net/{name}/statistics/.
	// These reflect the kernel value at sample time — they may wrap on 32-bit
	// virtual NICs; the per-second rate fields below already compensate.
	RxBytes uint64
	TxBytes uint64

	// Per-second rates derived from the prior-sample delta; 0 on first sample.
	RxBytesPerSec uint64
	TxBytesPerSec uint64
}

// NetSnapshot is a single sample of all visible network interfaces.
type NetSnapshot struct {
	SampledAtUnixMs int64
	Interfaces      []NetInterface
}
