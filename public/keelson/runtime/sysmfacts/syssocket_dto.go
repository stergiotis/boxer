package sysmfacts

import "time"

// SysSocket is one listening-socket table of one host (ADR-0126 observed
// topology), column-major: one array element per socket. Index i of every array
// describes the same socket — see [SysNet] for the alignment contract.
//
// The sockets collector samples on its own slower cadence, so consecutive
// bundles repeat one snapshot. The tee writes a row only when the collection
// stamp advances, and stamps Ts with it rather than with the bundle's — so a
// row's Order is when the table was observed, not when it was last re-sent, and
// the series carries one row per actual observation.
type SysSocket struct {
	_ struct{} `kind:"sysSocket"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindSocket,symbol"`
	Host string `lw:"sysmSocketHost,symbol"`

	Proto []string `lw:"sysmSocketProto,symbolArray"`
	// Addr is an IP literal for inet sockets, a filesystem or @abstract path
	// for unix ones; Port is 0 for unix.
	Addr []string `lw:"sysmSocketAddr,symbolArray"`
	Port []uint16 `lw:"sysmSocketPort,u16Array"`
	// Inode is the join key the fd walk attributes pids by, kept so an
	// unattributed row can still be correlated later.
	Inode []uint64 `lw:"sysmSocketInode,u64Array"`
	Uid   []uint32 `lw:"sysmSocketUid,u32Array"`
	// Pid is 0 where the owning process's fd table was unreadable — a privilege
	// boundary. Partial over absent (ADR-0126 §SD3), so a zero means "not
	// attributed", never "owned by pid 0".
	Pid []uint32 `lw:"sysmSocketPid,u32Array"`
}
