//go:build llm_generated_opus46

package eh

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/rs/zerolog"
)

// =============================================================================
// Strategy 1: ErrorMarshalFunc
// =============================================================================

func TestErrorMarshalFuncHuman(t *testing.T) {
	err := New("connection refused")
	result := ErrorMarshalFuncHuman(err)

	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if !strings.Contains(s, "connection refused") {
		t.Fatal("expected error message in output")
	}
	if !strings.Contains(s, "Error:") {
		t.Fatal("expected 'Error:' prefix")
	}
}

func TestErrorMarshalFuncHuman_InLogger(t *testing.T) {
	// Save and restore
	origMarshal := zerolog.ErrorMarshalFunc
	defer func() { zerolog.ErrorMarshalFunc = origMarshal }()

	zerolog.ErrorMarshalFunc = ErrorMarshalFuncHuman

	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	inner := New("db timeout")
	outer := Errorf("query failed: %w", inner)
	logger.Error().Err(outer).Msg("request failed")

	output := buf.String()
	t.Log("JSON output:", output)

	// The error field should contain our formatted text
	if !strings.Contains(output, "query failed") {
		t.Fatal("expected error message in JSON output")
	}
	if !strings.Contains(output, "cause:") {
		t.Fatal("expected 'cause:' in formatted error")
	}
}

func TestErrorMarshalFuncStructured(t *testing.T) {
	err := New("test")
	result := ErrorMarshalFuncStructured(err)
	if result == nil {
		t.Fatal("expected non-nil")
	}

	// Should be a LogObjectMarshaler
	_, ok := result.(zerolog.LogObjectMarshaler)
	if !ok {
		t.Fatal("expected LogObjectMarshaler")
	}
}

// =============================================================================
// Strategy 2: ConsoleFormatError
// =============================================================================

func TestConsoleFormatError_StringPassthrough(t *testing.T) {
	formatter := ConsoleFormatError(false)
	result := formatter("simple error")
	if result != "simple error" {
		t.Fatalf("simple strings should pass through, got: %q", result)
	}
}

func TestConsoleFormatError_PreformattedString(t *testing.T) {
	formatter := ConsoleFormatError(false)
	// Simulate a string that was produced by ErrorMarshalFuncHuman
	input := "Error: query failed\n└── cause: connection refused"
	result := formatter(input)
	if !strings.Contains(result, "query failed") {
		t.Fatal("expected formatted content")
	}
	// Should be indented
	if !strings.Contains(result, "  ") {
		t.Fatal("expected indentation")
	}
}

func TestConsoleFormatError_ErrorInterface(t *testing.T) {
	formatter := ConsoleFormatError(false)
	err := New("direct error")
	result := formatter(err)
	if !strings.Contains(result, "direct error") {
		t.Fatalf("expected error message, got: %q", result)
	}
}

func TestConsoleFormatError_WithColor(t *testing.T) {
	formatter := ConsoleFormatError(true)
	err := New("colored error")
	result := formatter(err)
	if !strings.Contains(result, "\033[") {
		t.Fatal("expected ANSI codes in colored mode")
	}
}

func TestConsoleFormatError_WithConsoleWriter(t *testing.T) {
	origMarshal := zerolog.ErrorMarshalFunc
	defer func() { zerolog.ErrorMarshalFunc = origMarshal }()

	zerolog.ErrorMarshalFunc = ErrorMarshalFuncHuman

	var buf bytes.Buffer
	writer := zerolog.ConsoleWriter{
		Out:                 &buf,
		NoColor:             true,
		FormatErrFieldValue: ConsoleFormatError(false),
	}
	logger := zerolog.New(writer)

	inner := New("connection refused")
	outer := Errorf("query failed: %w", inner)
	logger.Error().Err(outer).Msg("request failed")

	output := buf.String()
	t.Log("Console output:\n" + output)

	if !strings.Contains(output, "request failed") {
		t.Fatal("expected log message")
	}
}

// =============================================================================
// Strategy 3: FormatExtra
// =============================================================================

func TestConsoleFormatErrorExtra_NoError(t *testing.T) {
	fn := ConsoleFormatErrorExtra(false)
	var buf bytes.Buffer
	evt := map[string]interface{}{
		"message": "hello",
	}
	err := fn(evt, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatal("expected no output when no error field")
	}
}

func TestConsoleFormatErrorExtra_SimpleString(t *testing.T) {
	fn := ConsoleFormatErrorExtra(false)
	var buf bytes.Buffer
	evt := map[string]interface{}{
		zerolog.ErrorFieldName: "simple error",
	}
	err := fn(evt, &buf)
	if err != nil {
		t.Fatal(err)
	}
	// Simple string without "cause:" should not produce extra output
	if buf.Len() != 0 {
		t.Fatalf("simple errors should not produce extra block, got: %q", buf.String())
	}
}

