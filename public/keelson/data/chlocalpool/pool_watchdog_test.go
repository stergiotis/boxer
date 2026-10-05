package chlocalpool

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncBuffer collects log output from the watchdog goroutine. zerolog hands
// each event to Write in one call, so records keeps one entry per event: the
// events are CBOR under binary_log, where splitting on newlines does not work.
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

func (inst *syncBuffer) Records() (out [][]byte) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	out = append(out, inst.records...)
	return
}

func (inst *syncBuffer) String() (out string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.buf.String()
}

// Regression, 2026-07-24 review: the sweep logged acquired_age as the time
// since bornAt — the spawn, not the handover — so it read high by however
// long the worker had sat idle first and did not match the deadline the
// sweep had just applied. The acquisition time was in the map all along.
//
// The worker is deliberately left idle for longer than the watchdog
// deadline before being acquired, so the two ages are far enough apart to
// tell which one is being reported.
func TestPool_WatchdogLogsAgeSinceAcquisitionNotSpawn(t *testing.T) {
	requireBinary(t)
	const (
		idleSoak = 900 * time.Millisecond
		deadline = 300 * time.Millisecond
	)
	cfg := testConfig(t)
	cfg.MinIdle = 1
	cfg.MaxConcurrent = 2
	cfg.WatchdogMaxLifetime = deadline

	var logs syncBuffer
	pool, err := New(cfg, zerolog.New(&logs))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = pool.Stop(ctx)
	})

	waitFor(t, 5*time.Second, func() bool { return pool.Stats().Idle >= 1 }, "MinIdle filled")
	// Let the worker age while idle. The watchdog only judges acquired
	// workers, so nothing is reaped during this window.
	time.Sleep(idleSoak)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Forget to Close: the watchdog must reap it.
	select {
	case <-w.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("watchdog did not reap the forgotten worker")
	}

	var line map[string]any
	for _, raw := range logs.Records() {
		m := decodeLogRecord(raw)
		if m == nil {
			continue
		}
		if msg, _ := m["message"].(string); msg == "watchdog reaping forgotten worker" {
			line = m
			break
		}
	}
	require.NotNil(t, line, "no reap log line found in:\n%q", logs.String())

	acquiredAge, ok := logNumber(line["acquired_age"])
	require.True(t, ok, "acquired_age missing or not numeric: %v", line)
	workerAge, ok := logNumber(line["worker_age"])
	require.True(t, ok, "worker_age missing or not numeric: %v", line)

	// zerolog renders durations in milliseconds by default.
	assert.Less(t, acquiredAge, float64(idleSoak.Milliseconds()),
		"acquired_age must measure the hold, not the worker's whole life")
	assert.GreaterOrEqual(t, workerAge, acquiredAge+float64(idleSoak.Milliseconds())*0.5,
		"worker_age must include the idle soak that acquired_age excludes")
	assert.GreaterOrEqual(t, acquiredAge, float64(deadline.Milliseconds())*0.5,
		"acquired_age should be around the deadline the sweep applied")
}

// decodeLogRecord decodes one zerolog event in whichever encoding the build
// tags select (JSON, or CBOR under binary_log); nil when it is not an object.
func decodeLogRecord(raw []byte) (out map[string]any) {
	v, err := logging.UnmarshallZerologMsg(raw)
	if err != nil {
		return
	}
	switch m := v.(type) {
	case map[string]any:
		out = m
	case map[any]any:
		out = make(map[string]any, len(m))
		for k, val := range m {
			if ks, ok := k.(string); ok {
				out[ks] = val
			}
		}
	}
	return
}

func logNumber(v any) (f float64, ok bool) {
	ok = true
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	case int64:
		f = float64(n)
	case uint64:
		f = float64(n)
	default:
		ok = false
	}
	return
}
