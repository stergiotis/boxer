package sysmsnap

// SocketProto identifies the socket family/protocol of a listener row.
// Values match the /proc/net file names they are parsed from.
type SocketProto string

const (
	SocketProtoTCP  SocketProto = "tcp"
	SocketProtoTCP6 SocketProto = "tcp6"
	SocketProtoUDP  SocketProto = "udp"
	SocketProtoUDP6 SocketProto = "udp6"
	SocketProtoUnix SocketProto = "unix"
)

// SocketInfo is one listening socket (ADR-0126 observed topology).
type SocketInfo struct {
	Proto SocketProto

	// Addr is the bound address: an IP literal for inet sockets, the
	// filesystem or @abstract path for unix sockets.
	Addr string

	// Port is the bound port for inet sockets, 0 for unix.
	Port uint16

	// Inode is the socket inode — the join key the fd walk attributes
	// pids by.
	Inode uint64

	// UID is the socket owner uid as reported by the kernel table.
	UID uint32

	// PID is the process holding the socket, 0 when unattributed (the
	// owning process's fd table was unreadable — a privilege boundary —
	// or the socket changed hands mid-walk). Partial over absent:
	// unattributed rows are still published (ADR-0126 §SD3).
	PID uint32
}

// SocketsSnapshot is the listening-socket table of one collection pass.
type SocketsSnapshot struct {
	// CollectedAtUnixMs dates the pass. The sockets collector samples on
	// its own slower cadence (ADR-0126 §SD4), so consecutive bundles may
	// repeat one snapshot; this stamp tells consumers how old it is.
	CollectedAtUnixMs int64

	Sockets []SocketInfo
}
