package peaks

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
)

// Overview is a whole recording reduced to a fixed number of min/max columns
// per channel — what a thumbnail of it draws. It is the pyramid's answer to
// one question, kept on its own: a caller showing many recordings small
// builds the pyramid, asks for the columns, and lets the pyramid go, so what
// it retains per recording is a few hundred bytes a channel whatever the
// recording's length.
type Overview struct {
	Format pcm.Format
	// Frames is the recording's length.
	Frames int64
	// Min and Max are indexed [channel][column], quantised like the
	// pyramid's bins: -127..127 for full scale. Every channel has the same
	// number of columns, which is at most what was asked for — a recording
	// shorter than that in frames has one column per frame.
	Min, Max [][]int8
	// Peak is the largest magnitude over every channel, for a caller that
	// normalises.
	Peak int8
}

// Columns is the number of columns per channel.
func (inst *Overview) Columns() int {
	if inst == nil || len(inst.Min) == 0 {
		return 0
	}
	return len(inst.Min[0])
}

// OverviewE reduces src to at most columns min/max columns per channel in one
// sequential pass. The context is honoured between chunks, as in [BuildE].
func OverviewE(ctx context.Context, src pcm.SourceI, columns int) (inst *Overview, err error) {
	if src == nil {
		return nil, eb.Build().Errorf("nil source")
	}
	if columns <= 0 {
		return nil, eb.Build().Int("columns", columns).Errorf("overview needs at least one column")
	}
	format, frames := src.Format(), src.Frames()
	inst = &Overview{Format: format, Frames: frames}
	channels := int(format.Channels)
	inst.Min, inst.Max = make([][]int8, channels), make([][]int8, channels)
	if frames <= 0 {
		return inst, nil
	}
	p, err := BuildE(ctx, src, DefaultBaseBin(), 0, nil)
	if err != nil {
		return nil, err
	}
	columns = int(min(int64(columns), frames))
	for ch := range channels {
		lo, hi := make([]int8, columns), make([]int8, columns)
		n := p.Columns(0, frames, ch, lo, hi)
		inst.Min[ch], inst.Max[ch] = lo[:n], hi[:n]
	}
	inst.Peak = p.GlobalPeak()
	return inst, nil
}
