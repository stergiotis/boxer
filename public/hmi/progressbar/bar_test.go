//go:build llm_generated_opus47

package progressbar

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBar_SetWriterDisablesTTY(t *testing.T) {
	bar := New(100, "items")
	bar.SetWriter(&bytes.Buffer{})
	if bar.isTTY {
		t.Fatalf("SetWriter should flip isTTY off")
	}
}

func TestBar_EstimatorExposed(t *testing.T) {
	bar := New(100, "items")
	if bar.Estimator() == nil {
		t.Fatal("Estimator() returned nil")
	}
}

func TestBar_TickAddAreAtomicAndAccurate(t *testing.T) {
	bar := New(0, "items")
	const N = 1000
	const G = 8
	var wg sync.WaitGroup
	for i := 0; i < G; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < N; j++ {
				bar.Tick()
			}
		}()
	}
	for i := 0; i < G; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < N; j++ {
				bar.Add(1)
			}
		}()
	}
	wg.Wait()
	want := int64(2 * G * N)
	if got := bar.Processed(); got != want {
		t.Fatalf("Processed() = %d; want %d", got, want)
	}
}

func TestBar_StopUnblocksOnContextCancel(t *testing.T) {
	bar := New(0, "items")
	bar.SetWriter(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	bar.Start(ctx)
	bar.Tick()
	time.Sleep(20 * time.Millisecond)
	cancel()
	done := make(chan struct{})
	go func() {
		bar.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop hung after ctx cancellation")
	}
}

func TestBar_StopIsReentrantlySafe(t *testing.T) {
	bar := New(100, "items")
	bar.SetWriter(io.Discard)
	bar.Start(context.Background())
	bar.Add(50)
	done := make(chan struct{})
	go func() {
		bar.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop hung")
	}
}

func TestBar_NonTTYEmitsLines(t *testing.T) {
	var buf bytes.Buffer
	bar := New(0, "items")
	bar.SetWriter(&buf)
	bar.processed.Store(100)
	bar.render()
	if !strings.Contains(buf.String(), "100 items") {
		t.Fatalf("expected non-TTY line output to contain counter+label, got %q", buf.String())
	}
}

func TestProxyReader_AddsBytesAndPropagatesClose(t *testing.T) {
	bar := New(0, "bytes")
	data := []byte("hello world")
	src := bytes.NewReader(data)
	pr := bar.NewProxyReader(src)
	if _, err := io.Copy(io.Discard, pr); err != nil {
		t.Fatalf("Copy error: %v", err)
	}
	if got := bar.Processed(); got != int64(len(data)) {
		t.Fatalf("Processed = %d; want %d", got, len(data))
	}
	if err := pr.Close(); err != nil {
		t.Fatalf("Close on non-Closer reader should be nil, got %v", err)
	}
}

type closingReader struct {
	io.Reader
	closed bool
}

func (c *closingReader) Close() error { c.closed = true; return nil }

func TestProxyReader_PropagatesUnderlyingClose(t *testing.T) {
	bar := New(0, "bytes")
	cr := &closingReader{Reader: bytes.NewReader([]byte("x"))}
	pr := bar.NewProxyReader(cr)
	if err := pr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !cr.closed {
		t.Fatalf("Close did not reach underlying reader")
	}
}

func TestProxyWriter_AddsBytes(t *testing.T) {
	bar := New(0, "bytes")
	var buf bytes.Buffer
	pw := bar.NewProxyWriter(&buf)
	if _, err := pw.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := bar.Processed(); got != 5 {
		t.Fatalf("Processed = %d; want 5", got)
	}
	if buf.String() != "hello" {
		t.Fatalf("underlying writer got %q; want %q", buf.String(), "hello")
	}
}
