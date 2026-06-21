---
type: adr
status: accepted
date: 2026-05-03
reviewed-by: "@spx"
reviewed-date: 2026-05-03
---

> **Status: accepted 2026-05-03 by @spx.** Implementation tracked across M1–M6 in this document.

# ADR-0019: Linux Metrics Collector under `observability/sysmetrics/`

## Context

We want a non-interactive Go library that gathers the same Linux system signals btop displays — CPU (totals, per-core, frequency, RAPL watts, temperatures, fans), memory + swap (incl. ZFS ARC), per-mount disks (capacity + I/O rates), per-interface network (bytes + speeds, with counter-rollover handling), the process table, battery, and GPU metrics for NVIDIA / AMD / Intel — without any TUI, configuration globals, or color/threshold logic. The driver use cases are observability pipelines, demo workloads for the in-repo plot stack, and ad-hoc Go tooling that wants to avoid spawning processes to read `/proc` itself.

[btop](https://github.com/aristocratos/btop) ([`../../../contrib/btop/`](../../../contrib/btop/)) is the obvious reference: its Linux collectors are well-tested, cover the gotcha matrix (cgroup-effective CPU sets, hwmon label discovery, `rx_bytes` rollover, ZFS ARC accounting, RAPL energy_uj deltas, AMD ROCm-SMI v5/v6 ABI fork) that any from-scratch implementation would re-discover one outage at a time. The license — verified at [`../../../contrib/btop/LICENSE`](../../../contrib/btop/LICENSE) — is **Apache 2.0**, identical-spirit to this repo, so we can mirror structure and cite line:file freely.

The btop UI layer (`src/btop_draw.cpp`, `src/btop_input.cpp`, `src/btop_menu.cpp`, `src/btop_config.cpp`) is out of scope. So is btop's sample history (the per-metric `deque<long long>` that feeds sparklines) — that is a UI concern. We extract only the part of btop that touches `/proc`, `/sys`, `getifaddrs(3)`, `statvfs(3)`, `perf_event_open(2)`, and the vendor GPU SDKs.

Forces the design must respect:

- **Linux-only.** This ADR explicitly does not cover macOS / BSD; later ADRs may. The Go source uses `//go:build linux` files.
- **Non-interactive and library-shaped.** No TUI, no global config, no logger singleton; collectors take options structs and return values.
- **Boxer conventions** (CLAUDE.md, [CODINGSTANDARDS.md](../../CODINGSTANDARDS.md)) — `inst` receivers, `*I` interface suffix, `*E` enum suffix, `eh.Errorf` errors, `iter.Seq2[V, error]` for variable-length sequences, sized integers on fields, zero-value-usable structs, SoA over AoS where multiplicity exists.
- **No cgo, no libstdc++.** This repo's binary footprint already carries Rust-via-FFFI2 and a Go core; pulling C++ runtime support to read `/proc/stat` is disproportionate.
- **GPU vendor SDKs are optional and gated.** btop dlopens NVML and ROCm-SMI at runtime and degrades gracefully when missing; we mirror that, but at build-tag granularity so callers can compile NVIDIA-free binaries.
- **Drift-guarded.** Kernel procfs/sysfs schemas are stable but not literally append-only; we need fixture-based tests so a 2030 kernel change in `/proc/meminfo` field ordering does not silently corrupt our counters.

## Design space (QOC)

**Question.** How do we expose btop's data-gathering layer to Go callers?

**Options.**

- **O1 — Pure-Go reimplementation** of the `/proc` + `/sys` readers, with optional `purego` loaders for vendor GPU SDKs.
- **O2 — cgo wrapper around `src/linux/btop_collect.cpp`**: extract the Cpp::collect family, expose a C-API shim, bind via cgo.
- **O3 — Subprocess btop with `--export` mode**: spawn `btop` and parse a structured-output flag.

**Criteria.**

- **C1 — Feasibility.** Does the option actually work without additional upstream changes?
- **C2 — Toolchain footprint.** cgo, libstdc++, child processes, vendored libraries.
- **C3 — Maintenance cost.** Tracking upstream btop drift vs. tracking kernel schema drift.
- **C4 — API quality.** Idiomatic Go shape (iterators, typed errors, SoA snapshots) vs. C-shim leakage.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (pure Go) | O2 (cgo)  | O3 (subprocess) |
|----|--------------|-----------|-----------------|
| C1 — Feasibility            | ++ | +  | −− |
| C2 — Toolchain footprint    | ++ | −− | −  |
| C3 — Maintenance cost       | −  | +  | n/a |
| C4 — API quality            | ++ | −  | −− |

Notes per cell:

- **O3 / C1 (−−).** Verified: btop has no batch-export mode. `src/btop_cli.cpp` only handles `--help`, `--version`, `--low-color`, `--debug`, `--utf-force`. There is nothing to subprocess against. This option is dead.
- **O2 / C1 (+).** The C++ surface is intertwined with `Config::getB(...)`, `Logger::*`, `Runner::stopping` (atomic), and `fmt::format` — workable but every call needs a context shim. The collector is C++23 (`std::ranges`, `std::filesystem`), pulling libstdc++ into a Go binary that otherwise has none.
- **O1 / C3 (−).** Reimplementation parallel-tracks upstream; we don't get free bug fixes. Bounded by (a) kernel schemas being unusually stable, (b) per-source-file `// provenance: btop ../linux/btop_collect.cpp:NNNN-MMMM` citations so a reader can diff against upstream, and (c) drift-guard fixtures (SD12 below).

## Decision

We adopt **O1 — pure-Go reimplementation** under a new package tree `public/observability/sysmetrics/`, with vendor GPU SDKs loaded at runtime via [`github.com/ebitengine/purego`](https://github.com/ebitengine/purego) (no cgo) behind build tags `gpu_nvml`, `gpu_rocm`, `gpu_intel` listed in [`../../tags`](../../tags). Intel GPU is implemented directly against `golang.org/x/sys/unix.PerfEventOpen` (no library dependency). AMD GPU prefers pure-sysfs over ROCm-SMI. NPU support is out of scope for this ADR.

The implementation is staged across six independently-shippable milestones (M1–M6 below).

## Subsidiary design decisions

- **SD1 — Namespace `public/observability/sysmetrics/`.** Adjacent to existing observability assets (`observability/profile.proto`); signals "system metrics like Prometheus node-exporter" without claiming the `metrics` name. Considered alternatives: top-level `sysprobe/`, `boxerstaging/btoplike/`. Rejected — observability is the durable home; the others embed implementation provenance into the public namespace.

- **SD2 — Per-domain `Collector` types hold prior-tick state.** CPU (`/proc/stat` deltas), disk I/O rates, network byte counters with rollover, RAPL energy_uj deltas, and the process table all need monotonic-tick deltas. Each `Collector` owns its prior-sample state with `inst` receiver; calling `Sample(ctx)` returns the current `Snapshot` and updates state. Stateless domains (memory, battery, sensors) expose a free function or a `Collector` with no internal state, both shapes valid.

- **SD3 — Snapshot structs are SoA where multiplicity is bounded and known.** Per-core CPU arrays, per-NIC byte counters, per-disk I/O — emitted as parallel slices keyed by a single index slice. Process table is the exception: variable cardinality and per-row failure modes mean an iterator (SD4) is the right shape, not SoA.

- **SD4 — Variable-length sequences are `iter.Seq2[T, error]`.** Process table, GPU device list, mounted-disk list, network-interface list. Per [boxer CODINGSTANDARDS.md](../../../boxer/CODINGSTANDARDS.md) the standard names are `All` / `Values` / `Keys` / `Backward`. Callers that want a slice call `slices.Collect2`. Per-row errors do not abort iteration unless the caller breaks; the iterator yields `(zero, err)` and the next row.

- **SD5 — GPU vendor support is build-tag-gated.** Tags listed in [`../../tags`](../../tags): `gpu_nvml`, `gpu_rocm`, `gpu_intel`. The default `./tags`-driven build includes none, matching btop's runtime degradation path. A consumer who wants NVIDIA support edits `./tags`. Each tag activates one subpackage under `gpu/`; cross-tag interaction is none.

- **SD6 — Vendor SDKs load via `purego.Dlopen` (no cgo).** Mirrors btop's runtime dlopen pattern (`btop_collect.cpp:1241,1538`) without inheriting cgo costs. `purego` is mature for NVML and ROCm-SMI surface area. The build tag from SD5 still applies — the package compiles only when its tag is present, even though no cgo is involved.

- **SD7 — Intel GPU uses `unix.PerfEventOpen` directly.** btop calls into a vendored copy of `igt-gpu-tools` (`intel_gpu_top/`, `btop_collect.cpp:1885-1998`). Inspection shows that vendoring is just a wrapper around `perf_event_open(2)` against the `i915` PMU. We open the PMU directly via `golang.org/x/sys/unix`. PCI ID → device name mapping is a static table sourced from `i915_pciids.h` (subset only — known-shipping desktop and mobile parts).

- **SD8 — AMD GPU prefers pure-sysfs over ROCm-SMI.** `/sys/class/drm/card*/device/{gpu_busy_percent, mem_info_vram_total, mem_info_vram_used, hwmon/.../{temp1_input, power1_average}}` covers the metrics btop pulls from ROCm-SMI for consumer GPUs. ROCm-SMI is engaged only when the sysfs path is missing fields the caller declares required; the fallback is gated by a runtime probe and avoids the v5/v6 ABI fork at `btop_collect.cpp:253-254` for the common case.

- **SD9 — NPU support is out of scope.** btop has no NPU collectors (`grep -in "npu\|amdxdna\|/sys/class/accel\|vpu" src/linux/btop_collect.cpp` returns zero). Implementing NPU would be net-new work without an upstream to mirror; defer to a follow-on ADR (0020+). Sysfs entry points exist (`/sys/class/accel/accel0/` for Intel NPU drivers, `/sys/class/amdxdna/` for AMD XDNA) but the schemas are not stable.

- **SD10 — No internal history / ring buffer.** btop maintains per-metric `deque<long long>` for sparkline drawing (`btop_shared.hpp` graph fields). That is a UI concern. Each `Sample()` call returns one snapshot; consumers needing rolling averages or sparklines wrap with a ring buffer downstream. This keeps the Snapshot copy small and the collector zero-allocation in the steady state.

- **SD11 — Provenance citations are mandatory.** Each Go source file's package doc-comment names the upstream btop symbol it mirrors; significant branches (e.g. cpu frequency dual-path at `btop_collect.cpp:349,699-719`) carry an inline `// provenance: btop src/linux/btop_collect.cpp:NNNN-MMMM` comment. A reader can diff against upstream when chasing a bug.

- **SD12 — Drift-guard fixtures live in `testdata/{proc,sys}/`.** Each parser test runs against a fixture tree shaped like `/proc` or `/sys`. Kernel-version-specific quirks that informed an upstream branch (RAPL `0444` perms, ZFS `arcstats` shape, `/proc/meminfo` `SReclaimable` optionality, `rx_bytes` 32-bit rollover) get their own fixture and golden output. CI compares parse output byte-for-byte against the golden.

- **SD13 — Bundle aggregator is opt-in, not the default API.** A `sysmetrics.Bundle` type orchestrates per-domain collectors via `errgroup.Group` and returns a single `BundleSnapshot` SoA struct, but it is one consumer of the per-domain APIs — not the API. Callers wanting only CPU + memory pay no cost for unused domains.

- **SD14 — No `Config` global, no `Logger` global.** Each `New*` constructor takes an explicit options struct (sample paths, hwmon root override for testing, ZFS-arcstats-on toggle). Per-call errors flow back through `eh.Errorf`. No package-level state, no init-time side effects.

## Public API sketch

Illustrative only — not the implementation contract; the REFERENCE doc seeded in M1 will pin field-level shapes.

```go
// public/observability/sysmetrics/cpu/cpu.go
package cpu

type Snapshot struct {
    SampledAtUnixMs   int64
    TotalPercent      uint8     // 0..100, derived from /proc/stat deltas
    PerCorePercent    []uint8   // SoA, indexed by logical CPU
    PerCoreFreqMHz    []uint32
    LoadAvg1, LoadAvg5, LoadAvg15 float32
    UsageWatts        float32   // RAPL; 0 if unavailable
    Temperatures      []sensors.TempReading
    ActiveCPUs        []int32   // cgroup cpuset.cpus.effective
}

type CollectorI interface {
    Sample(ctx context.Context) (snap Snapshot, err error)
}

type Collector struct{ /* prior /proc/stat counters, hwmon paths, etc. */ }

func New(opts ...OptionI) (inst *Collector, err error) { /* ... */ }

func (inst *Collector) Sample(ctx context.Context) (snap Snapshot, err error) {
    // provenance: btop src/linux/btop_collect.cpp:1080-1208 (cpu_collect)
    // ...
}

var _ CollectorI = (*Collector)(nil)
```

```go
// public/observability/sysmetrics/proc/proc.go
package proc

type Info struct {
    PID, PPID  uint32
    Comm       string
    Cmd        string
    UID        uint32
    User       string
    State      byte      // R/S/D/Z/T/...
    CPUPercent float32   // delta vs prior tick
    RSSBytes   uint64
    VMSizeBytes uint64
    StartedAtUnixMs int64
}

type CollectorI interface {
    All(ctx context.Context) iter.Seq2[Info, error]
}
```

## Implementation plan

Six milestones, each independently shippable. A green `scripts/ci/lint.sh` and `scripts/ci/gotest.sh`, a CHANGELOG entry, and a doc-update gate each transition.

### M1 — Skeleton + CPU + memory + load + sensors

`internal/procfs` (line readers), `internal/sysfs` (directory walker), `internal/deltaclock` (monotonic-tick rate). `cpu` package (`/proc/stat`, `/proc/cpuinfo`, `cpufreq/scaling_cur_freq`, `/proc/loadavg`, `intel-rapl/energy_uj`). `mem` package (`/proc/meminfo`; ZFS arcstats off by default, opt-in). `sensors` package (hwmon enumeration; temperature readings only, fans deferred). REFERENCE.md skeleton seeded via `scripts/new-doc.sh`. ADR-0019 lands at `proposed`.

**Done when:** `cpu.New().Sample(ctx)` returns a populated Snapshot on any modern x86_64 Linux, `lint.sh` green, golden-file fixture tests for `/proc/stat` parsing pass, and the package compiles under default `./tags`.

### M2 — Disk + network + battery

`disk` (mounts via `/proc/self/mounts`, capacity via `statvfs`, I/O rates via `/sys/block/{dev}/stat` with rollover). `net` (interface list via `net.Interfaces()` + sysfs IP fallback for IPv6 link-local edge cases, `/sys/class/net/{iface}/statistics/{rx,tx}_bytes` with rollover handling per `btop_collect.cpp:2760`). `battery` (`/sys/class/power_supply/BAT*` walk; both energy_now/charge_now unit branches).

**Done when:** all three domains return populated Snapshots on a laptop fixture and a server fixture (no battery, multi-disk, multi-NIC); rollover-handling test fires deterministically against a synthetic 32-bit counter.

### M3 — Process table iterator

`proc` package: `iter.Seq2[Info, error]` over `/proc/[pid]/{comm,cmdline,stat,statm,status}`. uid→user via `/etc/passwd` cache + `os/user.LookupId` fallback. Kernel-thread filter (ppid==2). Dead-pid handling (skip races on `Open`/`Read` ENOENT).

**Done when:** iteration produces a stable list against a fixture `/proc/` tree with 50 synthetic PIDs and 5 racing dead-pid scenarios; CPU-percent deltas are within 0.5% of an externally-computed reference.

### M4 — Container engine sniff + `Bundle` aggregator

`container.Detect(ctx)` mirrors btop's `Proc::detect_container` heuristic (`/proc/1/cgroup` + `/proc/1/environ` + `/.dockerenv` + `/run/.containerenv` probes) returning a typed `EngineE` enum (`docker`, `podman`, `lxc`, `kubernetes`, `unknown`, `none`). `sysmetrics.Bundle` orchestrator runs per-domain collectors via `errgroup.Group`, produces `BundleSnapshot`. HOWTO_collector_loop.md lands.

**Done when:** the Bundle wraps the M1–M3 collectors and produces a single snapshot in <5 ms on a 16-core laptop; HOWTO walks a 1 Hz collector loop end-to-end.

### M5 — Intel GPU via `perf_event_open`

`gpu/intel` under `//go:build gpu_intel`. Open the `i915` PMU via `unix.PerfEventOpen`, sample render/blitter/video engines, normalize against time. Static PCI ID → device-name table (Tiger Lake, Alder Lake, Raptor Lake, Meteor Lake desktop/mobile entries — extend as needed). `xe` driver explicitly out of scope until btop adopts it.

**Done when:** `gpu/intel` compiles under the `gpu_intel` tag, returns valid device list on a Tiger Lake machine, fails gracefully (empty slice + sentinel error) on hardware with `kernel.perf_event_paranoid > 1`.

### M6 — NVIDIA + AMD GPU

`gpu/nvml` via `purego` under `//go:build gpu_nvml`. `gpu/rocm` defaults to pure-sysfs path; ROCm-SMI fallback gated on a runtime probe. EXPLANATION_btop_provenance.md finalized — a per-domain table mapping each upstream symbol to its Go counterpart.

**Done when:** `gpu/nvml` exposes power, utilization, memory, temperature on a discrete NVIDIA GPU; `gpu/rocm` does the same on a discrete AMD GPU via sysfs; both compile cleanly under their respective tags and return empty slices when no matching hardware is present.

### Out of scope for this ADR (named follow-ons)

- **NPU support** — Intel `accel0`, AMD `amdxdna`. Separate ADR; net-new schemas with no btop precedent.
- **macOS / FreeBSD / OpenBSD / NetBSD parity.** Future per-OS ADRs.
- **Sample history / sparkline-deque.** Punted to consumers per SD10.
- **Process tree / sort / filter helpers.** UI concerns.
- **Process write side** — `set_priority(pid, nice)`, `kill(pid, sig)`. Not a metric; out of scope.
- **Built-in Prometheus / OpenTelemetry exporter.** A separate `observability/sysmetrics/exporter/` package is plausible but outside this ADR.
- **Persistence to ClickHouse / Kafka.** ADR-0005 (Kafka) is the obvious downstream for streaming snapshots; the wiring is a follow-on, not part of this ADR.

## Alternatives

- **cgo wrapper around `btop_collect.cpp` (O2).** Rejected — pulls libstdc++ into a Go binary that otherwise has none, requires shimming `Config::getB` / `Logger` / `Runner::stopping` per call, and the C++ surface is C++23 (`std::ranges`, `std::filesystem`) which raises the cgo-compile bar. The "free upstream bug fixes" benefit is undercut by the impedance shim being itself a maintenance surface. Apache 2.0 license is fine — the rejection is purely engineering.

- **Subprocess btop with a hypothetical `--export` flag (O3).** Dead — btop has no batch-export mode. Adding one upstream is plausible but not on our timeline.

- **Adopt `prometheus/procfs` (`github.com/prometheus/procfs`) as the foundation and extend it.** Not rejected outright — `procfs` covers a strict subset of our needs (CPU, memory, network, process table) but lacks GPU, RAPL, hwmon enumeration, ZFS ARC, container detection, AMD-sysfs-GPU, Intel `i915` PMU, and battery. Mixing two parser stacks is worse than one consistent stack. We may *extract* `internal/procfs` into a small reusable subpackage later if the call sites multiply, but that is not blocking M1.

- **Adopt `gopsutil` (`github.com/shirou/gopsutil`) as the foundation.** Rejected — `gopsutil` is cross-platform-by-default and pulls in a lot of code paths we will never exercise; its API shape is not boxer-style (no `iter.Seq2`, no SoA, AoS-everywhere structs). Re-skinning it would touch every type. The kernel-fixture testing approach (SD12) is also not a `gopsutil` strength.

- **Vendor `igt-gpu-tools` for Intel GPU instead of opening the PMU ourselves.** Rejected — the vendored helpers in btop's `intel_gpu_top/` are a thin wrapper around `perf_event_open`. Going direct via `golang.org/x/sys/unix` is shorter and keeps the dependency surface to the standard library family.

## Consequences

### Positive

- **No cgo, no libstdc++.** Single-binary Go output stays CGO_ENABLED=0-buildable for the default tag set; cross-compile remains trivial.
- **Idiomatic Go API.** `iter.Seq2[T, error]` for variable-length, SoA Snapshots for fixed-shape, `inst` receivers, `eh.Errorf` errors. Composes naturally with `errgroup`, `slices`, `maps`.
- **Build-tag-gated GPU support** mirrors btop's runtime degradation but at compile time. NVIDIA-free / AMD-free / Intel-free binaries are first-class.
- **Drift-guarded by fixture trees.** Each kernel-version-specific quirk that informs a parser branch has a fixture; a 2030 kernel change that flips a field reorders becomes a CI failure, not a silent data corruption.
- **Provenance citations** make upstream bug-fix backports tractable: a bug in `cpu.Collector` searches `git log -- src/linux/btop_collect.cpp` for the matching change.
- **Six independently shippable milestones** mean we ship CPU+memory+sensors in week 1 and GPU is gated on need, not on critical path.

### Negative

- **Parallel implementation to upstream btop.** Bug fixes do not flow automatically; the provenance comments and drift fixtures (SD11, SD12) are the mitigation, not a fix.
- **Vendor SDK bindings via `purego`** are a new dependency surface for this repo. Mature, but new. Worst case we drop to subprocess `nvidia-smi --query-gpu` parsing (slow, lossy nanosecond timing) as a fallback.
- **`testdata/{proc,sys}/` fixtures bloat the repo.** Each fixture is small (kB) but they accumulate; a discipline of "one fixture per *upstream branch*, not per kernel version" keeps growth bounded.
- **No sample history out of the box** means consumers reach for ring buffers themselves. Acceptable per SD10; a followup `observability/sysmetrics/history/` could land if N call sites repeat the pattern.

### Neutral

- **`Bundle` is one orchestrator among possible many** (SD13). Callers are free to wire collectors against `errgroup`, `pool.Pool`, or anything else. The Bundle is a convenience.
- **Tags-file gating** (SD5) means users opting into NVIDIA recompile the entire repo. Acceptable per the existing `./tags` discipline (CLAUDE.md "Build tags from tags file").

### Derived practices

- **New procfs / sysfs parsers route through `internal/procfs` + `internal/sysfs`** so their fixture-test discipline applies uniformly. Direct `os.Open("/proc/...")` calls outside those packages are a code-review red flag.
- **GPU-vendor subpackages return empty slices on absence**, never errors. Absent hardware is not an error condition in a metrics collector.
- **Test fixtures land alongside the package** in `testdata/`; CHANGELOG entries note new fixtures so reviewers can spot fixture drift.

## Open questions

Tracked as named follow-ons; resolved at the milestone where they bind.

1. **Process tree helpers** — do we ship a `proc.Tree(snapshot) iter.Seq[TreeNode]` helper, or punt to consumers? Decided in M3.
2. **Network-interface filtering defaults** — loopback, virtual bridges (docker0, br-*), wg-*. btop hides these in the default UI but exposes them via a flag. Our default: emit all; provide an `Filter` option function. Decided in M2.
3. **Container detection scope** — should `container.Detect()` also return cgroup limits (CPU quota, memory.max) so callers can scale percentages against the container's effective resources, not the host's? Decided in M4.
4. **Sample timestamp source** — `time.Now()` vs `time.Now().UnixNano()` vs the kernel's `CLOCK_MONOTONIC`. btop uses wall-clock; for delta computations `CLOCK_MONOTONIC` is safer. Decided in M1, codified in `internal/deltaclock`.
5. **Streaming sink contract** — should snapshots implement `streamreadaccess.SinkI` (the ADR-0018 sink protocol) so they flow into the leeway-card pipeline directly? Plausible; defer until a real consumer asks. Out of scope of this ADR.

## Status

Accepted 2026-05-03 by @spx. Implementation tracked across M1–M6 above.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-30 — PSI collector (`psi`)

Adds a Pressure Stall Information collector under [`../../public/observability/sysmetrics/psi/`](../../public/observability/sysmetrics/psi/) reading `/proc/pressure/{cpu,memory,io}` — the share of wall-time tasks spent stalled on each resource over 10/60/300 s windows (`some` / `full`). This is *beyond* btop's data set (btop predates PSI): it is the kernel's saturation signal, distinguishing a resource that is merely busy from one that is contended. A standard `Sample()`-based collector wired into the `Bundle` (`DomainPSI`); a kernel without `CONFIG_PSI` (or booted `psi=0`) yields `Available=false` rather than an error. Consumed by the imztop Pressure tab ([ADR-0020](./0020-imzero2-imztop-resource-monitor.md), Update of the same date). `status` / `reviewed-date` unchanged.

### 2026-05-29 — static CPU topology reader (`ReadTopology`)

The Public API sketch above covers only **sampling** collectors: each `*.Collector` is primed once and then `Sample(ctx)`-ed on a cadence to produce rate/snapshot deltas. CPU *topology* — the package/cache/core/thread containment tree that `lstopo`(1) draws — is structural, not temporal: it is read once and never deltas. Rather than bend the `Sample()` contract around a value that has no second reading, this adds a one-shot reader alongside the CPU collector:

```go
// public/observability/sysmetrics/cpu/topology.go
func ReadTopology(opts TopologyOptions) (Topology, error)
```

- **Source is pure sysfs.** No CPUID, no `/proc`, no hwloc/cgo. The reader walks `/sys/devices/system/cpu/online`, then per logical CPU reads `topology/{physical_package_id,die_id,cluster_id,core_id,thread_siblings_list}` and `cache/index*/{level,type,size,shared_cpu_list}` and `cpufreq/{scaling_{min,max}_freq,scaling_governor,scaling_driver}` (the PU's `FreqPolicy`), plus `/sys/devices/system/node/node*/{cpulist,meminfo}` for NUMA grouping and per-node RAM (`MemTotal` → `NUMANode.MemBytes`). (Model name is still `/proc/cpuinfo`, unchanged.) `lstopo`/hwloc reads these same files; the parts it adds via CPUID/ACPI/PCI are out of scope below.
- **Reuses existing machinery.** `TopologyOptions` carries an injectable `Sys *sysfs.Reader` exactly like `Options.Sys`, so the SD12 fixture-tree drift guards apply unchanged. The `*_list` / `shared_cpu_list` fields are parsed by the existing `parseCPUSet` helper (ranges + comma lists).
- **Cache levels become nodes** by grouping the logical CPUs that share each `index*`; a split last-level cache (e.g. per-CCX L3) therefore materialises as sibling cache nodes automatically.
- **Scope.** Packages, dies/clusters, L1/L2/L3 caches, cores, SMT threads (PUs), NUMA-node grouping, per-node memory size (`MemTotal`), and per-PU cpufreq policy (governor/driver/min/max). Live per-node usage, PCI/GPU/NIC locality, and physical DIMM inventory (DMI/EDAC) — the rest of full hwloc parity — are deliberately deferred.
- **Consumer.** [ADR-0020](./0020-imzero2-imztop-resource-monitor.md) (Update of the same date) renders this in the imztop `Topology` panel. Topology stays out of the `Bundle`/`Sampler` path, so it adds zero per-tick cost.

`status` and `reviewed-date` are deliberately not re-stamped: the sampling-collector decision is unchanged; this extends the surface with an orthogonal static read.

## References

- [`../../../contrib/btop/LICENSE`](../../../contrib/btop/LICENSE) — Apache 2.0; structural mirroring with attribution is unencumbered.
- [`../../../contrib/btop/src/linux/btop_collect.cpp`](../../../contrib/btop/src/linux/btop_collect.cpp) — Linux collector implementations; primary provenance source.
- [`../../../contrib/btop/src/btop_shared.hpp`](../../../contrib/btop/src/btop_shared.hpp) — upstream Snapshot struct shapes.
- `../../CLAUDE.md` — repo conventions, build-tag handling.
- [`../../tags`](../../tags) — build-tag listing; new GPU tags appended here.
- [ADR-0005](0005-streaming-persisted-kafka-from-connect.md) — possible downstream sink for snapshots (out of scope here).
- `golang.org/x/sys/unix` — `PerfEventOpen` for Intel GPU PMU access (M5).
- `github.com/ebitengine/purego` — cgo-free vendor SDK loader for NVML / ROCm-SMI (M6).
- `github.com/prometheus/procfs` — surveyed and rejected as a foundation; see Alternatives.
- `github.com/shirou/gopsutil` — surveyed and rejected as a foundation; see Alternatives.
