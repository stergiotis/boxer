package sysmreplay

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// u2b reads back a flag the tee wrote onto a u8 lane. The facts schema has a
// scalar bool section but no boolean array, so every per-item flag is 0/1.
func u2b(v uint8) (b bool) {
	b = v != 0
	return
}

// arr names one of a kind's parallel arrays for the alignment check.
type arr struct {
	name string
	n    int
}

// coLen returns the common length of a kind's parallel arrays, refusing a row
// whose arrays disagree.
//
// The arrays of one kind live in different leeway sections, so ADR-0181 §SD5's
// co-length audit does not reach them and their alignment is the writer's
// contract rather than the schema's. Truncating to the shortest would answer
// plausibly and wrongly — one interface's name against another's counters — so
// a disagreement is an error, not a repair.
func coLen(kind string, arrs ...arr) (n int, err error) {
	if len(arrs) == 0 {
		return
	}
	n = arrs[0].n
	for _, a := range arrs[1:] {
		if a.n != n {
			n = 0
			err = eb.Build().Str("kind", kind).Str("array", arrs[0].name).Int("entries", arrs[0].n).
				Str("otherArray", a.name).Int("otherEntries", a.n).
				Errorf("sysmreplay: misaligned row — index i must name the same item in every array")
			return
		}
	}
	return
}

// CPUFrom rebuilds the CPU sample. info carries the descriptor fields, which
// live in their own kind because the tee writes them once per host rather than
// per tick; nil leaves ModelName and LogicalCores at their zero values.
//
// The per-core arrays are not co-length checked: the tee passes them through
// from the collector rather than transposing them out of one item slice, so
// their lengths are the collector's business and a mismatch here would refuse
// a row the writer legitimately produced.
func CPUFrom(row sysmfacts.SysCpu, info *sysmfacts.SysCpuInfo) (snap *sysmsnap.CPUSnapshot, err error) {
	snap = &sysmsnap.CPUSnapshot{
		SampledAtUnixMs: row.Ts.UnixMilli(),
		TotalPercent:    row.TotalPercent,
		PerCorePercent:  row.PerCorePercent,
		PerCoreFreqMHz:  row.PerCoreFreqMHz,
		LoadAvg1:        row.LoadAvg1,
		LoadAvg5:        row.LoadAvg5,
		LoadAvg15:       row.LoadAvg15,
		ActiveCPUs:      row.ActiveCPUs,
	}
	// Absence carries availability: the tee stores no watts rather than zero
	// watts on a host without RAPL, so "no reading" and "idle" stay distinct.
	if row.UsageWatts.Has {
		snap.UsageWatts = row.UsageWatts.Val
		snap.UsageWattsAvailable = true
	}
	if info != nil {
		snap.ModelName = info.ModelName
		snap.LogicalCores = info.LogicalCores
	}
	return
}

// MemFrom rebuilds the memory sample.
func MemFrom(row sysmfacts.SysMem) (snap *sysmsnap.MemSnapshot, err error) {
	snap = &sysmsnap.MemSnapshot{
		SampledAtUnixMs: row.Ts.UnixMilli(),
		TotalBytes:      row.TotalBytes,
		FreeBytes:       row.FreeBytes,
		AvailableBytes:  row.AvailableBytes,
		BuffersBytes:    row.BuffersBytes,
		CachedBytes:     row.CachedBytes,
		SwapTotalBytes:  row.SwapTotalBytes,
		SwapFreeBytes:   row.SwapFreeBytes,
		UsedBytes:       row.UsedBytes,
		SwapUsedBytes:   row.SwapUsedBytes,
		ARCSizeBytes:    row.ARCSizeBytes,
		ARCMinBytes:     row.ARCMinBytes,
	}
	return
}

