---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-06-17
---

# How to enable AMD hardware video encoding (VA-API) on Fedora

imzero2 pixel streaming ([ADR-0024](../adr/0024-imzero2-remote-access-browser-viewer.md))
can hand H.264/HEVC/AV1 encoding to the GPU's VCN block
([ADR-0088](../adr/0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)) instead of software
`libopenh264`. On a stock Fedora install the GPU and kernel driver are already
fine — the gap is purely userspace: **Fedora ships Mesa with the
patent-encumbered codecs (H.264, HEVC) stripped**, so only royalty-free AV1
encode is enabled. RPM Fusion's full-codec Mesa driver closes the gap.

Verified on AMD Strix Halo (Radeon 8060S, VCN 4.0.5) / Fedora 44 / Mesa 26.

## When you need this

Symptom: software-only encode even though the box has an AMD GPU. Confirm it's
the codec gap and **not** missing hardware:

- The GPU encode block is live — `dmesg | grep -i vcn` shows
  `Found VCN firmware Version ENC:` and `ring vcn_unified_*` up. If so, the
  hardware and kernel driver are not the problem.
- VA-API only exposes AV1 encode — an H.264 encode fails with
  `No usable encoding profile found` (see Verification for the probe), or
- the full-codec driver isn't installed — `/usr/lib64/dri-freeworld/` is absent.

## Prerequisites

- An AMD GPU with a working `amdgpu` driver and a render node at
  `/dev/dri/renderD128`.
- A box where RPM Fusion is permitted (the freeworld codecs are kept out of
  Fedora proper for patent reasons).

## Steps

```bash
# 1. Enable RPM Fusion free + nonfree ($(rpm -E %fedora) fills in the release)
sudo dnf install \
  https://mirrors.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm \
  https://mirrors.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-$(rpm -E %fedora).noarch.rpm

# 2. Full-codec Mesa VA driver + the vainfo diagnostic tool
sudo dnf install mesa-va-drivers-freeworld libva-utils

# 3. (optional) full ffmpeg in place of the codec-limited ffmpeg-free
sudo dnf swap ffmpeg-free ffmpeg --allowerasing
```

`mesa-va-drivers-freeworld` installs into `/usr/lib64/dri-freeworld/`, which
libva already searches **before** the stock `/usr/lib64/dri/` — so it takes
effect with no config change and no reboot.

## Verification

1. Entrypoint table (authoritative) — H.264/HEVC/AV1 should each appear with
   `VAEntrypointEncSlice`:

   ```bash
   vainfo --display drm --device /dev/dri/renderD128 2>&1 | grep EncSlice
   #   VAProfileH264Main    : VAEntrypointEncSlice   (+ ConstrainedBaseline, High)
   #   VAProfileHEVCMain    : VAEntrypointEncSlice   (+ Main10)
   #   VAProfileAV1Profile0 : VAEntrypointEncSlice
   ```

2. Confirm libva loads the **freeworld** driver, not stock:

   ```bash
   ffmpeg -hide_banner -v verbose -init_hw_device vaapi=va:/dev/dri/renderD128 \
     -f lavfi -i nullsrc=s=64x64 -frames:v 0 -f null - 2>&1 | grep 'Trying to open'
   # ... stops at /usr/lib64/dri-freeworld/radeonsi_drv_video.so
   #     (must NOT fall through to /usr/lib64/dri/)
   ```

3. End-to-end — a real hardware encode to `/dev/null` per codec (no output and
   exit 0 = success):

   ```bash
   for enc in h264_vaapi hevc_vaapi av1_vaapi; do
     ffmpeg -hide_banner -v error \
       -init_hw_device vaapi=va:/dev/dri/renderD128 -filter_hw_device va \
       -f lavfi -i testsrc=size=640x480:rate=30:duration=1 -vf 'format=nv12,hwupload' \
       -c:v "$enc" -f null - && echo "$enc OK"
   done
   ```

## Notes and limits

- **VP9 and MJPEG encode stay unavailable, and this is not fixable in software.**
  AMD's VCN block decodes VP9 but does not encode it, and exposes only JPEG
  *decode* — those `vainfo` entrypoints are genuinely absent in the hardware.
  Use H.264/HEVC/AV1.
- **Patent-clean alternative.** If a box must avoid RPM Fusion (e.g. a
  showcase/deploy host with a strict provenance posture), keep the software
  `libopenh264` path, or prefer **hardware AV1** — it is royalty-free and works
  on stock Fedora Mesa with no extra packages.
- **Consumer wiring.** imzero2's encoder selection (ADR-0088) is where a box
  opts into the hardware path once the steps above verify on it.
