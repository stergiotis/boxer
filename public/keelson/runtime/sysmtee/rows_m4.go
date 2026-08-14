package sysmtee

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Kind labels and domain tokens for the ADR-0184 M4 domains.
const (
	kindPsi       = "sysPsi"
	kindNet       = "sysNet"
	kindDiskMount = "sysDiskMount"
	kindDiskIo    = "sysDiskIo"
	kindBattery   = "sysBattery"
	kindGpu       = "sysGpu"
)

const (
	domainPsi     = string(sysmsnap.DomainPSI)
	domainNet     = string(sysmsnap.DomainNet)
	domainDisk    = string(sysmsnap.DomainDisk)
	domainBattery = string(sysmsnap.DomainBattery)
	domainGpu     = string(sysmsnap.DomainGPU)
)

// b2u renders a flag for a u8 lane. The facts schema has a scalar `bool`
// section but no boolean array, and every per-item flag below needs one.
func b2u(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func psiRow(host string, snap *sysmsnap.PSISnapshot, ts time.Time) (row sysmfacts.SysPsi, err error) {
	nk, err := entityNaturalKey(host, domainPsi)
	if err != nil {
		return
	}
	row = sysmfacts.SysPsi{
		Id:         entityKey(host, domainPsi),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindPsi,
		Host:       host,
		Available:  snap.Available,

		CpuSomeAvg10:   snap.CPU.Some.Avg10,
		CpuSomeAvg60:   snap.CPU.Some.Avg60,
		CpuSomeAvg300:  snap.CPU.Some.Avg300,
		CpuSomeTotalUs: snap.CPU.Some.TotalUs,
		CpuFullAvg10:   snap.CPU.Full.Avg10,
		CpuFullAvg60:   snap.CPU.Full.Avg60,
		CpuFullAvg300:  snap.CPU.Full.Avg300,
		CpuFullTotalUs: snap.CPU.Full.TotalUs,

		MemorySomeAvg10:   snap.Memory.Some.Avg10,
		MemorySomeAvg60:   snap.Memory.Some.Avg60,
		MemorySomeAvg300:  snap.Memory.Some.Avg300,
		MemorySomeTotalUs: snap.Memory.Some.TotalUs,
		MemoryFullAvg10:   snap.Memory.Full.Avg10,
		MemoryFullAvg60:   snap.Memory.Full.Avg60,
		MemoryFullAvg300:  snap.Memory.Full.Avg300,
		MemoryFullTotalUs: snap.Memory.Full.TotalUs,

		IoSomeAvg10:   snap.IO.Some.Avg10,
		IoSomeAvg60:   snap.IO.Some.Avg60,
		IoSomeAvg300:  snap.IO.Some.Avg300,
		IoSomeTotalUs: snap.IO.Some.TotalUs,
		IoFullAvg10:   snap.IO.Full.Avg10,
		IoFullAvg60:   snap.IO.Full.Avg60,
		IoFullAvg300:  snap.IO.Full.Avg300,
		IoFullTotalUs: snap.IO.Full.TotalUs,
	}
	return
}

// netRow transposes the per-interface structs into parallel arrays. Every
// array is filled in one pass over one slice, which is what makes index i mean
// the same interface in all of them — the alignment contract SysNet documents.
func netRow(host string, snap *sysmsnap.NetSnapshot, ts time.Time) (row sysmfacts.SysNet, err error) {
	nk, err := entityNaturalKey(host, domainNet)
	if err != nil {
		return
	}
	n := len(snap.Interfaces)
	row = sysmfacts.SysNet{
		Id:         entityKey(host, domainNet),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindNet,
		Host:       host,

		Name:          make([]string, n),
		Index:         make([]int32, n),
		HardwareAddr:  make([]string, n),
		Up:            make([]uint8, n),
		Running:       make([]uint8, n),
		RxBytes:       make([]uint64, n),
		TxBytes:       make([]uint64, n),
		RxBytesPerSec: make([]uint64, n),
		TxBytesPerSec: make([]uint64, n),
	}
	for i, iface := range snap.Interfaces {
		row.Name[i] = iface.Name
		row.Index[i] = iface.Index
		row.HardwareAddr[i] = iface.HardwareAddr
		row.Up[i] = b2u(iface.Up)
		row.Running[i] = b2u(iface.Running)
		row.RxBytes[i] = iface.RxBytes
		row.TxBytes[i] = iface.TxBytes
		row.RxBytesPerSec[i] = iface.RxBytesPerSec
		row.TxBytesPerSec[i] = iface.TxBytesPerSec
	}
	return
}

// diskMountRow transposes the mount table. Separate from the block-device row
// because the two lists have independent lengths.
func diskMountRow(host string, snap *sysmsnap.DiskSnapshot, ts time.Time) (row sysmfacts.SysDiskMount, err error) {
	nk, err := entityNaturalKey(host, domainDisk+"/mount")
	if err != nil {
		return
	}
	n := len(snap.Mounts)
	row = sysmfacts.SysDiskMount{
		Id:         entityKey(host, domainDisk+"/mount"),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindDiskMount,
		Host:       host,

		Device:      make([]string, n),
		MountPoint:  make([]string, n),
		FSType:      make([]string, n),
		BlockName:   make([]string, n),
		Real:        make([]uint8, n),
		TotalBytes:  make([]uint64, n),
		FreeBytes:   make([]uint64, n),
		UsedBytes:   make([]uint64, n),
		UsedPercent: make([]float32, n),
	}
	for i, m := range snap.Mounts {
		row.Device[i] = m.Device
		row.MountPoint[i] = m.MountPoint
		row.FSType[i] = m.FSType
		row.BlockName[i] = m.BlockName
		row.Real[i] = b2u(m.Real)
		row.TotalBytes[i] = m.Capacity.TotalBytes
		row.FreeBytes[i] = m.Capacity.FreeBytes
		row.UsedBytes[i] = m.Capacity.UsedBytes
		row.UsedPercent[i] = m.Capacity.UsedPercent
	}
	return
}

func diskIoRow(host string, snap *sysmsnap.DiskSnapshot, ts time.Time) (row sysmfacts.SysDiskIo, err error) {
	nk, err := entityNaturalKey(host, domainDisk+"/io")
	if err != nil {
		return
	}
	n := len(snap.BlockDevices)
	row = sysmfacts.SysDiskIo{
		Id:         entityKey(host, domainDisk+"/io"),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindDiskIo,
		Host:       host,

		Name:             make([]string, n),
		ReadBytesPerSec:  make([]uint64, n),
		WriteBytesPerSec: make([]uint64, n),
		BusyPercent:      make([]uint8, n),
	}
	for i, d := range snap.BlockDevices {
		row.Name[i] = d.Name
		row.ReadBytesPerSec[i] = d.ReadBytesPerSec
		row.WriteBytesPerSec[i] = d.WriteBytesPerSec
		row.BusyPercent[i] = d.BusyPercent
	}
	return
}

// batteryRow transposes both power-supply groups. The battery arrays and the
// adapter arrays have independent lengths and are not aligned with each other.
func batteryRow(host string, snap *sysmsnap.BatterySnapshot, ts time.Time) (row sysmfacts.SysBattery, err error) {
	nk, err := entityNaturalKey(host, domainBattery)
	if err != nil {
		return
	}
	nb, na := len(snap.Batteries), len(snap.ACAdapters)
	row = sysmfacts.SysBattery{
		Id:         entityKey(host, domainBattery),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindBattery,
		Host:       host,

		Name:           make([]string, nb),
		Type:           make([]string, nb),
		Percent:        make([]uint8, nb),
		State:          make([]uint8, nb),
		PowerWatts:     make([]float32, nb),
		SecondsToFull:  make([]int64, nb),
		SecondsToEmpty: make([]int64, nb),

		AcName:   make([]string, na),
		AcOnline: make([]uint8, na),
	}
	for i, b := range snap.Batteries {
		row.Name[i] = b.Name
		row.Type[i] = b.Type
		row.Percent[i] = b.Percent
		row.State[i] = uint8(b.State)
		row.PowerWatts[i] = b.PowerWatts
		row.SecondsToFull[i] = b.SecondsToFull
		row.SecondsToEmpty[i] = b.SecondsToEmpty
	}
	for i, a := range snap.ACAdapters {
		row.AcName[i] = a.Name
		row.AcOnline[i] = b2u(a.Online)
	}
	return
}

func gpuRow(host string, snap *sysmsnap.GPUSnapshot, ts time.Time) (row sysmfacts.SysGpu, err error) {
	nk, err := entityNaturalKey(host, domainGpu)
	if err != nil {
		return
	}
	n := len(snap.Devices)
	row = sysmfacts.SysGpu{
		Id:         entityKey(host, domainGpu),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindGpu,
		Host:       host,

		Vendor:           make([]string, n),
		Index:            make([]int32, n),
		Name:             make([]string, n),
		PCIID:            make([]string, n),
		BusyPercent:      make([]uint8, n),
		MemoryUsedBytes:  make([]uint64, n),
		MemoryTotalBytes: make([]uint64, n),
		PowerWatts:       make([]float32, n),
		TempC:            make([]float32, n),
		FreqMHz:          make([]uint32, n),
	}
	for i, d := range snap.Devices {
		row.Vendor[i] = d.Vendor
		row.Index[i] = d.Index
		row.Name[i] = d.Name
		row.PCIID[i] = d.PCIID
		row.BusyPercent[i] = d.BusyPercent
		row.MemoryUsedBytes[i] = d.MemoryUsedBytes
		row.MemoryTotalBytes[i] = d.MemoryTotalBytes
		row.PowerWatts[i] = d.PowerWatts
		row.TempC[i] = d.TempC
		row.FreqMHz[i] = d.FreqMHz
	}
	return
}
