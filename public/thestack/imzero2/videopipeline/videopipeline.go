// Package videopipeline is the Go-side, first-class model of the imzero2
// remote-stream video pipeline (ADR-0088 SD9). It holds the codec
// capabilities the headless host publishes each frame — the intersection of
// what the connected browser can decode and what this host can encode — and
// the codec the control has selected. The egui2 "video output" widget reads
// this model and drives the runtime switch via bindings.SetVideoPipeline.
//
// The capabilities arrive over FFFI2 as the (codecIds, flags) pair returned
// by bindings.Fetcher.FetchVideoCapabilities; [Decode] unpacks them. The flag
// bit layout is a cross-language contract with the Rust host's
// build_video_caps (headless.rs) — see [the flag constants] and the test.
package videopipeline

import "iter"

// Codec is the wire codec id shared with the Rust side
// (CodecLane::VideoCodec::from_u8 and the setVideoPipeline opcode argument).
type Codec uint32

const (
	CodecH264 Codec = 0
	CodecVP9  Codec = 1
	CodecAV1  Codec = 2
)

func (c Codec) String() string {
	switch c {
	case CodecVP9:
		return "VP9"
	case CodecAV1:
		return "AV1"
	default:
		return "H.264"
	}
}

// Capability flag bits — must match build_video_caps in
// rust/imzero2/src/imzero2/headless.rs.
const (
	flagHostEncode      = 1 << 0
	flagDecodeSupported = 1 << 1
	flagSmooth          = 1 << 2
	flagPowerEfficient  = 1 << 3
)

// CodecCaps is one codec's published availability.
type CodecCaps struct {
	Codec           Codec
	HostCanEncode   bool // this host has a working encoder (SD5 probe-encode)
	DecodeSupported bool // the browser reported VideoDecoder support
	Smooth          bool // browser mediaCapabilities: smooth at the geometry
	PowerEfficient  bool // browser mediaCapabilities: hardware-accelerated
}

// Offerable is true when the host can encode the codec and the browser can
// decode it — the precondition for putting it in the picker.
func (c CodecCaps) Offerable() bool { return c.HostCanEncode && c.DecodeSupported }

// Decode unpacks a FetchVideoCapabilities result. flags is the iterator the
// generated fetcher returns; it is consumed once.
func Decode(codecIds []uint64, flags iter.Seq[uint32]) []CodecCaps {
	fs := make([]uint32, 0, len(codecIds))
	for f := range flags {
		fs = append(fs, f)
	}
	out := make([]CodecCaps, 0, len(codecIds))
	for i, id := range codecIds {
		if i >= len(fs) {
			break
		}
		f := fs[i]
		out = append(out, CodecCaps{
			Codec:           Codec(id),
			HostCanEncode:   f&flagHostEncode != 0,
			DecodeSupported: f&flagDecodeSupported != 0,
			Smooth:          f&flagSmooth != 0,
			PowerEfficient:  f&flagPowerEfficient != 0,
		})
	}
	return out
}

// Model is the first-class video-pipeline state the control owns: the latest
// capabilities and the codec the user has selected (the desired state the
// switch drives toward).
type Model struct {
	Caps   []CodecCaps
	Active Codec
}

// Update replaces the capabilities (called each frame from the fetcher). It
// keeps the active selection unless that codec stops being offerable, in
// which case it falls back to the first offerable codec (or leaves it as-is
// when nothing is offerable yet — e.g. before the first capability report).
func (m *Model) Update(caps []CodecCaps) {
	m.Caps = caps
	if m.Find(m.Active).Offerable() {
		return
	}
	for _, c := range caps {
		if c.Offerable() {
			m.Active = c.Codec
			return
		}
	}
}

// Find returns the capabilities for a codec, or a zero value if absent.
func (m *Model) Find(codec Codec) CodecCaps {
	for _, c := range m.Caps {
		if c.Codec == codec {
			return c
		}
	}
	return CodecCaps{Codec: codec}
}

// Offered returns the codecs worth showing in the control (host-encodable).
// Decode support is annotated per entry so the UI can grey-out or warn rather
// than hide — keeping the picker stable as the browser's capabilities load.
func (m *Model) Offered() []CodecCaps {
	out := make([]CodecCaps, 0, len(m.Caps))
	for _, c := range m.Caps {
		if c.HostCanEncode {
			out = append(out, c)
		}
	}
	return out
}
