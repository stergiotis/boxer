package imageview

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// countingSource is a one-plane source that counts its own histogram, the
// count waiting until released so it is still under way when the viewer
// moves on.
type countingSource struct {
	gate    chan struct{}
	started chan struct{}
}

func (inst *countingSource) Info() SourceInfo {
	return SourceInfo{Width: 64, Height: 64, Model: imaging.ColorModelGray, Kind: imaging.SampleKindU8}
}

func (inst *countingSource) Tile(ctx context.Context, level, col, row int) (TileData, error) {
	return TileData{}, context.Canceled
}

func (inst *countingSource) Histogram(ctx context.Context, plane int) (h stats.Histogram, err error) {
	close(inst.started)
	select {
	case <-inst.gate:
	case <-ctx.Done():
		err = ctx.Err()
	}
	return
}

var _ HistogramSourceI = (*countingSource)(nil)

// The count of a source's histogram runs off the frame while the frame may
// replace or clear the source; the count must hold the source it was asked
// of rather than read the viewer's field. Under the race detector this test
// fails when the count reads the field.
func TestAHistogramCountUnderWayDoesNotReadTheViewersSource(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "", Options{})
	defer v.Close()
	src := &countingSource{gate: make(chan struct{}), started: make(chan struct{})}
	require.NoError(t, v.SetSource(src, false))
	_, ok := v.Histogram()
	require.False(t, ok, "the count has only been asked for")
	// No synchronisation with the count before the source is cleared: the
	// count must not need any.
	require.NoError(t, v.SetSource(nil, false))
	close(src.gate)
	select {
	case <-src.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the count never reached the source it was asked of")
	}
}
