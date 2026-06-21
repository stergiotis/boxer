package sysmsnap

// DiskCapacity is one filesystem's space accounting.
type DiskCapacity struct {
	TotalBytes  uint64
	FreeBytes   uint64 // available to non-root (matches statvfs f_bavail)
	UsedBytes   uint64
	UsedPercent float32
}

// DiskMount is one /proc/self/mounts entry.
type DiskMount struct {
	Device     string // raw device field, e.g. "/dev/sda1" or "tmpfs" or "none"
	MountPoint string // e.g. "/", "/home"
	FSType     string // e.g. "ext4", "tmpfs"
	Options    string // raw options string from the mount entry
	BlockName  string // basename of canonical /dev path, "" when not a block device
	Real       bool   // true when fstype is in /proc/filesystems without "nodev" prefix
	Capacity   DiskCapacity
}

// BlockDevice is one /sys/class/block/{name}/stat reading with derived
// per-second rates and busy percent.
type BlockDevice struct {
	Name             string // "sda1", "nvme0n1p1", "dm-2"
	ReadBytesPerSec  uint64
	WriteBytesPerSec uint64
	BusyPercent      uint8 // io_ticks delta / elapsed wall-time, clamped 0..100
}

// DiskSnapshot is a single sample of the mount table and per-block-device I/O.
type DiskSnapshot struct {
	SampledAtUnixMs int64
	Mounts          []DiskMount
	BlockDevices    []BlockDevice
}
