// Package container detects which (if any) container runtime hosts the
// current process, exposing a typed [EngineE] plus an optional Detail
// string for runtimes that publish a free-form identifier (e.g.
// systemd-nspawn).
//
// Detection precedence (first match wins):
//
//  1. /run/.containerenv exists → [EnginePodman]
//  2. /.dockerenv exists       → [EngineDocker]
//  3. /run/systemd/container   → content classified into [EngineSystemdNspawn]
//                                or kept verbatim in Detail when unknown
//  4. /proc/1/cgroup contains  → kubepods → [EngineKubernetes]
//                                docker   → [EngineDocker]
//                                podman   → [EnginePodman]
//                                lxc      → [EngineLXC]
//  5. otherwise                → [EngineNone]
//
// Provenance: btop src/btop_shared.cpp:295-313 (detect_container).
// btop's detector covers steps 1-3; we extend with /proc/1/cgroup
// substring matching for the kubernetes / nerdctl / unprivileged-LXC
// cases that don't drop a marker file.
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md (M4).
package container
