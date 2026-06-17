package videopipeline

import (
	"slices"
	"testing"
)

// TestDecodeFlagContract pins the flag bit layout shared with the Rust host's
// build_video_caps (headless.rs): bit0 host-encode, bit1 decode-supported,
// bit2 smooth, bit3 power-efficient.
func TestDecodeFlagContract(t *testing.T) {
	ids := []uint64{0, 1, 2}
	flags := []uint32{
		1 | 2,         // H.264: host-encode + decode-supported
		1,             // VP9: host-encode only (browser can't decode)
		1 | 2 | 4 | 8, // AV1: all set
	}
	caps := Decode(ids, slices.Values(flags))
	if len(caps) != 3 {
		t.Fatalf("want 3 caps, got %d", len(caps))
	}
	if !caps[0].HostCanEncode || !caps[0].DecodeSupported || caps[0].Smooth || !caps[0].Offerable() {
		t.Errorf("H.264 caps wrong: %+v", caps[0])
	}
	if !caps[1].HostCanEncode || caps[1].DecodeSupported || caps[1].Offerable() {
		t.Errorf("VP9 caps wrong (host-encode only, not offerable): %+v", caps[1])
	}
	if !(caps[2].Smooth && caps[2].PowerEfficient && caps[2].Offerable()) {
		t.Errorf("AV1 caps wrong (all set): %+v", caps[2])
	}
}

// TestModelUpdateFallback covers the active-selection policy: fall off a codec
// that stops being offerable, but don't churn while it stays offerable.
func TestModelUpdateFallback(t *testing.T) {
	m := &Model{Active: CodecAV1}
	m.Update([]CodecCaps{
		{Codec: CodecH264, HostCanEncode: true, DecodeSupported: true},
		{Codec: CodecAV1, HostCanEncode: true, DecodeSupported: false},
	})
	if m.Active != CodecH264 {
		t.Errorf("AV1 not decodable → want fallback to H.264, got %v", m.Active)
	}
	m.Update([]CodecCaps{
		{Codec: CodecH264, HostCanEncode: true, DecodeSupported: true},
		{Codec: CodecAV1, HostCanEncode: true, DecodeSupported: true},
	})
	if m.Active != CodecH264 {
		t.Errorf("active should stay H.264 (no churn), got %v", m.Active)
	}
	if len(m.Offered()) != 2 {
		t.Errorf("want 2 offered codecs, got %d", len(m.Offered()))
	}
}
