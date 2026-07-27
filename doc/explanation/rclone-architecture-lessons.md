---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-27
---

> **Provenance.** Compiled 2026-07-27 from rclone's public documentation and
> one upstream design post (see §8 Sources). No rclone source code was read;
> every statement about internal mechanism is a reconstruction from documented
> behavior. rclone is MIT-licensed, so this clean-room posture is not an IP
> firewall — it is a disclosure of how far the claims below were verified.

# Lessons from rclone's architecture

[rclone](https://rclone.org/) presents ~80 storage systems — object stores,
consumer drives, WebDAV endpoints, local disks — behind one command surface,
and re-exports them over nine wire protocols. The interesting part is not the
backend count but the shape that lets the count grow without the interface
growing with it. This note records the transferable properties, and the places
they bear on this repository.

## 1. The multiplication is the product

One internal filesystem interface sits between *N* backends and *M* consumers.
Backends are written once and reach every consumer; consumers are written once
and reach every backend. Neither side coordinates with the other.

The transferable property is the position of the interface, not its content:
it sits at the narrow waist of the domain — "named byte streams in a tree" —
where the smallest vocabulary that still describes everything lives. An
interface one level richer (say, with per-vendor sharing semantics) would have
had to change every time a backend was added.

Corollary worth stating plainly: a narrow waist is only discoverable if the
domain has one. §7 covers what happens when it does not.

## 2. Publish the capability matrix; own the leak

rclone's own framing: *"Each cloud storage system is slightly different.
Rclone attempts to provide a unified interface to them, but some underlying
differences show through."* Rather than hide that, the overview page carries
two per-backend tables — one for intrinsic characteristics (hash algorithms,
ModTime read/write, case-insensitivity, duplicate filenames, MIME type,
metadata) and one for eleven optional operations (`Purge`, `Copy`, `Move`,
`DirMove`, `CleanUp`, `ListR`, `StreamUpload`, `MultithreadUpload`,
`LinkSharing`, `About`, `EmptyDir`).

A unifying interface over heterogeneous substrates has exactly two honest
designs. **Lowest common denominator** — the interface shrinks with every
backend added, and capability that exists in the substrate becomes
unreachable. **Full surface plus a declared capability set** — the interface
stays wide, and callers branch on a declaration rather than on a vendor name.
Only the second scales with backend count.

The cost is real and should be named: the declaration is a second thing to
keep true, and a stale capability bit is worse than no bit at all, because
callers trust it.

This is the same shape as the pass-property declarations in
[`github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass`](../../public/db/clickhouse/dsl/nanopass),
where a pass states what it requires and preserves instead of the pipeline
inferring it, and it is what makes a registry such as
[ADR-0108](../adr/0108-keelson-sql-pass-registry.md) able to compose passes it
was not written against.

## 3. Name the degradation ladder

Presenting an object store as a POSIX filesystem cannot be done faithfully:
objects "can't [be extended] or [written] to the middle of". rclone does not
resolve this with a default; it exposes an ordered ladder of four named modes
— `off`, `minimal`, `writes`, `full` — and documents each one by its
**non-guarantees**: `off` cannot open a file for simultaneous read and write,
cannot seek while writing, and cannot retry a failed upload; `minimal` buffers
read-write opens; `writes` supports normal filesystem operations and retries
uploads with exponential backoff; `full` buffers everything and tracks
partially-downloaded regions as sparse files.

The property: when an emulation is *necessarily* partial, the compromise
belongs in the interface as a small ordered set of named modes, each labelled
with what it stops guaranteeing. A boolean hides the ladder; a silent default
picks a point on it for the caller and makes the failure look like a bug.

## 4. Offer more than one coupling level

Four integration surfaces, at four different distances:

| Surface | Boundary | Consumer pays |
|---|---|---|
| CLI (`rclone sync`) | process, argv | argument construction, exit-code parsing |
| `rclone serve X` | protocol | nothing — it already speaks X |
| `librclone` (C ABI, Go package, gomobile) | link | ABI and lifecycle management |
| `rclone rcd` | control, HTTP JSON-RPC | a client and a running daemon |

Downstream picks the level. A backup tool that wants an isolated blast radius
takes the process boundary; a Kubernetes CSI driver that must avoid shipping a
binary onto every node links the library; an Android file manager binds the
same library through gomobile.

The property: **the integration surface decides who can adopt you**, and
offering exactly one forces every consumer into the same coupling — usually
the one convenient for the author. This is a design axis, not packaging.
Within this repository the equivalent question is live wherever a boundary is
fixed by construction, e.g. the link boundary in
[`github.com/stergiotis/boxer/public/thestack/fffi2`](../../public/thestack/fffi2).

## 5. Speak the protocol the consumer already knows

`rclone serve` implements nine of them: `dlna`, `docker`, `ftp`, `http`,
`nfs`, `restic`, `s3`, `sftp`, `webdav`. The sharpest case is `serve restic`,
which implements *another project's* private REST API.

The asymmetry is the lesson. Implementing the consumer's protocol costs one
adapter, written once, on your side, and needs no agreement from anyone.
Asking the consumer to implement yours costs a negotiation, a release cycle
you do not control, and an ongoing compatibility obligation for them. Where
adoption matters more than elegance, the adapter is the cheaper instrument —
and it stays cheap only as long as the protocols are semantically close (§7).

## 6. A pipe is a transport

restic does not connect to rclone over a socket. It spawns
`rclone serve restic --stdio` and runs **HTTP/2 over stdin/stdout**. Upstream
states the reasoning: no network connection is needed, so the data is reachable
only by the user running the two processes (and root); no TCP port has to be
agreed on; the parent starts and stops the child; and multiple pairs run
concurrently with no port configuration at all. HTTP/2 is there for stream
muxing over the single pipe pair.

Two properties generalise:

- **Transport is not the same thing as network.** When both endpoints are
  processes on one host and one of them spawns the other, a mature protocol
  stack — multiplexing, flow control, framing, off-the-shelf client and server
  implementations — can be had over anonymous pipes, with none of the network
  attached to it.
- **Possession of the descriptor is the authorization.** The pipe is inherited,
  not addressed, so there is nothing to discover, bind, or firewall. The
  authentication layer is not implemented cheaply; it is *absent*, because the
  question it answers cannot be asked. Compare a localhost socket, which is
  addressable by any process on the machine and therefore needs one.

The corollary constraint: this works only where the parent-child relationship
is real. It does not survive a peer that outlives its consumer, needs to be
reached by more than one client, or lives on another host.

## 7. What does not transfer

- **The narrow waist is a property of the domain, not a technique.** Storage
  backends differ *operationally* — a hash is missing, a move is not atomic —
  while agreeing on what a file is. Substrates that differ *semantically* have
  no such waist, and forcing one produces an interface whose exceptions
  outnumber its guarantees. The columnar mapping in
  [`github.com/stergiotis/boxer/public/semistructured/leeway`](../../public/semistructured/leeway)
  is closer to that second case than the first.
- **Protocol adapters are cheap only across small semantic gaps.** The nine
  `serve` protocols are all file-shaped, which is why each is an adapter rather
  than a translation. Across a genuine mismatch the same move is a
  reimplementation wearing an adapter's name.
- **A capability matrix is a maintenance liability sized to the backend
  count.** rclone pays it with a large contributor base and per-backend
  integration tests. A small repository can adopt the pattern; it should not
  adopt the backend count that makes the pattern necessary.

## 8. Sources

Public documentation only, retrieved 2026-07-27:

- Overview and per-backend capability tables — https://rclone.org/overview/
- VFS cache modes — https://rclone.org/commands/rclone_mount/
- `rclone serve` subcommands — https://rclone.org/commands/rclone_serve/
- Docker volume plugin (`rclone serve docker`, since v1.56) — https://rclone.org/docker/
- restic's rclone backend, stdio + HTTP/2 rationale (restic project blog,
  2018-04-01) — https://restic.net/blog/2018-04-01/rclone-backend/
- Kopia's rclone provider, documented as experimental — https://kopia.io/docs/repositories/
- Project history, license, named deployments — https://en.wikipedia.org/wiki/Rclone
