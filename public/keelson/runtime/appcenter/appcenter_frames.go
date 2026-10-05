package appcenter

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
)

// secFrames is the frame-time lens (ADR-0261). It has no "Open in play"
// statement: the table is this process's live state, not a trail.
const secFrames = "frames"

// slowFrameUs is where the lens starts warning: an app whose p95 Frame
// takes half a 60 Hz frame leaves the other windows and the client's paint
// the other half. A signpost, not a budget any app is held to.
const slowFrameUs = 8000

// frameSummary folds one app's window rows against the loop row.
type frameSummary struct {
	windows int
	// p50Us sums the windows' medians — what the app costs a frame when
	// all of them draw — and worstP95Us is the slowest window's p95. Each
	// is over that window's own recent frames.
	p50Us      int64
	worstP95Us int64
	mountUs    int64
	messages   float64
	// loopP50Us is the render loop's median period, 0 when unread.
	loopP50Us int64
}

func summarizeFrames(fc *frameCols) (s frameSummary) {
	for j, scope := range fc.Scope {
		if scope == "loop" {
			s.loopP50Us = fc.P50Us[j]
			continue
		}
		if fc.Samples[j] == 0 {
			continue
		}
		s.windows++
		s.worstP95Us = max(s.worstP95Us, fc.P95Us[j])
		s.p50Us += fc.P50Us[j]
		s.mountUs = max(s.mountUs, fc.MountUs[j])
		s.messages += fc.MessagesMean[j]
	}
	return
}

func (inst *App) renderFrames(p *page) {
	fc := &p.frames.cols
	s := summarizeFrames(fc)
	if !inst.lensReady("frames", p.frames.state, p.frames.note, s.windows, "No window of it has drawn in this process.") {
		return
	}
	weak("Wall time of its Frame on the render goroutine, over each window's recent frames. " +
		"Every window draws on that one goroutine in turn, so this time delays all of them; " +
		"the client's paint, in its own process, is not in it.")
	head := fmt.Sprintf("%d window(s): median %s per frame, worst p95 %s, %.0f messages.",
		s.windows, fmtUs(s.p50Us), fmtUs(s.worstP95Us), s.messages)
	if s.loopP50Us > 0 {
		head += fmt.Sprintf(" The render loop's median period is %s; this app's median is %.0f%% of it.",
			fmtUs(s.loopP50Us), 100*float64(s.p50Us)/float64(s.loopP50Us))
	}
	if s.mountUs > 0 {
		head += " Mount took " + fmtUs(s.mountUs) + "."
	}
	c.Label(head).Send()
	if s.worstP95Us >= slowFrameUs {
		badge.New(inst.ids.PrepareStr("frames-slow"), "p95 at or past half a 60 Hz frame").Tone(badge.ToneWarning).Variant(badge.VariantSoft).Send()
	}
	rows := make([]int, 0, len(fc.Scope))
	for j, scope := range fc.Scope {
		if scope != "loop" {
			rows = append(rows, j)
		}
	}
	inst.grid("frames-grid", []string{"window", "frames", "last", "p50", "p95", "max", "messages", "mount"}, len(rows), func(k int) {
		j := rows[k]
		mono(windowOf(fc.InstanceKey[j]))
		mono(fmt.Sprint(fc.Frames[j]))
		mono(fmtUs(fc.LastUs[j]))
		mono(fmtUs(fc.P50Us[j]))
		mono(fmtUs(fc.P95Us[j]))
		mono(fmtUs(fc.MaxUs[j]))
		mono(fmt.Sprintf("%.0f", fc.MessagesMean[j]))
		mono(fmtUs(fc.MountUs[j]))
	})
}

// fmtUs is a microsecond count at the precision a frame's parts need: µs
// below a millisecond, two decimals of ms up to a second.
func fmtUs(us int64) string {
	switch {
	case us == 0:
		return "—"
	case us < 1000:
		return fmt.Sprintf("%d µs", us)
	case us < 1_000_000:
		return fmt.Sprintf("%.2f ms", float64(us)/1000)
	}
	return fmt.Sprintf("%.2f s", float64(us)/1e6)
}
