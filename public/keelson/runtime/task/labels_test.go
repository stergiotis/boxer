package task

import (
	"bytes"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/pprof/profile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A goroutine labelled for a window, and every goroutine it starts, are
// sampled under the window's labels; a goroutine nobody labelled is not.
func TestLabelGoroutineSplitsAProfileByWindow(t *testing.T) {
	api := NewBusApi(ApiConfig{AppId: "test.app", InstanceKey: 7})
	var buf bytes.Buffer
	require.NoError(t, pprof.StartCPUProfile(&buf))
	stop := time.Now().Add(600 * time.Millisecond)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		LabelGoroutine(api)
		wg.Add(1)
		go func() {
			defer wg.Done()
			spinLabelledChild(stop)
		}()
		spinLabelled(stop)
	}()
	go func() {
		defer wg.Done()
		spinPlain(stop)
	}()
	wg.Wait()
	pprof.StopCPUProfile()

	p, err := profile.Parse(&buf)
	require.NoError(t, err)
	byFn := map[string]map[string]int64{}
	for _, s := range p.Sample {
		fn := spinnerOf(s)
		if fn == "" {
			continue
		}
		if byFn[fn] == nil {
			byFn[fn] = map[string]int64{}
		}
		byFn[fn][strings.Join(s.Label[LabelWindow], ",")+"/"+strings.Join(s.Label[LabelApp], ",")] += s.Value[0]
	}
	for _, fn := range []string{"spinLabelled", "spinLabelledChild"} {
		require.NotEmpty(t, byFn[fn], "%s was sampled", fn)
		assert.Equal(t, map[string]int64{"7/test.app": byFn[fn]["7/test.app"]}, byFn[fn], "%s only under the window's labels", fn)
	}
	require.NotEmpty(t, byFn["spinPlain"])
	assert.Equal(t, map[string]int64{"/": byFn["spinPlain"]["/"]}, byFn["spinPlain"], "the plain goroutine carries no labels")
}

func TestLabelGoroutineWithoutAnOwnerLeavesLabelsAlone(t *testing.T) {
	assert.NotPanics(t, func() {
		LabelGoroutine(nil)
		LabelGoroutine(&NoopTaskApi{})
	})
}

// spinnerOf names the spin function a sample's stack runs in, if any.
func spinnerOf(s *profile.Sample) string {
	for _, loc := range s.Location {
		for _, ln := range loc.Line {
			if ln.Function == nil {
				continue
			}
			for _, fn := range []string{"spinLabelledChild", "spinLabelled", "spinPlain"} {
				if strings.HasSuffix(ln.Function.Name, "."+fn) {
					return fn
				}
			}
		}
	}
	return ""
}

var spinSink atomic.Uint64

//go:noinline
func spinLabelled(until time.Time) { spin(until) }

//go:noinline
func spinLabelledChild(until time.Time) { spin(until) }

//go:noinline
func spinPlain(until time.Time) { spin(until) }

func spin(until time.Time) {
	var x uint64
	for time.Now().Before(until) {
		for i := range 10_000 {
			x += uint64(i) * 2654435761
		}
	}
	spinSink.Add(x)
}
