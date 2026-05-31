---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to display an image with the Image widget

Show an RGBA8 bitmap (embedded asset or Go-generated) inside an imzero2
panel, with click and pixel-space hover read-back. Sister widget to the
ring-buffer `scrollingTexture` (see ADR-0009) — same texture-cache
plumbing, but a single static texture per widget id and Go controls when
to re-upload via a content-version counter.

## When to use this recipe

Reach for `Image` when:

- you have raw RGBA8 pixels (decoded by Go-side code or generated procedurally) and want to render them as a bitmap;
- you need click and/or hover events on the image rect, with hover position reported in **image-pixel space** (i.e. independent of the on-screen size);
- the data is *one frame at a time*, not a streaming time series — for the latter use `scrollingTexture`.

If the image source is a file path or a URI you want egui's loader system
to fetch, this recipe is the wrong choice — that path is intentionally
not implemented in this repo (no SVG/format-decoder dependency on the
Rust side; Go decodes everything).

## Prerequisites

- pixels packed as `[]uint32`, one element per pixel, byte order
  `0xRRGGBBAA` (matches `Color32::from_rgba_unmultiplied` on the Rust side
  and `colormap`-style RGBA-as-u32 elsewhere in this repo);
- a stable widget id (any `c.WidgetIdCreatorI`, e.g. `ids.PrepareStr("my-image")`);
- a Go-controlled `contentVersion uint64` per logical asset — bump it
  whenever the bytes change, leave it constant for immutable assets.

## Steps

1. **Decode or build the pixel buffer once.** For embedded assets,
   decode the PNG/JPEG via Go's stdlib and stash the resulting
   `[]uint32`. For dynamic bitmaps, fill the slice in place and bump
   `contentVersion` on each change. Note: pass `[]uint32{}` (empty,
   **not** `nil`) for the placeholder case — the FFFI2 nil-sentinel maps
   `nil` slices to `0xFFFFFFFF` length, which the Rust reader treats as
   four billion bytes. See the project memory entry "FFFI2 nil slice
   sentinel is asymmetric".

2. **Skip re-shipping pixels when nothing changed.** Hold an
   `ImageVersionTracker[K]` next to the asset and ask it which slice to
   pass per frame — the tracker returns the empty slice when the
   supplied `contentVersion` matches the last one it saw, so the FFI
   carries only the scalar header.

   **Keying:** the tracker key must be 1:1 with the **widget id**, not
   with the logical asset — the Rust GPU cache is keyed by widget id,
   so two widgets showing the same asset have two independent cache
   entries that each need a first-frame upload. The simplest safe
   pattern is to pass the same string to both the tracker and
   `ids.PrepareStr(...)`. If you reuse one tracker key across multiple
   widget ids, every widget after the first will render blank on its
   first frame (empty pixels + cold cache).

   For static assets shown a small fixed number of times, skip the
   tracker entirely — the per-widget-id one-shot upload cost is
   negligible. The tracker pays off for *dynamic* content where the
   alternative is shipping the full buffer every frame.

   ```go
   var tracker = c.NewImageVersionTracker[string]()

   func renderMyImage() {
       pixels := tracker.PixelsToSend("my-asset", currentVersion, fullPixels)
       c.Image(
           ids.PrepareStr("my-asset"),
           widthPx, heightPx,
           currentVersion,
           uint8(c.FitNativeE),
           0, 0,                // fixedW/fixedH unused by FitNative
           uint8(c.FilterNearestE),
           c.TintNoneRgba,
           pixels,
       ).SendResp()
   }
   ```

   The Rust-side cache uploads once per `(id, contentVersion, w, h)`
   tuple; subsequent frames with the same key reuse the cached
   `TextureHandle` regardless of whether Go ships pixels.

3. **Pick a fit mode.** All four are wire-level switches at the IDL — no
   spec change to swap:

   - `FitNativeE` — render at exact texture pixels (`fixedW`/`fixedH`
     ignored). Use for icons or pixel-art where any scaling would alias.
   - `FitFixedE` — render at exactly `(fixedW, fixedH)` screen pixels,
     possibly distorting the aspect.
   - `FitFillRectE` — render filling `ui.available_size()`.
   - `FitAspectMaxE` — aspect-preserved, scaled to fit inside
     `(fixedW, fixedH)`. Most common for photo-like content.

4. **Read back hover and clicks.** `SendResp()` returns the standard
   `ResponseFlagsE`, so `HasPrimaryClicked()`, `HasDoubleClicked()`,
   `HasSecondaryClicked()`, `HasHovered()` all work the way they do for
   `Button`. For pixel-space hover position, use `SendRespHoverPx`:

   ```go
   var hoverRc uint64
   flags := c.Image(...).SendRespHoverPx(&hoverRc)
   row, col, hovered := c.UnpackHoverRc(hoverRc)
   if hovered && flags.HasPrimaryClicked() {
       fmt.Printf("clicked at row=%d col=%d\n", row, col)
   }
   ```

   `(row, col)` index the source texture, **not** the screen rect — so
   the caller doesn't have to invert the fit-mode math. Sentinel
   `u64::MAX` (`UnpackHoverRc` reports `hovered=false`) for "outside the
   widget rect".

5. **Drop the cache entry on demand (optional).** The cache reaps any
   entry not touched for ~10 s at 60 Hz. If the caller knows an asset is
   gone for good (e.g. a closed tab), force-evict and forget the
   tracker's version record:

   ```go
   c.ImageRelease(ids.PrepareStr("my-asset")).SendResp()
   tracker.Forget("my-asset")
   ```

## Verification

- The "image" demo (`Name: "image"` in
  `egui2_hl_registrations.go`'s registry) renders an embedded checker in
  all four fit modes plus a per-frame gradient with hover read-back; run
  `rust/imzero2/hmi.sh` and pick it from the demo list.
- Set `IMZERO2_SCREENSHOT_DIR` to capture a per-window PNG and compare
  against the expected (4-frame tour is enough — there's no open
  animation to wait out).

## Troubleshooting

- **Symptom:** Rust logs `image: pixels length mismatch; skipping upload`
  and the widget renders the previous frame (or nothing).
  **Cause:** `len(pixels) != widthPx*heightPx` and the version key
  forced a cache miss.
  **Fix:** Either fix the buffer length, or — if you meant to draw the
  cached texture — let `ImageVersionTracker` decide and pass the
  unchanged `contentVersion` so it ships the empty slice instead of a
  partial buffer.

- **Symptom:** every frame re-uploads even though the image hasn't
  changed.
  **Cause:** `contentVersion` is being bumped unconditionally, or the
  tracker isn't being reused across frames.
  **Fix:** Bump `contentVersion` only on actual content change, and hold
  the tracker at package or app scope so it spans frames.

- **Symptom:** `nil`-pixel call panics or hangs Rust on read.
  **Cause:** Passed `nil` instead of `[]uint32{}` — the FFFI2
  encoder maps `nil` to the `0xFFFFFFFF` length sentinel, which the
  Rust reader treats as a literal length.
  **Fix:** Always pass `[]uint32{}` for "no pixels this frame" (the
  `ImageVersionTracker` already does this for you).