func TestConsoleFormatErrorExtra_FormattedString(t *testing.T) {
	fn := ConsoleFormatErrorExtra(false)
	var buf bytes.Buffer

	formatted := "Error: query failed\n└── cause: connection refused\n    at Connect (db.go:15)"
	evt := map[string]interface{}{
		zerolog.ErrorFieldName: formatted,
	}
	err := fn(evt, &buf)
	if err != nil {
		t.Fatal(err)
	}
	output := buf.String()
	t.Log("Extra output:\n" + output)
	if !strings.Contains(output, "query failed") {
		t.Fatal("expected formatted error in extra block")
	}
	if !strings.Contains(output, "cause:") {
		t.Fatal("expected cause in extra block")
	}
}

func TestConsoleFormatErrorExtra_FullIntegration(t *testing.T) {
	origMarshal := zerolog.ErrorMarshalFunc
	defer func() { zerolog.ErrorMarshalFunc = origMarshal }()

	zerolog.ErrorMarshalFunc = ErrorMarshalFuncHuman

	var buf bytes.Buffer
	writer := zerolog.ConsoleWriter{
		Out:           &buf,
		NoColor:       true,
		FieldsExclude: []string{zerolog.ErrorFieldName},
		FormatExtra:   ConsoleFormatErrorExtra(false),
	}
	logger := zerolog.New(writer)

	inner := New("connection refused")
	outer := Errorf("query failed: %w", inner)
	logger.Error().Err(outer).Str("user", "alice").Msg("request failed")

	output := buf.String()
	t.Log("Full integration output:\n" + output)

	if !strings.Contains(output, "request failed") {
		t.Fatal("expected log message")
	}
	if !strings.Contains(output, "user=alice") {
		t.Fatal("expected field in output")
	}
	// Error block should appear below the main line
	if !strings.Contains(output, "cause:") {
		t.Fatal("expected formatted error block")
	}
}

// =============================================================================
// Helper: indentBlock
// =============================================================================

func TestIndentBlock(t *testing.T) {
	input := "line1\nline2\nline3"
	result := indentBlock(input, "  ")
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("expected indentation, got: %q", line)
		}
	}
}

func TestIndentBlock_EmptyLines(t *testing.T) {
	input := "line1\n\nline3"
	result := indentBlock(input, "  ")
	lines := strings.Split(result, "\n")
	if lines[1] != "" {
		t.Fatalf("empty lines should stay empty, got: %q", lines[1])
	}
}

// =============================================================================
// Visual demo
// =============================================================================

func TestZerologIntegration_VisualDemo(t *testing.T) {
	origMarshal := zerolog.ErrorMarshalFunc
	defer func() { zerolog.ErrorMarshalFunc = origMarshal }()

	var buf bytes.Buffer
	divider := strings.Repeat("─", 70)

	// Strategy 1: ErrorMarshalFunc (JSON with human error string)
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("STRATEGY 1: ErrorMarshalFunc → JSON output\n")
	buf.WriteString(divider + "\n")
	zerolog.ErrorMarshalFunc = ErrorMarshalFuncHuman
	logger1 := zerolog.New(&buf)
	data, _ := cbor.Marshal(map[string]string{"table": "users"})
	err1 := ErrorfWithData(data, "constraint violation")
	err1 = Errorf("insert failed: %w", err1)
	logger1.Error().Err(err1).Str("db", "primary").Msg("write failed")
	buf.WriteByte('\n')

	// Strategy 2: ConsoleWriter + FormatErrFieldValue
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("STRATEGY 2: ConsoleWriter + FormatErrFieldValue\n")
	buf.WriteString(divider + "\n")
	zerolog.ErrorMarshalFunc = ErrorMarshalFuncHuman
	writer2 := zerolog.ConsoleWriter{
		Out:                 &buf,
		NoColor:             true,
		FormatErrFieldValue: ConsoleFormatError(false),
	}
	logger2 := zerolog.New(writer2)
	logger2.Error().Err(err1).Str("db", "primary").Msg("write failed")
	buf.WriteByte('\n')

	// Strategy 3: FormatExtra (cleanest)
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("STRATEGY 3: FormatExtra (error as block below log line)\n")
	buf.WriteString(divider + "\n")
	zerolog.ErrorMarshalFunc = ErrorMarshalFuncHuman
	writer3 := zerolog.ConsoleWriter{
		Out:           &buf,
		NoColor:       true,
		FieldsExclude: []string{zerolog.ErrorFieldName},
		FormatExtra:   ConsoleFormatErrorExtra(false),
	}
	logger3 := zerolog.New(writer3)
	logger3.Error().Err(err1).Str("db", "primary").Msg("write failed")
	buf.WriteByte('\n')

	// Strategy 3 with complex error tree
	buf.WriteString("\n" + divider + "\n")
	buf.WriteString("STRATEGY 3: Complex error tree\n")
	buf.WriteString(divider + "\n")
	e1 := New("disk full")
	e2 := New("permission denied")
	e3 := errors.New("context canceled")
	joined := errors.Join(e1, e2, e3)
	outer := Errorf("batch write failed: %w", joined)
	logger3.Error().Err(outer).Str("op", "batch").Msg("storage failure")
	buf.WriteByte('\n')

	// Prevent the test output from being eaten by go test buffering
	t.Log(buf.String())
	// Also write to stderr for immediate visibility during test -v
	_, _ = os.Stderr.Write(buf.Bytes())
}
