package videopipeline

import (
	"slices"
	"testing"
)

// TestDecodeFlagContract pins the flag bit layout shared with the Rust host's
// build_video_caps (headless.rs): bit0 sw-encode, bit1 decode-supported,
// bit2 decode-smooth, bit3 decode-hardware, bit4 hw-encode.
func TestDecodeFlagContract(t *testing.T) {
	ids := []uint64{0, 1, 2}
	flags := []uint32{
		1 | 2 | 16,    // H.264: sw+hw encode, decode-supported
		1 | 2 | 4 | 8, // VP9: sw-encode, decode HW (supported+smooth+power-efficient)
		16,            // AV1: hw-encode only, browser can't decode
	}
	caps := Decode(ids, slices.Values(flags))
	if len(caps) != 3 {
		t.Fatalf("want 3 caps, got %d", len(caps))
	}
	if !caps[0].EncodeSoftware || !caps[0].EncodeHardware || !caps[0].DecodeSupported || !caps[0].Offerable() {
		t.Errorf("H.264 caps wrong: %+v", caps[0])
	}
	if caps[1].EncodeHardware || !caps[1].EncodeSoftware || !caps[1].DecodeHardware || !caps[1].DecodeSmooth || !caps[1].Offerable() {
		t.Errorf("VP9 caps wrong (sw-encode, HW decode): %+v", caps[1])
	}
	if !caps[2].EncodeHardware || caps[2].EncodeSoftware || !caps[2].HostCanEncode() || caps[2].DecodeSupported || caps[2].Offerable() {
		t.Errorf("AV1 caps wrong (hw-encode only, not offerable): %+v", caps[2])
	}
}

func TestDecodeStreamInfo(t *testing.T) {
	s := DecodeStreamInfo(slices.Values([]uint64{1920, 986, 30}))
	if s.Width != 1920 || s.Height != 986 || s.Fps != 30 || !s.Valid() {
		t.Errorf("stream info wrong: %+v", s)
	}
	if DecodeStreamInfo(slices.Values([]uint64{})).Valid() {
		t.Errorf("empty stream info should be invalid")
	}
}

// TestModelUpdateFallback covers the active-selection policy: fall off a codec
// that stops being offerable, but don't churn while it stays offerable.
func TestModelUpdateFallback(t *testing.T) {
	m := &Model{Active: CodecAV1}
	m.Update([]CodecCaps{
		{Codec: CodecH264, EncodeSoftware: true, DecodeSupported: true},
		{Codec: CodecAV1, EncodeSoftware: true, DecodeSupported: false},
	})
	if m.Active != CodecH264 {
		t.Errorf("AV1 not decodable → want fallback to H.264, got %v", m.Active)
	}
	if len(m.Offered()) != 2 {
		t.Errorf("want 2 offered codecs (both host-encodable), got %d", len(m.Offered()))
	}
}

// TestDisabledEncoders surfaces the encoder lanes that failed their host probe,
// in codec order with hardware before software. The common case (a Fedora-mesa
// box) is the three VAAPI lanes — each codec encodes in software but its
// hardware encoder is unavailable.
func TestDisabledEncoders(t *testing.T) {
	m := &Model{}
	m.Update([]CodecCaps{
		{Codec: CodecH264, EncodeSoftware: true, EncodeHardware: false, DecodeSupported: true}, // HW lane disabled
		{Codec: CodecVP9, EncodeSoftware: true, EncodeHardware: true, DecodeSupported: true},   // both work → no row
		{Codec: CodecAV1, EncodeSoftware: false, EncodeHardware: false, DecodeSupported: true}, // neither → two rows
	})
	dis := m.DisabledEncoders()
	if len(dis) != 3 {
		t.Fatalf("want 3 disabled lanes (H.264 hw, AV1 hw, AV1 sw), got %d: %+v", len(dis), dis)
	}
	if dis[0].Codec != CodecH264 || dis[0].Backend != "hardware" || dis[0].Encoder != "h264_vaapi" {
		t.Errorf("row0 want H.264 hardware h264_vaapi, got %+v", dis[0])
	}
	if dis[0].Reason == "" {
		t.Errorf("a disabled lane must carry a reason: %+v", dis[0])
	}
	// A codec with neither lane contributes hardware before software.
	if dis[1].Codec != CodecAV1 || dis[1].Backend != "hardware" || dis[1].Encoder != "av1_vaapi" {
		t.Errorf("row1 want AV1 hardware av1_vaapi, got %+v", dis[1])
	}
	if dis[2].Codec != CodecAV1 || dis[2].Backend != "software" || dis[2].Encoder != "libsvtav1" {
		t.Errorf("row2 want AV1 software libsvtav1, got %+v", dis[2])
	}
}
