package sysmtee

import (
	"testing"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPsiRow_CarriesEveryResourceAndShare(t *testing.T) {
	res := func(base float32, total uint64) sysmsnap.PSIResource {
		return sysmsnap.PSIResource{
			Some: sysmsnap.PSIPressure{Avg10: base, Avg60: base + 1, Avg300: base + 2, TotalUs: total},
			Full: sysmsnap.PSIPressure{Avg10: base + 3, Avg60: base + 4, Avg300: base + 5, TotalUs: total + 1},
		}
	}
	row, err := psiRow("box1", &sysmsnap.PSISnapshot{
		Available: true,
		CPU:       res(10, 1000),
		Memory:    res(20, 2000),
		IO:        res(30, 3000),
	}, testTs)
	require.NoError(t, err)

	assert.True(t, row.Available)
	// Spot-check one field per (resource, share) pair: the risk in a block this
	// wide is a mis-paired assignment, which shows up as a value from the wrong
	// resource rather than as a missing one.
	assert.EqualValues(t, 10, row.CpuSomeAvg10)
	assert.EqualValues(t, 13, row.CpuFullAvg10)
	assert.EqualValues(t, 21, row.MemorySomeAvg60)
	assert.EqualValues(t, 24, row.MemoryFullAvg60)
	assert.EqualValues(t, 32, row.IoSomeAvg300)
	assert.EqualValues(t, 35, row.IoFullAvg300)
	assert.EqualValues(t, 1000, row.CpuSomeTotalUs)
	assert.EqualValues(t, 2001, row.MemoryFullTotalUs)
	assert.EqualValues(t, 3000, row.IoSomeTotalUs)
}

// Available is what separates "kernel without CONFIG_PSI" from "nothing
// stalled". Both are all-zero readings, so only this flag tells them apart.
func TestPsiRow_UnavailableIsNotZeroPressure(t *testing.T) {
	row, err := psiRow("box1", &sysmsnap.PSISnapshot{Available: false}, testTs)
	require.NoError(t, err)
	assert.False(t, row.Available)
	assert.EqualValues(t, 0, row.CpuSomeAvg10)
}

// The alignment contract: index i of every array must describe the same
// interface. Nothing in leeway enforces it across sections, so it is asserted
// here — the failure it guards against is a row that reads back plausibly with
// one interface's name against another's counters.
func TestNetRow_ArraysAreAlignedAndEquiLength(t *testing.T) {
	row, err := netRow("box1", &sysmsnap.NetSnapshot{Interfaces: []sysmsnap.NetInterface{
		{Name: "eth0", Index: 2, HardwareAddr: "aa:bb", Up: true, Running: true, RxBytes: 100, TxBytes: 200, RxBytesPerSec: 10, TxBytesPerSec: 20},
		{Name: "lo", Index: 1, HardwareAddr: "", Up: true, Running: false, RxBytes: 300, TxBytes: 400, RxBytesPerSec: 30, TxBytesPerSec: 40},
	}}, testTs)
	require.NoError(t, err)

	require.Len(t, row.Name, 2)
	for _, n := range []int{len(row.Index), len(row.HardwareAddr), len(row.Up), len(row.Running),
		len(row.RxBytes), len(row.TxBytes), len(row.RxBytesPerSec), len(row.TxBytesPerSec)} {
		assert.Equal(t, len(row.Name), n, "every array must have one element per interface")
	}
	// Position 0 is eth0 throughout, position 1 is lo throughout.
	assert.Equal(t, "eth0", row.Name[0])
	assert.EqualValues(t, 2, row.Index[0])
	assert.EqualValues(t, 100, row.RxBytes[0])
	assert.EqualValues(t, 1, row.Running[0])
	assert.Equal(t, "lo", row.Name[1])
	assert.EqualValues(t, 400, row.TxBytes[1])
	assert.EqualValues(t, 0, row.Running[1], "lo is not running in this fixture")
}

func TestNetRow_EmptyInterfaceListIsEmptyArrays(t *testing.T) {
	row, err := netRow("box1", &sysmsnap.NetSnapshot{}, testTs)
	require.NoError(t, err)
	assert.Empty(t, row.Name)
	assert.Empty(t, row.RxBytes)
}

// The mount table and the block-device list have independent lengths, which is
// exactly why they are two kinds: one entity cannot hold two aligned groups of
// different length without the arrays lying about which row they describe.
func TestDiskRows_SplitIntoTwoIndependentlyLengthedKinds(t *testing.T) {
	snap := &sysmsnap.DiskSnapshot{
		Mounts: []sysmsnap.DiskMount{
			{Device: "/dev/sda1", MountPoint: "/", FSType: "ext4", BlockName: "sda1", Real: true,
				Capacity: sysmsnap.DiskCapacity{TotalBytes: 100, FreeBytes: 40, UsedBytes: 60, UsedPercent: 60}},
			{Device: "tmpfs", MountPoint: "/run", FSType: "tmpfs", Real: false,
				Capacity: sysmsnap.DiskCapacity{TotalBytes: 10, FreeBytes: 9, UsedBytes: 1, UsedPercent: 10}},
			{Device: "/dev/sdb1", MountPoint: "/data", FSType: "xfs", BlockName: "sdb1", Real: true},
		},
		BlockDevices: []sysmsnap.BlockDevice{
			{Name: "sda1", ReadBytesPerSec: 1000, WriteBytesPerSec: 2000, BusyPercent: 5},
		},
	}

	mounts, err := diskMountRow("box1", snap, testTs)
	require.NoError(t, err)
	io, err := diskIoRow("box1", snap, testTs)
	require.NoError(t, err)

	assert.Len(t, mounts.Device, 3)
	assert.Len(t, io.Name, 1)
	assert.NotEqual(t, mounts.Id, io.Id, "the two groups are separate series")

	assert.EqualValues(t, 1, mounts.Real[0], "ext4 is a real filesystem")
	assert.EqualValues(t, 0, mounts.Real[1], "tmpfs is not")
	assert.EqualValues(t, 60, mounts.UsedPercent[0])
	assert.Equal(t, "sda1", io.Name[0])
	assert.EqualValues(t, 5, io.BusyPercent[0])
}

// Batteries and mains adapters are two groups in one kind, each internally
// aligned but with lengths that differ in practice.
func TestBatteryRow_TwoGroupsWithIndependentLengths(t *testing.T) {
	row, err := batteryRow("box1", &sysmsnap.BatterySnapshot{
		Batteries: []sysmsnap.BatteryStatus{
			{Name: "BAT0", Type: "Battery", Percent: 80, State: sysmsnap.StateDischarging,
				PowerWatts: 12.5, SecondsToFull: -1, SecondsToEmpty: 3600},
		},
		ACAdapters: []sysmsnap.ACAdapter{{Name: "AC", Online: false}, {Name: "ADP1", Online: true}},
	}, testTs)
	require.NoError(t, err)

	require.Len(t, row.Name, 1)
	require.Len(t, row.AcName, 2)
	assert.EqualValues(t, 80, row.Percent[0])
	assert.EqualValues(t, uint8(sysmsnap.StateDischarging), row.State[0],
		"the numeric code is stored, not the label")
	// -1 is the collector's "not applicable in this state" sentinel and must
	// survive as a negative rather than clamping to zero.
	assert.EqualValues(t, -1, row.SecondsToFull[0])
	assert.EqualValues(t, 3600, row.SecondsToEmpty[0])
	assert.EqualValues(t, 0, row.AcOnline[0])
	assert.EqualValues(t, 1, row.AcOnline[1])
}

func TestGpuRow_ArraysAreAlignedAcrossVendors(t *testing.T) {
	row, err := gpuRow("box1", &sysmsnap.GPUSnapshot{Devices: []sysmsnap.GPUDevice{
		{Vendor: "amd", Index: 0, Name: "Radeon", PCIID: "0x1234", BusyPercent: 30,
			MemoryUsedBytes: 1 << 30, MemoryTotalBytes: 8 << 30, PowerWatts: 45, TempC: 60, FreqMHz: 2100},
		{Vendor: "intel", Index: 0, Name: "Iris", PCIID: "0x9a49", BusyPercent: 5},
	}}, testTs)
	require.NoError(t, err)

	require.Len(t, row.Vendor, 2)
	assert.Equal(t, len(row.Vendor), len(row.FreqMHz))
	assert.Equal(t, "amd", row.Vendor[0])
	assert.EqualValues(t, 2100, row.FreqMHz[0])
	// Both devices are index 0 of their own vendor — neither field identifies a
	// device alone, which is why both are stored.
	assert.EqualValues(t, 0, row.Index[0])
	assert.EqualValues(t, 0, row.Index[1])
	assert.Equal(t, "intel", row.Vendor[1])
}

func TestB2u(t *testing.T) {
	assert.EqualValues(t, 1, b2u(true))
	assert.EqualValues(t, 0, b2u(false))
}
