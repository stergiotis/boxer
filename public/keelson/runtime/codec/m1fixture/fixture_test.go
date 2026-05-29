//go:build llm_generated_opus47

package m1fixture

import (
	"bytes"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"

	"github.com/stergiotis/boxer/public/functional/option"
)

func sampleM1Sample() M1Sample {
	return M1Sample{
		Id:           0x0011223344556677,
		Ts:           time.Unix(1700000000, 0).UTC(),
		Source:       "m1-fixture",
		Severity:     7,
		MajorVer:     42,
		Sequence:     0xCAFEBABE,
		LatencyNanos: 1_234_567_890_123,
		CpuPct:       3.14,
		LoadAvg1:     2.71828,
		Healthy:      true,
		PeerV4:       [4]byte{10, 0, 0, 42},
		PeerV6:       [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01},
		LastSuccess:  option.Some(time.Unix(1699999000, 0).UTC()),
		OperatorName: option.Some("alice"),
		Tags:         []string{"t1", "t2", "t3"},
		CapBits:      roaring.BitmapOf(1000001, 2000002, 3000003),
	}
}

// --- Tests. ---

// TestMarshal_EmptyBatch asserts that Marshal on a zero-row batch
// writes zero bytes and does not panic. Catches regressions in the
// `Len() == 0` early-exit path and the pooled-buffer reset.
func TestMarshal_EmptyBatch(t *testing.T) {
	cols := &M1SampleColumns{}
	var buf bytes.Buffer
	if err := cols.Marshal(&buf); err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if got := buf.Len(); got != 0 {
		t.Fatalf("expected 0 bytes from empty batch, got %d", got)
	}
}
