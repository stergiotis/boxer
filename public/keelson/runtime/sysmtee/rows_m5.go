package sysmtee

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

const (
	kindProc    = "sysProc"
	kindProcCmd = "sysProcCmd"
	kindSocket  = "sysSocket"
)

const (
	domainProc    = string(sysmsnap.DomainProc)
	domainSockets = string(sysmsnap.DomainSockets)
)

// procRow transposes the process table. The command line, user and ids are not
// here — see [procCmdRow].
func procRow(host string, procs []sysmsnap.ProcInfo, ts time.Time) (row sysmfacts.SysProc, err error) {
	nk, err := entityNaturalKey(host, domainProc)
	if err != nil {
		return
	}
	n := len(procs)
	row = sysmfacts.SysProc{
		Id:         entityKey(host, domainProc),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindProc,
		Host:       host,

		Pid:             make([]uint32, n),
		Ppid:            make([]uint32, n),
		Name:            make([]string, n),
		State:           make([]string, n),
		CPUPercent:      make([]float32, n),
		RSSBytes:        make([]uint64, n),
		VMSizeBytes:     make([]uint64, n),
		NumThreads:      make([]int32, n),
		Nice:            make([]int32, n),
		Priority:        make([]int32, n),
		KernelThread:    make([]uint8, n),
		StartedAtUnixMs: make([]int64, n),
		Component:       make([]string, n),
		CgroupUnit:      make([]string, n),
	}
	for i, p := range procs {
		row.Pid[i] = p.PID
		row.Ppid[i] = p.PPID
		row.Name[i] = p.Name
		row.State[i] = procStateLabel(p.State)
		row.CPUPercent[i] = p.CPUPercent
		row.RSSBytes[i] = p.RSSBytes
		row.VMSizeBytes[i] = p.VMSizeBytes
		row.NumThreads[i] = p.NumThreads
		row.Nice[i] = p.Nice
		row.Priority[i] = p.Priority
		row.KernelThread[i] = b2u(p.KernelThread)
		row.StartedAtUnixMs[i] = p.StartedAtUnixMs
		row.Component[i] = p.Component
		row.CgroupUnit[i] = p.CgroupUnit
	}
	return
}

// procStateLabel renders the kernel's single-letter process state. A zero byte
// means the collector had nothing to report; it becomes an empty string rather
// than a NUL, which would otherwise reach a LowCardinality(String) column.
func procStateLabel(state byte) string {
	if state == 0 {
		return ""
	}
	return string([]byte{state})
}

// procCmdRow transposes the sensitive half of the process table. Only called
// when the tee is configured to persist it — see [Options.PersistProcCmd].
func procCmdRow(host string, procs []sysmsnap.ProcInfo, ts time.Time) (row sysmfacts.SysProcCmd, err error) {
	nk, err := entityNaturalKey(host, domainProc+"/cmd")
	if err != nil {
		return
	}
	n := len(procs)
	row = sysmfacts.SysProcCmd{
		Id:         entityKey(host, domainProc+"/cmd"),
		NaturalKey: nk,
		Ts:         ts,
		Kind:       kindProcCmd,
		Host:       host,

		Pid:  make([]uint32, n),
		Cmd:  make([]string, n),
		User: make([]string, n),
		Uid:  make([]uint32, n),
		Gid:  make([]uint32, n),
	}
	for i, p := range procs {
		row.Pid[i] = p.PID
		row.Cmd[i] = p.Cmd
		row.User[i] = p.User
		row.Uid[i] = p.UID
		row.Gid[i] = p.GID
	}
	return
}

// socketRow transposes the listening-socket table.
//
// Ts is the collector's own stamp, not the bundle's: the sockets collector runs
// on a slower cadence and consecutive bundles repeat one snapshot, so dating a
// row by the bundle would put many rows on the Order lane for one observation.
func socketRow(host string, snap *sysmsnap.SocketsSnapshot) (row sysmfacts.SysSocket, err error) {
	nk, err := entityNaturalKey(host, domainSockets)
	if err != nil {
		return
	}
	n := len(snap.Sockets)
	row = sysmfacts.SysSocket{
		Id:         entityKey(host, domainSockets),
		NaturalKey: nk,
		Ts:         time.UnixMilli(snap.CollectedAtUnixMs).UTC(),
		Kind:       kindSocket,
		Host:       host,

		Proto: make([]string, n),
		Addr:  make([]string, n),
		Port:  make([]uint16, n),
		Inode: make([]uint64, n),
		Uid:   make([]uint32, n),
		Pid:   make([]uint32, n),
	}
	for i, s := range snap.Sockets {
		row.Proto[i] = string(s.Proto)
		row.Addr[i] = s.Addr
		row.Port[i] = s.Port
		row.Inode[i] = s.Inode
		row.Uid[i] = s.UID
		row.Pid[i] = s.PID
	}
	return
}
