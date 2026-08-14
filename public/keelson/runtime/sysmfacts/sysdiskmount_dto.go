package sysmfacts

import "time"

// SysDiskMount is one filesystem-capacity sample of one host, column-major:
// one array element per mount entry. Index i of every array below describes
// the same mount — see [SysNet] for the alignment contract these parallel
// arrays carry.
//
// Block-device I/O is a separate kind ([SysDiskIo]) rather than more arrays
// here: the mount table and the device list have independent lengths, and one
// entity per aligned group keeps every array in a row the same length.
//
// The raw mount options string is deliberately absent — high-cardinality text
// that no metric question asks about.
type SysDiskMount struct {
	_ struct{} `kind:"sysDiskMount"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindDiskMount,symbol"`
	Host string `lw:"sysmDiskMountHost,symbol"`

	Device     []string `lw:"sysmDiskMountDevice,symbolArray"`
	MountPoint []string `lw:"sysmDiskMountPoint,symbolArray"`
	FSType     []string `lw:"sysmDiskMountFsType,symbolArray"`
	// BlockName is the basename of the canonical /dev path, empty when the
	// entry is not a block device.
	BlockName []string `lw:"sysmDiskMountBlockName,symbolArray"`
	// Real is 1 for a filesystem listed in /proc/filesystems without the
	// "nodev" prefix — the usual way to tell a real mount from a virtual one.
	Real []uint8 `lw:"sysmDiskMountReal,u8Array,ct=u8h"`

	TotalBytes []uint64 `lw:"sysmDiskMountTotalBytes,u64Array"`
	// FreeBytes is what is available to non-root, matching statvfs f_bavail,
	// so Total - Free is not Used.
	FreeBytes   []uint64  `lw:"sysmDiskMountFreeBytes,u64Array"`
	UsedBytes   []uint64  `lw:"sysmDiskMountUsedBytes,u64Array"`
	UsedPercent []float32 `lw:"sysmDiskMountUsedPct,f32Array"`
}
