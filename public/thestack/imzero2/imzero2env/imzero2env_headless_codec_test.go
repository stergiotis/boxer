package imzero2env

import "testing"

// TestHeadlessCodec_Values pins the codec strings. They are a cross-language
// contract: the Rust client's VideoCodec::parse (codeclane.rs) maps these
// canonical spellings when reading IMZERO2_HEADLESS_CODEC, so a rename here
// must be matched there. Pinning surfaces a drift as a test failure rather
// than a silent Go/Rust mismatch.
func TestHeadlessCodec_Values(t *testing.T) {
	if HeadlessCodecH264 != "h264" {
		t.Errorf("HeadlessCodecH264: got %q want h264", HeadlessCodecH264)
	}
	if HeadlessCodecVP9 != "vp9" {
		t.Errorf("HeadlessCodecVP9: got %q want vp9", HeadlessCodecVP9)
	}
	if HeadlessCodecAV1 != "av1" {
		t.Errorf("HeadlessCodecAV1: got %q want av1", HeadlessCodecAV1)
	}
}

func TestHeadlessCodec_DefaultsToH264(t *testing.T) {
	HeadlessCodec.SetForTest(t, "")
	if got := HeadlessCodec.Get(); got != HeadlessCodecH264 {
		t.Errorf("HeadlessCodec default: got %q want %q", got, HeadlessCodecH264)
	}
}

func TestHeadlessCodec_AcceptsVP9AndAV1(t *testing.T) {
	HeadlessCodec.SetForTest(t, HeadlessCodecVP9)
	if got := HeadlessCodec.Get(); got != HeadlessCodecVP9 {
		t.Errorf("HeadlessCodec vp9: got %q want %q", got, HeadlessCodecVP9)
	}
	HeadlessCodec.SetForTest(t, HeadlessCodecAV1)
	if got := HeadlessCodec.Get(); got != HeadlessCodecAV1 {
		t.Errorf("HeadlessCodec av1: got %q want %q", got, HeadlessCodecAV1)
	}
}

func TestHeadlessCodec_OutOfSetFallsBackToDefault(t *testing.T) {
	// An unrecognised value is user error; Get falls back to the default
	// (same convention as the other categorial vars — see CategorialStringVar.Get).
	HeadlessCodec.SetForTest(t, "bogus")
	if got := HeadlessCodec.Get(); got != HeadlessCodecH264 {
		t.Errorf("HeadlessCodec out-of-set: got %q want default %q", got, HeadlessCodecH264)
	}
}
