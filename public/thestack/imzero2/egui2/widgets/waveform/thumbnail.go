package waveform

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// ThumbnailInput is one frame of a [RenderThumbnail].
type ThumbnailInput struct {
	// Ids and Key name the canvas; two thumbnails under one id stack need
	// different keys.
	Ids *c.WidgetIdStack
	Key string
	// Overview is the recording, already reduced. A nil or empty one draws
	// the background and a centre line.
	Overview *peaks.Overview
	W, H     float32
	// Progress is the played fraction in [0, 1]: the columns before it take
	// the progress colour and a playhead is drawn at it. Negative draws
	// neither — a recording that is not the one playing.
	Progress float64
	// Visuals overrides [DefaultVisuals]; only Background, Wave, Progress,
	// Playhead and ChannelDivider are read.
	Visuals *Visuals
}

// ThumbnailResult is what the frame's input did.
type ThumbnailResult struct {
	// Clicked reports a click on the thumbnail; Fraction is where, in
	// [0, 1] of the recording — a seek target.
	Clicked  bool
	Fraction float64
}

// thumbScratch is the rect batch's buffers. Widget code runs on the frame
// goroutine only and a batch is serialised by its Send, so one set serves
// every thumbnail of a frame — a page of recordings would otherwise allocate
// five slices per card per frame.
var thumbScratch struct {
	minXs, minYs, maxXs, maxYs []float32
	cols                       color.Colors
}

// RenderThumbnail draws a whole recording w×h at the current position of the
// enclosing Ui: the minimap's drawing (ADR-0208 SD10) without a [Player]
// behind it (ADR-0245 §SD6). One band per channel, one rect batch per band,
// so a thumbnail costs a few opcodes whatever the recording's length. There
// is no view to pan or zoom — a click reports where it landed and nothing
// else, and what a click means is the host's.
func RenderThumbnail(in ThumbnailInput) (res ThumbnailResult) {
	if in.Ids == nil || in.W < 1 || in.H < 1 {
		return
	}
	vis := in.Visuals
	if vis == nil {
		def := DefaultVisuals()
		vis = &def
	}
	canvasH := widgethandle.Make(in.Ids.PrepareStr(in.Key).Derive())
	sm := c.CurrentApplicationState.StateManager
	if sm.GetResponse(canvasH).HasPrimaryClicked() {
		res.Clicked = true
		if cur, live := sm.GetCanvasCursor(canvasH); live && !isNaN32(cur.PosX) {
			res.Fraction = min(max(float64(cur.PosX/in.W), 0), 1)
		}
	}

	ov := in.Overview
	channels := 0
	if ov != nil {
		channels = len(ov.Min)
	}
	n := ov.Columns()
	if n == 0 || channels == 0 {
		c.PaintRectFilled(0, in.H/2-0.5, in.W, in.H/2+0.5, 0, vis.ChannelDivider).Send()
	} else {
		// At most one column per pixel: a wider overview is folded, a
		// narrower one drawn in wider columns.
		fold := max(1, (n+int(in.W)-1)/int(in.W))
		drawn := (n + fold - 1) / fold
		colW := in.W / float32(drawn)
		growThumbScratch(drawn)
		bandH := in.H / float32(channels)
		waveHex, progHex := vis.Wave.Literal(), vis.Progress.Literal()
		played := -1
		if in.Progress >= 0 {
			played = int(in.Progress * float64(drawn))
		}
		for ch := range channels {
			top := float32(ch) * bandH
			yc, hh := top+bandH/2, bandH/2-1
			for i := range drawn {
				lo, hi := int8(127), int8(-127)
				for j := i * fold; j < min((i+1)*fold, n); j++ {
					lo, hi = min(lo, ov.Min[ch][j]), max(hi, ov.Max[ch][j])
				}
				yTop := yc - float32(hi)/127*hh
				yBot := yc - float32(lo)/127*hh
				if yBot-yTop < 1 {
					// A silent column still shows a hairline.
					m := (yTop + yBot) / 2
					yTop, yBot = m-0.5, m+0.5
				}
				s := &thumbScratch
				s.minXs[i], s.minYs[i] = float32(i)*colW, max(yTop, top)
				s.maxXs[i], s.maxYs[i] = float32(i+1)*colW, min(yBot, top+bandH)
				if i < played {
					s.cols.SetHex(i, progHex)
				} else {
					s.cols.SetHex(i, waveHex)
				}
			}
			s := &thumbScratch
			c.PaintRectsFilled(s.minXs[:drawn], s.minYs[:drawn], s.maxXs[:drawn], s.maxYs[:drawn], s.cols[:drawn]).Send()
			if ch > 0 {
				c.PaintLine(0, top, in.W, top, vis.ChannelDivider, styletokens.StrokeHair).Send()
			}
		}
	}
	if in.Progress >= 0 {
		px := float32(min(in.Progress, 1)) * in.W
		c.PaintLine(px, 0, px, in.H, vis.Playhead, styletokens.StrokeRegular).Send()
	}
	c.PaintCanvas(in.Ids.PrepareStr(in.Key), in.W, in.H).
		Background(vis.Background).
		Sense(true, false, false).
		Send()
	return
}

func growThumbScratch(n int) {
	s := &thumbScratch
	if cap(s.minXs) >= n {
		return
	}
	s.minXs, s.minYs = make([]float32, n), make([]float32, n)
	s.maxXs, s.maxYs = make([]float32, n), make([]float32, n)
	s.cols = color.NewColors(n)
}
