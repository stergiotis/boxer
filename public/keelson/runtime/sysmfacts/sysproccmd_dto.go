package sysmfacts

import "time"

// SysProcCmd is process identity and invocation: the ADR-0090 §SD8 sensitive
// class, in its own kind and written only when a deployment opts in.
//
// # Why a separate kind rather than a tagged attribute
//
// §SD8 designed a `sensitive` membership carried alongside each sensitive
// attribute's own, so the tag would travel with the data. For the stored form
// two things make separation better. A component DTO binds one membership per
// field, so the second tag is not reachable from the generated write path at
// all. And the masking switch §SD8 defers does not exist, so a tag today is an
// annotation nothing enforces — whereas a kind a deployment never writes needs
// no enforcement.
//
// It also matters that this is durable. §SD8's accepted gap was scoped to a
// single-tenant, localhost-bound bus, where a command line lives as long as the
// subscriber holds it. A row in `boxer.facts` outlives the process, is readable
// by anything with database access, and is backed up with everything else.
//
// Arrays are aligned among themselves — see [SysNet] — but not with [SysProc]'s;
// the two are separate entities and a reader joins them on Pid.
type SysProcCmd struct {
	_ struct{} `kind:"sysProcCmd"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindProcCmd,symbol"`
	Host string `lw:"sysmProcCmdHost,symbol"`

	Pid []uint32 `lw:"sysmProcCmdPid,u32Array"`
	// Cmd is /proc/[pid]/cmdline with NULs rendered as spaces. Empty for kernel
	// threads. High-cardinality by nature — it carries paths, arguments and
	// whatever a caller passed on a command line.
	Cmd  []string `lw:"sysmProcCmdLine,stringArray"`
	User []string `lw:"sysmProcCmdUser,symbolArray"`
	Uid  []uint32 `lw:"sysmProcCmdUid,u32Array"`
	Gid  []uint32 `lw:"sysmProcCmdGid,u32Array"`
}
