//go:build llm_generated_opus47

package progressbar

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestLogWriter_NonTTYPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	bar := New(100, "items")
	bar.SetWriter(&buf)
	if _, err := bar.LogWriter().Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.String() != "hello\n" {
		t.Fatalf("got %q; want %q", buf.String(), "hello\n")
	}
}

func TestLogWriter_AppendsMissingNewline(t *testing.T) {
	var buf bytes.Buffer
	bar := New(100, "items")
	bar.SetWriter(&buf)
	_, _ = bar.LogWriter().Write([]byte("hello"))
	if buf.String() != "hello\n" {
		t.Fatalf("got %q; want %q", buf.String(), "hello\n")
	}
}

func TestLogWriter_PreservesExistingNewline(t *testing.T) {
	var buf bytes.Buffer
	bar := New(100, "items")
	bar.SetWriter(&buf)
	_, _ = bar.LogWriter().Write([]byte("hello\n"))
	if buf.String() != "hello\n" {
		t.Fatalf("double newline: got %q", buf.String())
	}
}

func TestLogWriter_EmptyWriteNoop(t *testing.T) {
	var buf bytes.Buffer
	bar := New(100, "items")
	bar.SetWriter(&buf)
	_, _ = bar.LogWriter().Write(nil)
	if buf.Len() != 0 {
		t.Fatalf("empty write should not produce output, got %q", buf.String())
	}
}

func TestLogWriter_TTYPrefixesClearLine(t *testing.T) {
	var buf bytes.Buffer
	bar := New(100, "items")
	bar.SetWriter(&buf)
	bar.isTTY = true
	_, _ = bar.LogWriter().Write([]byte("hello\n"))
	out := buf.String()
	if !strings.HasPrefix(out, "\r\x1b[2K") {
		t.Fatalf("expected TTY write to start with \\r\\x1b[2K, got %q", out)
	}
	if !strings.Contains(out, "hello\n") {
		t.Fatalf("expected hello\\n in output, got %q", out)
	}
}

func TestLogWriter_PrintlnSugar(t *testing.T) {
	var buf bytes.Buffer
	bar := New(100, "items")
	bar.SetWriter(&buf)
	bar.Println("a", 1, "b")
	if buf.String() != "a 1 b\n" {
		t.Fatalf("Println: got %q; want %q", buf.String(), "a 1 b\n")
	}
}

func TestLogWriter_PrintfSugar(t *testing.T) {
	var buf bytes.Buffer
	bar := New(100, "items")
	bar.SetWriter(&buf)
	bar.Printf("n=%d", 42)
	if buf.String() != "n=42\n" {
		t.Fatalf("Printf auto-newline: got %q; want %q", buf.String(), "n=42\n")
	}
}

// TestLogWriter_AtomicUnderConcurrentRender asserts that every log line
// appears in the output immediately preceded by the clear-line sequence —
// i.e. a concurrent render never slips bytes between the \r\x1b[2K prefix
// and the log payload, and never splits either across another frame.
func TestLogWriter_AtomicUnderConcurrentRender(t *testing.T) {
	var buf bytes.Buffer
	bar := New(1000, "items")
	bar.SetWriter(&buf)
	bar.isTTY = true // exercise the TTY code path; mutex covers both

	const N = 200
	lw := bar.LogWriter()
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			fmt.Fprintf(lw, "log-%04d\n", i)
		}(i)
		go func() {
			defer wg.Done()
			bar.Add(1)
			bar.render()
		}()
	}
	wg.Wait()

	out := buf.String()
	for i := 0; i < N; i++ {
		needle := fmt.Sprintf("\r\x1b[2Klog-%04d\n", i)
		if !strings.Contains(out, needle) {
			t.Fatalf("log-%04d was not emitted atomically with its clear-line prefix", i)
		}
	}
}

// TestLogWriter_LogLineOnOwnRowAfterRender verifies the narrative flow:
// render → log → render leaves the log line clearly demarcated (its
// clear-line prefix clips the preceding render's content) and the
// following render lands on a fresh row.
func TestLogWriter_LogLineOnOwnRowAfterRender(t *testing.T) {
	var buf bytes.Buffer
	bar := New(1000, "items")
	bar.SetWriter(&buf)
	bar.isTTY = true
	bar.processed.Store(100)

	bar.render()
	bar.Println("log line 1")
	bar.render()

	out := buf.String()
	// After the first render (ending in \x1b[K) we must see \r\x1b[2K,
	// then the log, then \n, then another \r render-frame prefix.
	idx := strings.Index(out, "\r\x1b[2Klog line 1\n")
	if idx < 0 {
		t.Fatalf("expected clear-line + log + \\n sequence in %q", out)
	}
	after := out[idx+len("\r\x1b[2Klog line 1\n"):]
	if !strings.HasPrefix(after, "\r") {
		t.Fatalf("expected next render frame to start with \\r after log line, got %q", after)
	}
}