// PSIFrom rebuilds the pressure sample.
func PSIFrom(row sysmfacts.SysPsi) (snap *sysmsnap.PSISnapshot, err error) {
	snap = &sysmsnap.PSISnapshot{
		SampledAtUnixMs: row.Ts.UnixMilli(),
		Available:       row.Available,
		CPU: sysmsnap.PSIResource{
			Some: sysmsnap.PSIPressure{Avg10: row.CpuSomeAvg10, Avg60: row.CpuSomeAvg60, Avg300: row.CpuSomeAvg300, TotalUs: row.CpuSomeTotalUs},
			Full: sysmsnap.PSIPressure{Avg10: row.CpuFullAvg10, Avg60: row.CpuFullAvg60, Avg300: row.CpuFullAvg300, TotalUs: row.CpuFullTotalUs},
		},
		Memory: sysmsnap.PSIResource{
			Some: sysmsnap.PSIPressure{Avg10: row.MemorySomeAvg10, Avg60: row.MemorySomeAvg60, Avg300: row.MemorySomeAvg300, TotalUs: row.MemorySomeTotalUs},
			Full: sysmsnap.PSIPressure{Avg10: row.MemoryFullAvg10, Avg60: row.MemoryFullAvg60, Avg300: row.MemoryFullAvg300, TotalUs: row.MemoryFullTotalUs},
		},
		IO: sysmsnap.PSIResource{
			Some: sysmsnap.PSIPressure{Avg10: row.IoSomeAvg10, Avg60: row.IoSomeAvg60, Avg300: row.IoSomeAvg300, TotalUs: row.IoSomeTotalUs},
			Full: sysmsnap.PSIPressure{Avg10: row.IoFullAvg10, Avg60: row.IoFullAvg60, Avg300: row.IoFullAvg300, TotalUs: row.IoFullTotalUs},
		},
	}
	return
}

// NetFrom rebuilds the interface table.
//
// The IPv4 and IPv6 lists are not restored: a nested list per element does not
// flatten onto parallel arrays, so the tee never stored them (ADR-0184 §M4).
func NetFrom(row sysmfacts.SysNet) (snap *sysmsnap.NetSnapshot, err error) {
	n, err := coLen("sysNet",
		arr{"Name", len(row.Name)}, arr{"Index", len(row.Index)},
		arr{"HardwareAddr", len(row.HardwareAddr)}, arr{"Up", len(row.Up)},
		arr{"Running", len(row.Running)}, arr{"RxBytes", len(row.RxBytes)},
		arr{"TxBytes", len(row.TxBytes)}, arr{"RxBytesPerSec", len(row.RxBytesPerSec)},
		arr{"TxBytesPerSec", len(row.TxBytesPerSec)})
	if err != nil {
		return
	}
	snap = &sysmsnap.NetSnapshot{
		SampledAtUnixMs: row.Ts.UnixMilli(),
		Interfaces:      make([]sysmsnap.NetInterface, n),
	}
	for i := range n {
		snap.Interfaces[i] = sysmsnap.NetInterface{
			Name:          row.Name[i],
			Index:         row.Index[i],
			HardwareAddr:  row.HardwareAddr[i],
			Up:            u2b(row.Up[i]),
			Running:       u2b(row.Running[i]),
			RxBytes:       row.RxBytes[i],
			TxBytes:       row.TxBytes[i],
			RxBytesPerSec: row.RxBytesPerSec[i],
			TxBytesPerSec: row.TxBytesPerSec[i],
		}
	}
	return
}

// DiskFrom rebuilds the disk sample from its two kinds. The mount table and the
// block-device list have independent lengths, which is why the tee splits them;
// either may be nil.
//
// Mount options are not restored — the tee does not store the raw options
// string (ADR-0184 §M4).
func DiskFrom(mounts *sysmfacts.SysDiskMount, io *sysmfacts.SysDiskIo) (snap *sysmsnap.DiskSnapshot, err error) {
	snap = &sysmsnap.DiskSnapshot{}
	if mounts != nil {
		var n int
		n, err = coLen("sysDiskMount",
			arr{"Device", len(mounts.Device)}, arr{"MountPoint", len(mounts.MountPoint)},
			arr{"FSType", len(mounts.FSType)}, arr{"BlockName", len(mounts.BlockName)},
			arr{"Real", len(mounts.Real)}, arr{"TotalBytes", len(mounts.TotalBytes)},
			arr{"FreeBytes", len(mounts.FreeBytes)}, arr{"UsedBytes", len(mounts.UsedBytes)},
			arr{"UsedPercent", len(mounts.UsedPercent)})
		if err != nil {
			snap = nil
			return
		}
		snap.SampledAtUnixMs = mounts.Ts.UnixMilli()
		snap.Mounts = make([]sysmsnap.DiskMount, n)
		for i := range n {
			snap.Mounts[i] = sysmsnap.DiskMount{
				Device:     mounts.Device[i],
				MountPoint: mounts.MountPoint[i],
				FSType:     mounts.FSType[i],
				BlockName:  mounts.BlockName[i],
				Real:       u2b(mounts.Real[i]),
				Capacity: sysmsnap.DiskCapacity{
					TotalBytes:  mounts.TotalBytes[i],
					FreeBytes:   mounts.FreeBytes[i],
					UsedBytes:   mounts.UsedBytes[i],
					UsedPercent: mounts.UsedPercent[i],
				},
			}
		}
	}
	if io != nil {
		var n int
		n, err = coLen("sysDiskIo",
			arr{"Name", len(io.Name)}, arr{"ReadBytesPerSec", len(io.ReadBytesPerSec)},
			arr{"WriteBytesPerSec", len(io.WriteBytesPerSec)}, arr{"BusyPercent", len(io.BusyPercent)})
		if err != nil {
			snap = nil
			return
		}
		snap.SampledAtUnixMs = io.Ts.UnixMilli()
		snap.BlockDevices = make([]sysmsnap.BlockDevice, n)
		for i := range n {
			snap.BlockDevices[i] = sysmsnap.BlockDevice{
				Name:             io.Name[i],
				ReadBytesPerSec:  io.ReadBytesPerSec[i],
				WriteBytesPerSec: io.WriteBytesPerSec[i],
				BusyPercent:      io.BusyPercent[i],
			}
		}
	}
	return
}

