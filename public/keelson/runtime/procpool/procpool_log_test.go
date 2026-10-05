package procpool

import (
	"bytes"
	"sync"

	"github.com/stergiotis/boxer/public/observability/logging"
)

// syncBuffer collects log events from the pool's goroutines, one record per
// event: under binary_log the events are CBOR, where splitting on newlines
// does not work.
type syncBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	records [][]byte
}

func (inst *syncBuffer) Write(p []byte) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.records = append(inst.records, bytes.Clone(p))
	return inst.buf.Write(p)
}

func (inst *syncBuffer) String() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.buf.String()
}

// find returns the first event whose message is msg, decoded in whichever
// encoding the build selects.
func (inst *syncBuffer) find(msg string) (out map[string]any) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for _, raw := range inst.records {
		v, err := logging.UnmarshallZerologMsg(raw)
		if err != nil {
			continue
		}
		m := make(map[string]any)
		switch e := v.(type) {
		case map[string]any:
			m = e
		case map[any]any:
			for k, val := range e {
				if ks, ok := k.(string); ok {
					m[ks] = val
				}
			}
		}
		if m["message"] == msg {
			out = m
			return
		}
	}
	return
}
