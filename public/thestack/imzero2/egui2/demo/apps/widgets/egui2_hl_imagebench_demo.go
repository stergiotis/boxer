package widgets

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// The measurement harness of the image upload-cost trial
// (doc/trials/image-upload-cost): one 512 × 512 canvas, an RGBA8 image of a
// chosen side drawn into it through paintImage, and an arm that says how its
// pixels reach the host — every frame as one texture, every frame as a grid of
// tiles, once and then from the host's cache, or not at all. A window of
// frames is summarised into labels and one log line; flowbench's shape, for
// texture uploads.
//
// What it can and cannot see. The Go side of the image is timed around the
// paintImage calls (emit), which includes the write of the pixel slice into
// the pipe and so waits on the host reading it. Render, sync, total, interpret
// and the bytes written are the whole frame's (gallery chrome included), and
// describe the frame before the one that records them. Period is the wall
// time between two entries of this function: with a repaint requested every
// frame it is the whole cycle — Go, the pipe, the host's dispatch, its texture
// upload, its raster and whatever the carrier does with the frame — floored by
// the host's frame cadence. The host's raster statistics are its own
// (IMZERO2_HEADLESS_RASTER_STATS).
//
// The canvas is the same size in every cell, so what the host rasterises is
// the same area whatever the image's side; only the upload differs.
const imageBenchCanvas = 512

var imageBenchArms = []string{"reupload", "tiles512", "tiles256", "resident", "off", "still"}

// imageBenchSides are the image sides; the pixel payload is 4·side² bytes:
// 1, 4, 16, 64 and 256 MiB.
var imageBenchSides = []int{512, 1024, 2048, 4096, 8192}

// imageBenchPlan is the warm-up and the measured window per side. The windows
// shrink with the side because a frame of the largest cell is expected to
// take on the order of a second.
var imageBenchPlan = []struct{ warmup, window int }{
	{30, 120}, {30, 120}, {15, 60}, {6, 30}, {3, 12},
}

// imageBenchFlipDelay is how many frames after "flip" the still arm swaps its
// pattern: long enough for a driver to capture the frame before it.
const imageBenchFlipDelay = 150

type imageBenchState struct {
	// side is -1 until the trial's script asks: a launch's raster statistics
	// are cumulative, and frames nobody asked for would be in them.
	arm, side               int
	appliedArm, appliedSide int

	// bufs are two patterns of side² pixels; an upload alternates between
	// them, so consecutive versions differ in content as well as in number.
	bufs    [2][]uint32
	frames  int
	version uint64
	sent    bool
	flipAt  int
	flipped bool
	// nudged moves the still arm's image by one point: a change of the mesh
	// over the canvas, for telling a texture the host never received from one
	// it holds and did not redraw.
	nudged bool

	lastEntry                                              time.Time
	periodNs, emitNs, renderNs, syncNs, totalNs, interpret []int64
	written                                                []int64
	firstTotalNs, firstInterpretNs, firstPeriodNs          int64
	reported                                               bool
	summary                                                [4]string
}

func newImageBenchState() *imageBenchState {
	return &imageBenchState{side: -1, appliedSide: -2}
}

// imageBenchFill writes one of the two patterns: a horizontal ramp in red (0)
// or a vertical ramp in blue (1), opaque, 0xRRGGBBAA.
func imageBenchFill(dst []uint32, side int, pattern int) {
	for y := 0; y < side; y++ {
		row := dst[y*side : (y+1)*side]
		if pattern == 0 {
			for x := range row {
				row[x] = uint32(x&0xff)<<24 | 0x00402000 | 0xff
			}
		} else {
			b := uint32(y&0xff)<<8 | 0x20400000 | 0xff
			for x := range row {
				row[x] = b
			}
		}
	}
}

// imageBenchTiles is the tile side of an arm, or 0 for an arm that ships one
// texture.
func imageBenchTiles(arm int) int {
	switch imageBenchArms[arm] {
	case "tiles512":
		return 512
	case "tiles256":
		return 256
	}
	return 0
}