// BatteryFrom rebuilds both power-supply groups. The battery arrays and the
// adapter arrays have independent lengths and are checked separately.
func BatteryFrom(row sysmfacts.SysBattery) (snap *sysmsnap.BatterySnapshot, err error) {
	nb, err := coLen("sysBattery",
		arr{"Name", len(row.Name)}, arr{"Type", len(row.Type)},
		arr{"Percent", len(row.Percent)}, arr{"State", len(row.State)},
		arr{"PowerWatts", len(row.PowerWatts)}, arr{"SecondsToFull", len(row.SecondsToFull)},
		arr{"SecondsToEmpty", len(row.SecondsToEmpty)})
	if err != nil {
		return
	}
	na, err := coLen("sysBattery adapters",
		arr{"AcName", len(row.AcName)}, arr{"AcOnline", len(row.AcOnline)})
	if err != nil {
		return
	}
	snap = &sysmsnap.BatterySnapshot{
		SampledAtUnixMs: row.Ts.UnixMilli(),
		Batteries:       make([]sysmsnap.BatteryStatus, nb),
		ACAdapters:      make([]sysmsnap.ACAdapter, na),
	}
	for i := range nb {
		snap.Batteries[i] = sysmsnap.BatteryStatus{
			Name:           row.Name[i],
			Type:           row.Type[i],
			Percent:        row.Percent[i],
			State:          sysmsnap.StateE(row.State[i]),
			PowerWatts:     row.PowerWatts[i],
			SecondsToFull:  row.SecondsToFull[i],
			SecondsToEmpty: row.SecondsToEmpty[i],
		}
	}
	for i := range na {
		snap.ACAdapters[i] = sysmsnap.ACAdapter{Name: row.AcName[i], Online: u2b(row.AcOnline[i])}
	}
	return
}

// GPUFrom rebuilds the device table.
func GPUFrom(row sysmfacts.SysGpu) (snap *sysmsnap.GPUSnapshot, err error) {
	n, err := coLen("sysGpu",
		arr{"Vendor", len(row.Vendor)}, arr{"Index", len(row.Index)},
		arr{"Name", len(row.Name)}, arr{"PCIID", len(row.PCIID)},
		arr{"BusyPercent", len(row.BusyPercent)}, arr{"MemoryUsedBytes", len(row.MemoryUsedBytes)},
		arr{"MemoryTotalBytes", len(row.MemoryTotalBytes)}, arr{"PowerWatts", len(row.PowerWatts)},
		arr{"TempC", len(row.TempC)}, arr{"FreqMHz", len(row.FreqMHz)})
	if err != nil {
		return
	}
	snap = &sysmsnap.GPUSnapshot{
		SampledAtUnixMs: row.Ts.UnixMilli(),
		Devices:         make([]sysmsnap.GPUDevice, n),
	}
	for i := range n {
		snap.Devices[i] = sysmsnap.GPUDevice{
			Vendor:           row.Vendor[i],
			Index:            row.Index[i],
			Name:             row.Name[i],
			PCIID:            row.PCIID[i],
			BusyPercent:      row.BusyPercent[i],
			MemoryUsedBytes:  row.MemoryUsedBytes[i],
			MemoryTotalBytes: row.MemoryTotalBytes[i],
			PowerWatts:       row.PowerWatts[i],
			TempC:            row.TempC[i],
			FreqMHz:          row.FreqMHz[i],
		}
	}
	return
}

