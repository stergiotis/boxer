package flowoverlay

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
)

// BenchmarkDraw is one frame of the layer at a tick per frame — the whole of
// the Go side's cost: advance every particle, build the segment batch, encode
// it into a channel that discards it. The trial under
// doc/trials/flow-particles-frame-cost measures the same thing in a running
// host; this is where to look for where the time goes.
func BenchmarkDraw(b *testing.B) {
	src, err := vectorfield.NewPyramidE(context.Background(),
		vectorfield.Meta{SpeedMax: 30, Steps: hourly(1)},
		vectorfield.NewGlobalAnalyticLoader(1, vectorfield.Swirl(0)), vectorfield.PyramidOptions{})
	if err != nil {
		b.Fatal(err)
	}
	for _, n := range []int{1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			s := newBenchScene(b, src, Options{Seed: 1, Density: 1e6, MaxParticles: n, Synchronous: true})
			for range 60 {
				s.frame(time.Second / 30)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.frame(time.Second / 30)
			}
			b.ReportMetric(float64(s.layer.Stats().Segments), "segments")
		})
	}
}
