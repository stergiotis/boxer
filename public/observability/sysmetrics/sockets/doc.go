// Package sockets samples the host's listening sockets from
// /proc/net/{tcp,tcp6,udp,udp6,unix} and attributes each to its owning
// pid via the /proc/[pid]/fd socket-inode walk — the observed-topology
// listener table of ADR-0126.
//
// Topology changes slowly, so the collector owns a per-domain cadence
// (ADR-0126 §SD4): [Collector.Sample] performs a real collection only
// when [Options.Interval] has elapsed and returns the cached snapshot
// between due times; [sysmsnap.SocketsSnapshot.CollectedAtUnixMs] dates
// the pass a bundle carries.
//
// Attribution is best-effort, partial over absent: reading another
// uid's fd table needs ptrace-class privilege, so rows whose owner is
// unreadable are published with PID 0 rather than dropped
// (ADR-0126 §SD3).
//
// # See also
//
//   - doc/adr/0126-appliance-topology-as-data.md
package sockets
