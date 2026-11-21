package agent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

// A call's row is written while another goroutine routes the call: a
// status poll runs on a goroutine of its request's own (ADR-0386 SD5), and
// it records the final row while route still writes the call under mu.
// Under -race this fails when record reads the call outside mu.
func TestRecordReadsTheCallWhileItIsRouted(t *testing.T) {
	r := newRig(t, false)
	svc := r.svc
	rec := &callRec{key: "k", instance: 7}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 200 {
			svc.mu.Lock()
			rec.callId = "call-" + string(rune('a'+i%26))
			rec.consent = rec.callId
			svc.mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			svc.record(nil, rec, "final", phaseOutcome(opwire.PhaseCompleted, ""))
		}
	}()
	wg.Wait()
	assert.NotEmpty(t, svc.Actions())
}