// ProcsFrom rebuilds the process table. cmd carries the command line, user name
// and ids, which live in their own opt-in kind (ADR-0184 §SD8) and are absent
// from a deployment that did not ask for them; nil leaves those fields empty.
//
// The two kinds are joined on pid rather than by position. Both are transposed
// from one slice in one pass, so position would work today, but a join across
// kinds that happens to hold is not a contract either of them states.
func ProcsFrom(row sysmfacts.SysProc, cmd *sysmfacts.SysProcCmd) (procs []sysmsnap.ProcInfo, err error) {
	n, err := coLen("sysProc",
		arr{"Pid", len(row.Pid)}, arr{"Ppid", len(row.Ppid)},
		arr{"Name", len(row.Name)}, arr{"State", len(row.State)},
		arr{"CPUPercent", len(row.CPUPercent)}, arr{"RSSBytes", len(row.RSSBytes)},
		arr{"VMSizeBytes", len(row.VMSizeBytes)}, arr{"NumThreads", len(row.NumThreads)},
		arr{"Nice", len(row.Nice)}, arr{"Priority", len(row.Priority)},
		arr{"KernelThread", len(row.KernelThread)}, arr{"StartedAtUnixMs", len(row.StartedAtUnixMs)},
		arr{"Component", len(row.Component)}, arr{"CgroupUnit", len(row.CgroupUnit)})
	if err != nil {
		return
	}
	type cmdEntry struct {
		cmd, user string
		uid, gid  uint32
	}
	var byPid map[uint32]cmdEntry
	if cmd != nil {
		var cn int
		cn, err = coLen("sysProcCmd",
			arr{"Pid", len(cmd.Pid)}, arr{"Cmd", len(cmd.Cmd)},
			arr{"User", len(cmd.User)}, arr{"Uid", len(cmd.Uid)}, arr{"Gid", len(cmd.Gid)})
		if err != nil {
			return
		}
		byPid = make(map[uint32]cmdEntry, cn)
		for i := range cn {
			byPid[cmd.Pid[i]] = cmdEntry{cmd: cmd.Cmd[i], user: cmd.User[i], uid: cmd.Uid[i], gid: cmd.Gid[i]}
		}
	}
	procs = make([]sysmsnap.ProcInfo, n)
	for i := range n {
		procs[i] = sysmsnap.ProcInfo{
			PID:             row.Pid[i],
			PPID:            row.Ppid[i],
			Name:            row.Name[i],
			State:           procState(row.State[i]),
			CPUPercent:      row.CPUPercent[i],
			RSSBytes:        row.RSSBytes[i],
			VMSizeBytes:     row.VMSizeBytes[i],
			NumThreads:      row.NumThreads[i],
			Nice:            row.Nice[i],
			Priority:        row.Priority[i],
			KernelThread:    u2b(row.KernelThread[i]),
			StartedAtUnixMs: row.StartedAtUnixMs[i],
			Component:       row.Component[i],
			CgroupUnit:      row.CgroupUnit[i],
		}
		if e, ok := byPid[row.Pid[i]]; ok {
			procs[i].Cmd = e.cmd
			procs[i].User = e.user
			procs[i].UID = e.uid
			procs[i].GID = e.gid
		}
	}
	return
}

// procState reads back the kernel's single-letter process state. The tee writes
// an empty string where the collector had nothing to report, rather than a NUL
// reaching a LowCardinality(String) column; that empty comes back as the zero
// byte it stood for.
func procState(label string) (state byte) {
	if label == "" {
		return
	}
	state = label[0]
	return
}

// SocketsFrom rebuilds the listening-socket table. The stamp is the row's own
// order value, which for this kind is the collector's CollectedAtUnixMs rather
// than the bundle's — the sockets collector runs on its own slower cadence and
// the tee dates the row by the observation, not by the tick that carried it.
func SocketsFrom(row sysmfacts.SysSocket) (snap *sysmsnap.SocketsSnapshot, err error) {
	n, err := coLen("sysSocket",
		arr{"Proto", len(row.Proto)}, arr{"Addr", len(row.Addr)},
		arr{"Port", len(row.Port)}, arr{"Inode", len(row.Inode)},
		arr{"Uid", len(row.Uid)}, arr{"Pid", len(row.Pid)})
	if err != nil {
		return
	}
	snap = &sysmsnap.SocketsSnapshot{
		CollectedAtUnixMs: row.Ts.UnixMilli(),
		Sockets:           make([]sysmsnap.SocketInfo, n),
	}
	for i := range n {
		snap.Sockets[i] = sysmsnap.SocketInfo{
			Proto: sysmsnap.SocketProto(row.Proto[i]),
			Addr:  row.Addr[i],
			Port:  row.Port[i],
			Inode: row.Inode[i],
			UID:   row.Uid[i],
			PID:   row.Pid[i],
		}
	}
	return
}
