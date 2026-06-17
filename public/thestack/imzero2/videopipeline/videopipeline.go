// Package videopipeline is the Go-side, first-class model of the imzero2
// remote-stream video pipeline (ADR-0088 SD9). It holds the codec
// capabilities the headless host publishes each frame — per codec, whether
// the host can encode it (hardware/software) and whether the connected
// browser can decode it (hardware/software/not) — plus the active codec and
// the stream geometry. The egui2 "video output" control reads this model and
// drives the runtime switch via bindings.SetVideoPipeline.
//
// Capabilities arrive over FFFI2 as the (codecIds, flags) pair from
// bindings.Fetcher.FetchVideoCapabilities; the flag bit layout is a
// cross-language contract with the Rust host's build_video_caps
// (headless.rs) — see the flag constants and the test.
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
	flagEncodeSoftware  = 1 << 0
	flagDecodeSupported = 1 << 1
	flagDecodeSmooth    = 1 << 2
	flagDecodeHardware  = 1 << 3 // browser mediaCapabilities powerEfficient
	flagEncodeHardware  = 1 << 4 // host VAAPI encoder probed working
)

// CodecCaps is one codec's published availability: the host encode side and
// the browser decode side, each split into hardware vs software.
type CodecCaps struct {
	Codec           Codec
	EncodeSoftware  bool
	EncodeHardware  bool
	DecodeSupported bool
	DecodeSmooth    bool
	DecodeHardware  bool
}

// HostCanEncode is true when the host has a working encoder (HW or SW).
func (c CodecCaps) HostCanEncode() bool { return c.EncodeSoftware || c.EncodeHardware }

// Offerable: the host can encode it and the browser can decode it — the
// precondition for selecting it.
func (c CodecCaps) Offerable() bool { return c.HostCanEncode() && c.DecodeSupported }

// EncoderName is the ffmpeg encoder the host would use for this codec — the
// VAAPI encoder when hardware encode is available here, else the software lane.
func (c CodecCaps) EncoderName() string {
	if c.EncodeHardware {
		switch c.Codec {
		case CodecVP9:
			return "vp9_vaapi"
		case CodecAV1:
			return "av1_vaapi"
		default:
			return "h264_vaapi"
		}
	}
	switch c.Codec {
	case CodecVP9:
		return "libvpx-vp9"
	case CodecAV1:
		return "libsvtav1"
	default:
		return "libopenh264"
	}
}

// CodecString is the representative WebCodecs string for this codec. The active
// H.264 stream's exact profile may differ (the viewer derives it from the SPS).
func (c CodecCaps) CodecString() string {
	switch c.Codec {
	case CodecVP9:
		return "vp09.00.41.08"
	case CodecAV1:
		return "av01.0.08M.08"
	default:
		return "avc1.42E01E"
	}
}

// EncodeBackend / DecodeBackend name the hardware/software path for the table.
func (c CodecCaps) EncodeBackend() string {
	if c.EncodeHardware {
		return "hardware"
	}
	return "software"
}

func (c CodecCaps) DecodeBackend() string {
	switch {
	case !c.DecodeSupported:
		return "unsupported"
	case c.DecodeHardware:
		return "hardware"
	case c.DecodeSmooth:
		return "software"
	default:
		return "software?"
	}
}

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
			EncodeSoftware:  f&flagEncodeSoftware != 0,
			EncodeHardware:  f&flagEncodeHardware != 0,
			DecodeSupported: f&flagDecodeSupported != 0,
			DecodeSmooth:    f&flagDecodeSmooth != 0,
			DecodeHardware:  f&flagDecodeHardware != 0,
		})
	}
	return out
}

// StreamInfo is the active stream's telemetry (ADR-0088 fetchVideoStreamInfo:
// [width, height, fps, cadence, bitrate_kbps, frames_sent, frames_dropped,
// frames_in_flight]).
type StreamInfo struct {
	Width, Height, Fps int
	Reactive           bool // render cadence: false=continuous, true=reactive
	BitrateKbps        int  // EMA of the wire bitrate
	FramesSent         int
	FramesDropped      int // coalesced before the encoder under congestion (SD9)
	FramesInFlight     int // sent − decoded: how far the viewer is behind
}

// Valid is true once the host has reported a geometry (i.e. a viewer is live).
func (s StreamInfo) Valid() bool { return s.Width > 0 && s.Height > 0 }

// CadenceName is the render cadence as a word.
func (s StreamInfo) CadenceName() string {
	if s.Reactive {
		return "reactive"
	}
	return "continuous"
}

// DecodeStreamInfo unpacks a FetchVideoStreamInfo result (3 or 8 values).
func DecodeStreamInfo(info iter.Seq[uint64]) StreamInfo {
	v := make([]uint64, 0, 8)
	for x := range info {
		v = append(v, x)
	}
	if len(v) < 3 {
		return StreamInfo{}
	}
	s := StreamInfo{Width: int(v[0]), Height: int(v[1]), Fps: int(v[2])}
	if len(v) >= 8 {
		s.Reactive = v[3] != 0
		s.BitrateKbps = int(v[4])
		s.FramesSent = int(v[5])
		s.FramesDropped = int(v[6])
		s.FramesInFlight = int(v[7])
	}
	return s
}

// Model is the first-class video-pipeline state the control owns.
type Model struct {
	Caps   []CodecCaps
	Active Codec
	Stream StreamInfo
}

// Update replaces the capabilities (called each frame). It keeps the active
// selection unless that codec stops being offerable, in which case it falls
// back to the first offerable codec (or leaves it as-is when nothing is
// offerable yet — e.g. before the first capability report).
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
// Decode standing is annotated per entry so the UI can describe rather than
// hide — keeping the picker stable as the browser's capabilities load.
func (m *Model) Offered() []CodecCaps {
	out := make([]CodecCaps, 0, len(m.Caps))
	for _, c := range m.Caps {
		if c.HostCanEncode() {
			out = append(out, c)
		}
	}
	return out
}