func demoImageBench(ids *c.WidgetIdStack, st *imageBenchState) {
	now := time.Now()
	var periodNs int64
	if !st.lastEntry.IsZero() {
		periodNs = now.Sub(st.lastEntry).Nanoseconds()
	}
	st.lastEntry = now

	if st.side != st.appliedSide || st.arm != st.appliedArm {
		st.appliedSide, st.appliedArm = st.side, st.arm
		st.frames, st.reported, st.sent, st.flipAt, st.flipped, st.nudged = 0, false, false, 0, false, false
		st.version = 0
		st.periodNs, st.emitNs, st.renderNs = st.periodNs[:0], st.emitNs[:0], st.renderNs[:0]
		st.syncNs, st.totalNs, st.interpret, st.written = st.syncNs[:0], st.totalNs[:0], st.interpret[:0], st.written[:0]
		st.firstTotalNs, st.firstInterpretNs, st.firstPeriodNs = 0, 0, 0
		st.summary = [4]string{}
		st.bufs = [2][]uint32{}
		if st.side >= 0 && imageBenchArms[st.arm] != "off" {
			side := imageBenchSides[st.side]
			for i := range st.bufs {
				st.bufs[i] = make([]uint32, side*side)
				imageBenchFill(st.bufs[i], side, i)
			}
		}
	}
	armName := imageBenchArms[st.arm]
	side, sideName := 0, "none"
	if st.side >= 0 {
		side = imageBenchSides[st.side]
		sideName = fmt.Sprintf("s%d", side)
	}
	still := armName == "still"

	switch {
	case still:
		// Nothing in this arm's chrome may change between the click on "flip"
		// and the swap: the swap is the only difference between two frames.
		c.Label("Still arm: one upload, and a second one a fixed number of frames after flip").Send()
	case st.summary[0] != "":
		for _, s := range st.summary {
			c.Label(s).Send()
		}
	default:
		plan := imageBenchPlan[max(st.side, 0)]
		c.Label(fmt.Sprintf("bench running arm=%s side=%s measured=%d of %d (after %d warm-up frames)",
			armName, sideName, len(st.periodNs), plan.window, plan.warmup)).Send()
	}
	// The controls come first: the gallery's window shows the top of a demo,
	// and a driver cannot click a button it cannot see.
	for range c.HorizontalTop().KeepIter() {
		for i, s := range imageBenchSides {
			if c.Button(ids.PrepareSeq(uint64(0xf900+i)), c.Atoms().Text(fmt.Sprintf("s%d", s)).Keep()).Selected(st.side == i).SendResp().HasPrimaryClicked() {
				st.side = i
			}
		}
	}
	for range c.HorizontalTop().KeepIter() {
		for i, name := range imageBenchArms {
			if c.Button(ids.PrepareSeq(uint64(0xfa00+i)), c.Atoms().Text(name).Keep()).Selected(st.arm == i).SendResp().HasPrimaryClicked() {
				st.arm = i
			}
		}
		if c.Button(ids.PrepareSeq(0xfaff), c.Atoms().Text("flip").Keep()).SendResp().HasPrimaryClicked() && still && st.flipAt == 0 {
			st.flipAt = st.frames + imageBenchFlipDelay
		}
		if c.Button(ids.PrepareSeq(0xfafe), c.Atoms().Text("nudge").Keep()).SendResp().HasPrimaryClicked() && still {
			st.nudged = true
		}
	}

	// The frame that applies a cell builds its buffers and uploads nothing, so
	// the first upload is a frame of its own.
	var emitNs int64
	if st.side >= 0 && st.frames > 0 && armName != "off" {
		t := time.Now()
		imageBenchEmit(ids, st, side)
		emitNs = time.Since(t).Nanoseconds()
	}
	c.PaintCanvas(ids.PrepareStr("ib-canvas"), imageBenchCanvas, imageBenchCanvas).Send()

	if still {
		// A steady stream of frames whose only possible difference is the
		// texture's content.
		c.RequestRepaintAfter(1.0 / 30)
	} else {
		c.RequestRepaint()
	}
	if st.side < 0 {
		return
	}
	st.frames++
	if still {
		return
	}
	plan := imageBenchPlan[st.side]
	// The frame metrics describe the frame before this one, and so does the
	// period. Frame 1 is the first upload; frame 2 reads its cost.
	if st.frames == 3 {
		st.firstTotalNs, st.firstInterpretNs, st.firstPeriodNs = metrics.Current.LastTotalNs, metrics.Current.LastInterpretNs, periodNs
	}
	if st.frames > plan.warmup+2 && len(st.periodNs) < plan.window {
		st.periodNs = append(st.periodNs, periodNs)
		st.emitNs = append(st.emitNs, emitNs)
		st.renderNs = append(st.renderNs, metrics.Current.LastRenderNs)
		st.syncNs = append(st.syncNs, metrics.Current.LastSyncNs)
		st.totalNs = append(st.totalNs, metrics.Current.LastTotalNs)
		st.interpret = append(st.interpret, metrics.Current.LastInterpretNs)
		st.written = append(st.written, metrics.Current.LastWritten)
	}
	if len(st.periodNs) == plan.window && !st.reported {
		st.reported = true
		tiles := 1
		if ts := imageBenchTiles(st.arm); ts > 0 {
			tiles = (side / ts) * (side / ts)
		}
		if armName == "off" {
			tiles = 0
		}
		per50, per95 := flowBenchQuantiles(st.periodNs)
		em50, em95 := flowBenchQuantiles(st.emitNs)
		re50, _ := flowBenchQuantiles(st.renderNs)
		sy50, _ := flowBenchQuantiles(st.syncNs)
		to50, to95 := flowBenchQuantiles(st.totalNs)
		in50, in95 := flowBenchQuantiles(st.interpret)
		wr50, _ := flowBenchQuantiles(st.written)
		st.summary[0] = fmt.Sprintf("bench done arm=%s side=%s tiles=%d payload_bytes=%d frames=%d", armName, sideName, tiles, 4*side*side, plan.window)
		st.summary[1] = fmt.Sprintf("bench go emit_p50_us=%d emit_p95_us=%d render_p50_us=%d sync_p50_us=%d total_p50_us=%d total_p95_us=%d",
			em50/1000, em95/1000, re50/1000, sy50/1000, to50/1000, to95/1000)
		st.summary[2] = fmt.Sprintf("bench host interpret_p50_us=%d interpret_p95_us=%d written_p50_bytes=%d", in50/1000, in95/1000, wr50)
		st.summary[3] = fmt.Sprintf("bench cycle period_p50_us=%d period_p95_us=%d first_total_us=%d first_interpret_us=%d first_period_us=%d",
			per50/1000, per95/1000, st.firstTotalNs/1000, st.firstInterpretNs/1000, st.firstPeriodNs/1000)
		log.Info().Str("arm", armName).Str("side", sideName).Int("tiles", tiles).Int("frames", plan.window).
			Int64("emitP50Us", em50/1000).Int64("totalP50Us", to50/1000).
			Int64("interpretP50Us", in50/1000).Int64("periodP50Us", per50/1000).
			Int64("writtenP50Bytes", wr50).
			Msg("imagebench window complete")
	}
	c.Label("Emit is the Go side of the image alone, pipe write included; render, sync, total, interpret and bytes are the " +
		"whole frame's and describe the frame before; period is the wall time of a whole cycle, floored by the host's cadence. " +
		"Compare against arm=off and arm=resident.").Wrap().Send()
}

