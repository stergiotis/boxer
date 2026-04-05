package eh

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// =============================================================================
// FormatError tests
// =============================================================================

func TestFormatError_Nil(t *testing.T) {
	s := FormatErrorPlainS(nil)
	if !strings.Contains(s, "<nil error>") {
		t.Fatalf("expected nil message, got: %q", s)
	}
}

func TestFormatError_Simple(t *testing.T) {
	err := New("connection refused")
	s := FormatErrorPlainS(err)
	t.Log("\n" + s)

	if !strings.Contains(s, "Error:") {
		t.Fatal("expected 'Error:' header")
	}
	if !strings.Contains(s, "connection refused") {
		t.Fatal("expected error message")
	}
	if !strings.Contains(s, "format_test.go") {
		t.Fatal("expected source file reference")
	}
}

func TestFormatError_LinearChain(t *testing.T) {
	inner := New("connection refused")
	mid := Errorf("query failed: %w", inner)
	outer := Errorf("user lookup failed: %w", mid)

	s := FormatErrorPlainS(outer)
	t.Log("\n" + s)

	if !strings.Contains(s, "user lookup failed") {
		t.Fatal("expected outermost message")
	}
	if !strings.Contains(s, "query failed") {
		t.Fatal("expected middle message")
	}
	if !strings.Contains(s, "connection refused") {
		t.Fatal("expected innermost message")
	}
	if !strings.Contains(s, "cause:") {
		t.Fatal("expected 'cause:' labels")
	}
	if !strings.Contains(s, "Stack trace") {
		t.Fatal("expected stack trace section")
	}
}

func TestFormatError_LinearChain_Dedup(t *testing.T) {
	inner := New("connection refused")
	outer := Errorf("query failed: %w", inner)

	s := FormatErrorPlainS(outer)
	t.Log("\n" + s)

	// The outer message should be deduplicated to just "query failed"
	// (without repeating "connection refused" inline).
	// "connection refused" should appear as the cause line and in the stack annotation.
	lines := strings.Split(s, "\n")
	outerLine := ""
	for _, line := range lines {
		if strings.Contains(line, "Error:") {
			outerLine = line
			break
		}
	}
	if strings.Contains(outerLine, "connection refused") {
		t.Fatalf("outer Error line should be deduplicated, got: %q", outerLine)
	}
	if !strings.Contains(outerLine, "query failed") {
		t.Fatalf("outer Error line should say 'query failed', got: %q", outerLine)
	}
	// "connection refused" should appear as a cause line
	hasCause := false
	for _, line := range lines {
		if strings.Contains(line, "cause:") && strings.Contains(line, "connection refused") {
			hasCause = true
			break
		}
	}
	if !hasCause {
		t.Fatal("expected 'connection refused' as a cause line")
	}
}

func TestFormatError_WithCBOR(t *testing.T) {
	data, _ := cbor.Marshal(map[string]interface{}{
		"host":       "db-primary.internal",
		"port":       5432,
		"timeout_ms": 3000,
	})
	err := ErrorfWithData(data, "connection refused")
	wrapped := Errorf("query failed: %w", err)

	s := FormatErrorPlainS(wrapped)
	t.Log("\n" + s)

	if !strings.Contains(s, "data:") {
		t.Fatal("expected 'data:' label")
	}
	if !strings.Contains(s, "db-primary.internal") {
		t.Fatal("expected CBOR data content")
	}
}

func TestFormatError_JoinedErrors(t *testing.T) {
	e1 := New("disk full")
	e2 := New("permission denied")
	e3 := errors.New("context canceled")
	joined := errors.Join(e1, e2, e3)
	outer := Errorf("batch write failed: %w", joined)

	s := FormatErrorPlainS(outer)
	t.Log("\n" + s)

	if !strings.Contains(s, "batch write failed") {
		t.Fatal("expected outer message")
	}
	if !strings.Contains(s, "disk full") {
		t.Fatal("expected first branch")
	}
	if !strings.Contains(s, "permission denied") {
		t.Fatal("expected second branch")
	}
	if !strings.Contains(s, "context canceled") {
		t.Fatal("expected third branch")
	}
}

func TestFormatError_ErrorTree(t *testing.T) {
	dbErr := New("connection reset")
	dbWrap := Errorf("db query: %w", dbErr)

	cacheErr := New("cache miss")
	cacheWrap := Errorf("cache lookup: %w", cacheErr)

	joined := errors.Join(dbWrap, cacheWrap)
	top := Errorf("data fetch failed: %w", joined)

	s := FormatErrorPlainS(top)
	t.Log("\n" + s)

	if !strings.Contains(s, "data fetch failed") {
		t.Fatal("expected top message")
	}
	if !strings.Contains(s, "connection reset") {
		t.Fatal("expected db error")
	}
	if !strings.Contains(s, "cache miss") {
		t.Fatal("expected cache error")
	}
}

