//go:build llm_generated_opus47

package taskerror_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/taskerror"
)

func sampleError() taskerror.TaskError {
	return taskerror.TaskError{
		FactId:    11,
		AtNs:      1_700_000_000_000_000_000,
		TaskId:    "task-abc123",
		Reason:    "connect timeout",
		ErrorText: "*errors.errorString: connect timeout\n\tat foo.go:42",
	}
}

func TestBuscodecAutoRegistersTaskError(t *testing.T) {
	got := buscodec.Lookup[taskerror.TaskError]()
	want := "taskError-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleError()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskerror.TaskError](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.FactId != orig.FactId {
		t.Errorf("FactId: got %v, want %v", got.FactId, orig.FactId)
	}
	if got.AtNs != orig.AtNs {
		t.Errorf("AtNs: got %v, want %v", got.AtNs, orig.AtNs)
	}
	if got.TaskId != orig.TaskId {
		t.Errorf("TaskId: got %q, want %q", got.TaskId, orig.TaskId)
	}
	if got.Reason != orig.Reason {
		t.Errorf("Reason: got %q, want %q", got.Reason, orig.Reason)
	}
	if got.ErrorText != orig.ErrorText {
		t.Errorf("ErrorText: got %q, want %q", got.ErrorText, orig.ErrorText)
	}
}

func TestBuscodecRoundTripReasonOnly(t *testing.T) {
	// Producers may surface a reason-only failure with no Go error
	// attached (handle.Error called with taskErr=nil). ErrorText
	// empty must reconstruct as the literal empty string.
	orig := taskerror.TaskError{
		FactId: 1,
		AtNs:   1_700_000_000_000_000_000,
		TaskId: "task-reason-only",
		Reason: "permission denied",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskerror.TaskError](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ErrorText != "" {
		t.Errorf("ErrorText: got %q, want empty", got.ErrorText)
	}
	if got.Reason != "permission denied" {
		t.Errorf("Reason: got %q, want %q", got.Reason, "permission denied")
	}
}

func TestBuscodecRoundTripMultilineErrorText(t *testing.T) {
	// FormatErrorWithStackS rendering is multi-line; the text
	// section must preserve newlines + indentation as the wire
	// is what errorview / log readers display.
	rendering := "outer wrap: middle wrap: inner: i/o timeout\n" +
		"  at foo.go:42\n" +
		"  at bar.go:88\n"
	orig := taskerror.TaskError{
		FactId:    2,
		AtNs:      1_700_000_000_000_000_000,
		TaskId:    "task-multiline",
		Reason:    "i/o timeout",
		ErrorText: rendering,
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskerror.TaskError](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ErrorText != rendering {
		t.Errorf("ErrorText: got %q, want %q", got.ErrorText, rendering)
	}
	if strings.Count(got.ErrorText, "\n") != strings.Count(rendering, "\n") {
		t.Errorf("ErrorText: newline count differs (got %d, want %d)",
			strings.Count(got.ErrorText, "\n"), strings.Count(rendering, "\n"))
	}
}

func TestBuscodecAtNsLosslessNanoPrecision(t *testing.T) {
	// z64 wire → sub-second nanos round-trip losslessly.
	orig := taskerror.TaskError{
		FactId: 1,
		AtNs:   1_700_000_000_111_222_333,
		TaskId: "task-precision",
		Reason: "tick",
	}
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[taskerror.TaskError](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.AtNs != orig.AtNs {
		t.Errorf("AtNs: got %v, want %v (nanos must round-trip lossless)", got.AtNs, orig.AtNs)
	}
}
