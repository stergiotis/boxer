package sysmfacts

import "time"

// SysNet is one network sample of one host, column-major: one array element
// per interface, every array in the row the same length and in the same order.
//
// # The alignment contract
//
// Index i of every array below describes the same interface. Nothing in leeway
// enforces that — the arrays live in different sections, so the co-length
// check ADR-0181 §SD5 generates applies within a section, not across them. The
// writer builds all of them in one pass from one slice, which is what keeps
// them aligned; a reader that filters one array must index the others by
// position rather than re-filtering them.
//
// Per-interface IP address lists are deliberately absent: a list per element
// does not flatten into this shape, and addresses are closer to the
// ADR-0090 §SD8 sensitive class than to a metric.
type SysNet struct {
	_ struct{} `kind:"sysNet"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindNet,symbol"`
	Host string `lw:"sysmNetHost,symbol"`

	Name         []string `lw:"sysmNetName,symbolArray"`
	Index        []int32  `lw:"sysmNetIndex,i32Array"`
	HardwareAddr []string `lw:"sysmNetHardwareAddr,symbolArray"`

	// Flags ride a u8 lane as 0/1: the facts schema has a scalar `bool`
	// section but no boolean array, and a per-element flag needs one.
	Up      []uint8 `lw:"sysmNetUp,u8Array,ct=u8h"`
	Running []uint8 `lw:"sysmNetRunning,u8Array,ct=u8h"`

	RxBytes []uint64 `lw:"sysmNetRxBytes,u64Array"`
	TxBytes []uint64 `lw:"sysmNetTxBytes,u64Array"`

	// The rates are the collector's own derivation, stored beside the raw
	// counters rather than left to the reader: they compensate for counter
	// wrap on 32-bit virtual NICs, which a consumer differencing the
	// cumulative fields cannot detect after the fact.
	RxBytesPerSec []uint64 `lw:"sysmNetRxBytesPerSec,u64Array"`
	TxBytesPerSec []uint64 `lw:"sysmNetTxBytesPerSec,u64Array"`
}
