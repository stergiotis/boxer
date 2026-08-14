package sysmfacts

import "time"

// SysDiskIo is one block-device I/O sample of one host, column-major: one
// array element per device. Index i of every array describes the same device —
// see [SysNet] for the alignment contract.
//
// Separate from [SysDiskMount] because the mount table and the device list
// have independent lengths.
type SysDiskIo struct {
	_ struct{} `kind:"sysDiskIo"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindDiskIo,symbol"`
	Host string `lw:"sysmDiskIoHost,symbol"`

	Name []string `lw:"sysmDiskIoName,symbolArray"`
	// The two rates are the collector's derivation from consecutive
	// /sys/class/block/{name}/stat readings; the cumulative counters they come
	// from are not carried on the plane.
	ReadBytesPerSec  []uint64 `lw:"sysmDiskIoReadBytesPerSec,u64Array"`
	WriteBytesPerSec []uint64 `lw:"sysmDiskIoWriteBytesPerSec,u64Array"`
	// BusyPercent is the io_ticks delta over elapsed wall-time, clamped 0..100.
	BusyPercent []uint8 `lw:"sysmDiskIoBusyPct,u8Array,ct=u8h"`
}
