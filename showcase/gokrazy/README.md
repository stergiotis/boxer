---
type: how-to
audience: operator building an appliance image
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# gokrazy appliance images for the imzero2 headless host

Two bootable x86-64 appliance images built with
[gokrazy](https://gokrazy.org/), carrying the imzero2 headless host and no GPU
stack at all. This is [ADR-0205](../../doc/adr/0205-imzero2-cpu-rasterized-pixel-host.md)
M6, which inherits the appliance question from
[ADR-0128](../../doc/adr/0128-imzero2-mesh-draw-stream-codec-lane.md) M3.

## Why this is possible now

ADR-0128 M3 could not put pixels on an appliance: every raster path went
through `headless_wgpu`, and so through a Vulkan loader plus an ICD plus Mesa.
The CPU rasterizer added by ADR-0205 changes the closure rather than the
performance story. Measured on the `headless_soft` binary:

```
$ ldd target/headless-soft/release/imzero2
	linux-vdso.so.1
	libgcc_s.so.1 => /lib64/libgcc_s.so.1
	libm.so.6 => /lib64/libm.so.6
	libc.so.6 => /lib64/libc.so.6
	/lib64/ld-linux-x86-64.so.2
```

Four files, about 4.8 MB of glibc. `build.sh` reads that list from the binary
with `ldd` rather than hardcoding it, so a new dependency fails the build
instead of failing the boot.

Static-musl (the other half of ADR-0205 M6) is **not** required for this and is
not done here — see "What this does not cover".

## The two variants

Both run the same `headless_soft` binary. They differ only by whether a static
ffmpeg is in the image, which is the whole point of the pair.

| Instance | ffmpeg | What the carrier serves |
| --- | --- | --- |
| `boxer-soft` | absent | The encoder probe finds nothing to spawn, so `CodecLane::best()` falls back to the ADR-0128 mesh draw-stream lane, which needs no encoder. |
| `boxer-soft-video` | static, software-only | The H.264 lane. A fully static ffmpeg cannot `dlopen` a VAAPI driver, so every hardware lane fails and `libopenh264` is the only candidate — the case `codeclane.rs` already documents. |
| `boxer-soft-play` | static, software-only | The same, plus `clickhouse-local` and the `play` SQL playground. See "The ClickHouse variant" below. |

The no-ffmpeg image demonstrates that fallback rather than asserting it: there
is no configuration anywhere that says "use mesh", only an absent binary.

## The ClickHouse variant

`boxer-soft-play` carries the `clickhouse` binary at
`chlocalpool.DefaultBinaryPath` — boxer invokes it as `clickhouse local` — and
runs `play` instead of the widget gallery.
Two things about it are worth stating plainly, because both were wrong in the
first draft.

**The binary rides the A/B root images, not `/perm`.** 728 MB compresses to
about 261 MB in squashfs, taking the image from 119 MB to 380 MB, which the
500 MB root holds comfortably. The deciding argument is not size, though: an
update then swaps the engine and the app that expects it together, the root is
read-only and verified, and `/perm` is not usable for this at all — gokrazy
leaves it unformatted in a file image, and the boot log says so
(`/perm/random.seed: read-only file system`). Putting an executable there would
mean provisioning a filesystem first and mounting it exec.

**`clickhouse-local` is not what `play` queries.** play talks HTTP to a
ClickHouse *server*; `chlocalbroker` appears in it only as a capability grant
for the timerangepicker's evaluator pool. So the image points play at an
endpoint with `CLICKHOUSE_URL`, set to `http://10.0.2.2:8123/` — QEMU
user-mode networking's alias for the host, so the appliance queries whatever
ClickHouse is already running there. A real deployment sets it to a real
endpoint. `clickhouse-local` stays in the image because the ad-hoc dataset path
([ADR-0134](../../doc/adr/0134-adhoc-datasets.md)) and the `apps/sqlapplet`
books do use it.

Verified end to end: `SELECT * FROM boxer.facts` typed into the appliance
returned **95,826 rows in 591 ms**, and the host's `system.query_log` shows the
guest's `SELECT * FROM "boxer"."facts" FORMAT ArrowStream` plus the leeway UDF
registration it does at startup.

The pool spawns `MinIdle: 2` workers, so this variant wants more than the 1 GiB
`gok vm run` gives a guest. Extra QEMU arguments go after `--`, and QEMU takes
the last `-m`:

```sh
gok -i boxer-soft-play vm run --arch amd64 -- -m 3072
```

## Building

Needs `gok` and, for `--run`, QEMU:

```sh
go install github.com/gokrazy/tools/cmd/gok@latest
sudo dnf install qemu-system-x86         # only for --run
```

The video variant needs a static ffmpeg. The airgap lane already builds one and
this reuses it rather than growing a second:

```sh
./scripts/dev/build-static-ffmpeg.sh     # produces .airgap-ffmpeg-src/ffmpeg-*/ffmpeg
```

Then:

```sh
./showcase/gokrazy/build.sh                       # the two ffmpeg variants
./showcase/gokrazy/build.sh --variant all         # ...and the ClickHouse one
./showcase/gokrazy/build.sh --variant mesh        # just the no-ffmpeg one
./showcase/gokrazy/build.sh --variant video --run # build and boot in QEMU
```

`--run` forwards four ports into the guest: gokrazy's own web interface on
`localhost:8080`, breakglass on `8022`, and the carrier and its viewer page on
`8089` and `8090`. gok's default `-netdev` carries only the first two, so
`build.sh` overrides it — without that the carrier is running but unreachable.
`gok vm run` packs its own ephemeral image rather than booting the `.img`
`build.sh` wrote; that one is the artifact you would put on a disk.

`build.sh` builds the Rust host, stages what the image needs into
`instances/<name>/_stage/`, writes the module context gokrazy builds boxer in
(`instances/<name>/builddir/`), and packs a full disk image. All three are
generated; only `build.sh` and the two `config.json` files are checked in.

## How the Go side is built from this checkout

gokrazy builds Go packages itself, and would otherwise fetch
`github.com/stergiotis/boxer` from its published location rather than using
your working tree. `packer.BuildDir` walks up from `builddir/<import path>`
looking for a `go.mod`, which is gokrazy's documented hook for exactly this;
`build.sh` writes one at the boxer module root of that tree with a `replace`
pointing back at the checkout. The path is relative, so no build host's
filesystem layout ends up in the tree.

The build tags match `rust/imzero2/build_go.sh` — `boxer_enable_profiling` from
`./tags`, plus `binary_log` for the keelson logbridge's CBOR wire format — and
`CGO_ENABLED=0`, which the host is already built with.

## Fonts

gokrazy has no fontconfig, so the host cannot `fc-match` at run time the way
`hmi_headless.sh` does. The fonts ship in the image and their paths are passed
as flags. The CJK fallback is deliberately omitted: ~20 MB for glyphs the
widget demo never draws. A demo that needs them stages a third font in
`build.sh`.

## The bind address is not safe by default

Both images set `IMZERO2_HEADLESS_LISTEN=0.0.0.0:8089`, because a QEMU port
forward cannot reach a guest that binds loopback.

The headless carrier has **no authentication and no TLS** — ADR-0082 is
accepted but unimplemented — and the same WebSocket carries input. Anyone who
can reach that port gets full remote control of the app. `hmi_headless.sh`
refuses a non-loopback bind for this reason, but that gate lives in the shell
script; neither the Go nor the Rust side enforces it, so setting the variable
here bypasses nothing that would otherwise protect you.

This is acceptable **only** because `gok vm run` uses QEMU user-mode
networking, which is host-local NAT and not reachable from the LAN. An image
put on real hardware needs ADR-0082, or an authenticating TLS reverse proxy in
front, before that port is exposed.

## `gok vm run` rebuilds the Go program every time

The Rust host is staged once, into `_stage/`; the Go program is built by
gokrazy from the checkout on **every** `vm run`. In a working tree where the
egui2 IDL is being regenerated, the two halves drift apart between boots, and
the symptom does not name the cause: the carrier accepts viewers and serves
zero frames, with

```
ERROR headless.rs: interpret error during dispatch
error=FFFI error: unable to convert from representation
```

in the program log. Re-run `build.sh` (without `--no-rust-build`) so both
halves come from the same tree.

## What this does not cover

- **musl-static.** The other half of ADR-0205 M6. `cargo check --target
  x86_64-unknown-linux-musl --features headless_soft` fails on exactly two
  build scripts, `ring` and `libmimalloc-sys`, both for lack of
  `x86_64-linux-musl-gcc`. `ring` arrives via `reqwest` ← `walkers`, which
  [ADR-0204](../../doc/adr/0204-leaflet-map-core-port.md) M4 removes outright;
  once it lands, only the allocator is left. Not needed for these images.
- **ClickHouse.** The widget demo is self-contained. An image running a
  ClickHouse-backed app has to answer ADR-0134 SD8's deferred question —
  whether `clickhouse-local` rides the A/B root images or parks under `/perm`.
- **A boot assertion in CI.** These images are built and booted by hand. CI has
  no QEMU lane for them.