// imageBenchEmit issues the frame's paintImage commands for the applied cell.
func imageBenchEmit(ids *c.WidgetIdStack, st *imageBenchState, side int) {
	armName := imageBenchArms[st.arm]
	// Texture ids are per cell, so a cell never inherits another's texture.
	base := uint64(0x1b000000) | uint64(st.arm)<<20 | uint64(st.side)<<16
	switch armName {
	case "reupload":
		st.version++
		id := ids.PrepareSeq(base).Derive()
		c.PaintImage(id, 0, 0, imageBenchCanvas, imageBenchCanvas, uint32(side), uint32(side), st.version, st.bufs[st.version&1]).Send()
	case "tiles512", "tiles256":
		st.version++
		ts := imageBenchTiles(st.arm)
		g := side / ts
		buf := st.bufs[st.version&1]
		step := float32(imageBenchCanvas) / float32(g)
		for ty := 0; ty < g; ty++ {
			for tx := 0; tx < g; tx++ {
				i := ty*g + tx
				// A tile is a contiguous run of the cell's buffer: the same
				// source memory as the single texture, in tile-sized pieces.
				id := ids.PrepareSeq(base + 1 + uint64(i)).Derive()
				c.PaintImage(id, float32(tx)*step, float32(ty)*step, float32(tx+1)*step, float32(ty+1)*step,
					uint32(ts), uint32(ts), st.version, buf[i*ts*ts:(i+1)*ts*ts]).Send()
			}
		}
	case "resident", "still":
		id := ids.PrepareSeq(base).Derive()
		version := uint64(1)
		if armName == "still" && st.flipAt > 0 && st.frames >= st.flipAt {
			version = 2
			if !st.flipped {
				st.flipped, st.sent = true, false
			}
		}
		pixels := imageBenchEmptyPixels
		if !st.sent || c.CurrentApplicationState.StateManager.TextureStarved(id) {
			pixels = st.bufs[version-1]
			st.sent = true
		}
		var dx float32
		if st.nudged {
			dx = 1
		}
		c.PaintImage(id, dx, 0, imageBenchCanvas+dx, imageBenchCanvas, uint32(side), uint32(side), version, pixels).Send()
	}
}

// imageBenchEmptyPixels is the non-nil empty slice the ship-once protocol
// sends for an unchanged version.
var imageBenchEmptyPixels = []uint32{}

func init() {
	registry.Register(registry.Demo{
		Name: "imagebench", Category: "Graphics & canvas", Title: icons.IconFileImage + " image bench (upload-cost trial harness)",
		Stage:       [2]float32{1024, 760},
		Flags:       registry.DemoFlagNeedsLargeArea | registry.DemoFlagSkipInTour | registry.DemoFlagNonDeterministic,
		Kind:        registry.DemoKindDX,
		Description: "The measurement harness of the image upload-cost trial (doc/trials/image-upload-cost): one canvas, an RGBA8 image of a chosen side, and an arm — re-uploaded every frame as one texture, as 512 px or 256 px tiles, resident in the host's cache, or absent — and a window of frames summarised into labels and a log line. Not a showcase; the trial's run script drives it.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newImageBenchState()
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoImageBench(ids, state.(*imageBenchState))
		},
	})
}