func TestFormatError_DeepChain(t *testing.T) {
	err := New("root cause")
	for i := 0; i < 5; i++ {
		err = Errorf("layer-%d: %w", i, err)
	}

	s := FormatErrorPlainS(err)
	t.Log("\n" + s)

	if !strings.Contains(s, "root cause") {
		t.Fatal("expected root cause")
	}
	if !strings.Contains(s, "layer-4") {
		t.Fatal("expected outermost layer")
	}
}

func TestFormatError_StdlibOnly(t *testing.T) {
	err := errors.New("plain error")
	s := FormatErrorPlainS(err)
	t.Log("\n" + s)

	if !strings.Contains(s, "plain error") {
		t.Fatal("expected message")
	}
	// Should not crash, should be a simple format
}

func TestFormatError_MixedStdlibAndEh(t *testing.T) {
	stdErr := fmt.Errorf("os: file not found")
	ehErr := Errorf("config load failed: %w", stdErr)
	topErr := Errorf("startup failed: %w", ehErr)

	s := FormatErrorPlainS(topErr)
	t.Log("\n" + s)

	if !strings.Contains(s, "startup failed") {
		t.Fatal("expected top message")
	}
	if !strings.Contains(s, "config load failed") {
		t.Fatal("expected mid message")
	}
	if !strings.Contains(s, "file not found") {
		t.Fatal("expected inner message")
	}
}

func TestFormatError_StripAnsi(t *testing.T) {
	err := New("test")
	withAnsi := FormatErrorWithStackS(err)
	withoutAnsi := FormatErrorPlainS(err)

	if strings.Contains(withoutAnsi, "\033[") {
		t.Fatal("plain output should not contain ANSI codes")
	}
	if !strings.Contains(withAnsi, "\033[") {
		t.Fatal("colored output should contain ANSI codes")
	}
	// Content should be the same modulo ANSI
	if !strings.Contains(withoutAnsi, "test") {
		t.Fatal("plain output should contain the error message")
	}
}

func TestStripAnsi(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"\033[1mBold\033[0m", "Bold"},
		{"\033[31mRed\033[0m", "Red"},
		{"no ansi", "no ansi"},
		{"\033[2m\033[31mcombined\033[0m", "combined"},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripAnsi(tt.input)
		if got != tt.expected {
			t.Errorf("stripAnsi(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// =============================================================================
// Visual demo (run with -v to see the output)
// =============================================================================

func TestFormatError_VisualDemo(t *testing.T) {
	var buf bytes.Buffer
	divider := strings.Repeat("─", 60)

	// Demo 1: Simple
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("CASE 1: Simple error\n")
	buf.WriteString(divider + "\n")
	FormatErrorWithStack(&buf, New("connection refused"))

	// Demo 2: Linear chain
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("CASE 2: Linear wrap chain\n")
	buf.WriteString(divider + "\n")
	inner := New("connection refused")
	mid := Errorf("query failed: %w", inner)
	outer := Errorf("user lookup failed: %w", mid)
	FormatErrorWithStack(&buf, outer)

	// Demo 3: With CBOR data
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("CASE 3: With structured data\n")
	buf.WriteString(divider + "\n")
	data, _ := cbor.Marshal(map[string]interface{}{
		"host": "db-primary.internal",
		"port": 5432,
	})
	dataErr := ErrorfWithData(data, "connection refused")
	dataWrap := Errorf("query failed: %w", dataErr)
	FormatErrorWithStack(&buf, dataWrap)

	// Demo 4: Joined errors (tree)
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("CASE 4: Joined errors (tree)\n")
	buf.WriteString(divider + "\n")
	e1 := New("disk full")
	e2 := New("permission denied")
	e3 := errors.New("context canceled")
	joined := errors.Join(e1, e2, e3)
	joinOuter := Errorf("batch write failed: %w", joined)
	FormatErrorWithStack(&buf, joinOuter)

	// Demo 5: Deep tree
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("CASE 5: Error tree (join of wrapped)\n")
	buf.WriteString(divider + "\n")
	dbErr := New("connection reset")
	dbWrap := Errorf("db query: %w", dbErr)
	cacheErr := New("cache miss")
	cacheWrap := Errorf("cache lookup: %w", cacheErr)
	treeJoined := errors.Join(dbWrap, cacheWrap)
	treeTop := Errorf("data fetch failed: %w", treeJoined)
	FormatErrorWithStack(&buf, treeTop)

	// Demo 6: Plain text (no ANSI)
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("CASE 6: Plain text (no ANSI)\n")
	buf.WriteString(divider + "\n")
	FormatErrorPlain(&buf, outer)

	t.Log(buf.String())
}
