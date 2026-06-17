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

// hardwareEncoderName and softwareEncoderName name the VAAPI and the software
// ffmpeg encoder for the codec, independent of which one is currently usable.
// [CodecCaps.EncoderName] picks between them by the probed hardware bit; the
// disabled-encoder list (see [Model.DisabledEncoders]) needs both names even
// when neither lane works.
func (c Codec) hardwareEncoderName() string {
	switch c {
	case CodecVP9:
		return "vp9_vaapi"
	case CodecAV1:
		return "av1_vaapi"
	default:
		return "h264_vaapi"
	}
}

func (c Codec) softwareEncoderName() string {
	switch c {
	case CodecVP9:
		return "libvpx-vp9"
	case CodecAV1:
		return "libsvtav1"
	default:
		return "libopenh264"
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

	// Per-lane probe-fail reason codes (codeclane.LaneProbe.reason_code):
	// bits 5-7 carry the hardware lane's reason, bits 8-10 the software
	// lane's. Zero when the lane works (its flagEncode* bit is set) or the
	// host reported no reason.
	shiftHardwareFail = 5
	shiftSoftwareFail = 8
	maskFailReason    = 0x7
)

// ProbeFailReason is why an encoder lane failed its host trial-encode — the
// wire code the Rust host packs into the capability flags (mirrors
// codeclane.LaneProbe). ProbeOK means the lane works, or no reason was
// reported.
type ProbeFailReason uint8

const (
	ProbeOK             ProbeFailReason = 0
	ProbeNotBuilt       ProbeFailReason = 1 // ffmpeg lacks the encoder (not compiled in)
	ProbeNoDevice       ProbeFailReason = 2 // no usable VAAPI device on the host
	ProbeEncodeRejected ProbeFailReason = 3 // device opened, driver rejected the encode (ENOSYS)
	ProbeOther          ProbeFailReason = 4 // some other ffmpeg failure
)

// String is a concise, table-ready phrase for the failure, or "" for ProbeOK
// (and any unknown future code, so the caller can fall back).
func (r ProbeFailReason) String() string {
	switch r {
	case ProbeNotBuilt:
		return "encoder not built into this ffmpeg"
	case ProbeNoDevice:
		return "no VAAPI device on this host"
	case ProbeEncodeRejected:
		return "GPU driver can't encode this codec"
	case ProbeOther:
		return "encoder probe failed"
	default:
		return ""
	}
}

// CodecCaps is one codec's published availability: the host encode side and
// the browser decode side, each split into hardware vs software.
type CodecCaps struct {
	Codec           Codec
	EncodeSoftware  bool
	EncodeHardware  bool
	DecodeSupported bool
	DecodeSmooth    bool
	DecodeHardware  bool
	// EncodeHardwareFail / EncodeSoftwareFail say why the respective encode
	// lane is unavailable when its Encode* bit is false (ProbeOK otherwise).
	EncodeHardwareFail ProbeFailReason
	EncodeSoftwareFail ProbeFailReason
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
		return c.Codec.hardwareEncoderName()
	}
	return c.Codec.softwareEncoderName()
}

// CodecString is the WebCodecs string for this codec at the given stream
// dimensions. The level is derived from the resolution (WebCodecs validates the
// codec-string level against the coded size), mirroring the Rust host
// (codeclane.rs) and the viewer probe (viewer/index.html) — keep the three in
// sync. H.264's exact profile/level the viewer derives from the SPS; this is the
// representative string for the control's table.
func (c CodecCaps) CodecString(width, height int) string {
	switch c.Codec {
	case CodecVP9:
		return "vp09.00." + vp9Level(width, height) + ".08"
	case CodecAV1:
		return "av01.0." + av1Level(width, height) + "M.08"
	default:
		return "avc1.42E0" + h264Level(width, height)
	}
}

// vp9Level / av1Level / h264Level pick the smallest level whose max picture
// size (VP9/AV1) or max frame size in macroblocks (H.264) covers width×height.
// These mirror codeclane.rs (Rust host) and viewer/index.html — keep identical.
func vp9Level(w, h int) string {
	switch s := w * h; {
	case s <= 36864:
		return "10"
	case s <= 73728:
		return "11"
	case s <= 122880:
		return "20"
	case s <= 245760:
		return "21"
	case s <= 552960:
		return "30"
	case s <= 983040:
		return "31"
	case s <= 2228224:
		return "40"
	case s <= 8912896:
		return "50"
	case s <= 35651584:
		return "60"
	default:
		return "61"
	}
}

func av1Level(w, h int) string {
	switch s := w * h; {
	case s <= 147456:
		return "00"
	case s <= 278784:
		return "01"
	case s <= 665856:
		return "04"
	case s <= 1065024:
		return "05"
	case s <= 2359296:
		return "08"
	case s <= 8912896:
		return "12"
	default:
		return "16"
	}
}

func h264Level(w, h int) string {
	switch mbs := ((w + 15) / 16) * ((h + 15) / 16); {
	case mbs <= 1620:
		return "1e"
	case mbs <= 3600:
		return "1f"
	case mbs <= 5120:
		return "20"
	case mbs <= 8192:
		return "28"
	case mbs <= 8704:
		return "2a"
	case mbs <= 22080:
		return "32"
	case mbs <= 36864:
		return "33"
	default:
		return "3c"
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
			Codec:              Codec(id),
			EncodeSoftware:     f&flagEncodeSoftware != 0,
			EncodeHardware:     f&flagEncodeHardware != 0,
			DecodeSupported:    f&flagDecodeSupported != 0,
			DecodeSmooth:       f&flagDecodeSmooth != 0,
			DecodeHardware:     f&flagDecodeHardware != 0,
			EncodeHardwareFail: ProbeFailReason((f >> shiftHardwareFail) & maskFailReason),
			EncodeSoftwareFail: ProbeFailReason((f >> shiftSoftwareFail) & maskFailReason),
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

// DisabledEncoder is one encoder lane the host probed but cannot use: a
// (codec, backend) pair with the ffmpeg encoder name and a human reason. The
// hardware lane being disabled does not stop a codec being offered — the
// software lane still serves it — so these are surfaced in their own table
// rather than removing the codec from [Model.Offered].
type DisabledEncoder struct {
	Codec   Codec
	Encoder string // the ffmpeg encoder that is unavailable, e.g. "h264_vaapi"
	Backend string // "hardware" or "software"
	Reason  string // why the host cannot use it
}

// DisabledEncoders lists the encoder lanes the host probed unusable, in codec
// order with the hardware lane before the software one. Each codec contributes
// a row per lane that failed its probe (a false EncodeHardware/EncodeSoftware
// bit); a codec whose lanes both work contributes none. The host probes every
// lane with a short trial encode (codeclane.probe_lane), so a disabled row
// means that trial did not produce output — not merely that the lane went
// unselected. Reason is the specific probe cause (ProbeFailReason) when the
// host reported one.
func (m *Model) DisabledEncoders() []DisabledEncoder {
	out := make([]DisabledEncoder, 0, 2*len(m.Caps))
	for _, c := range m.Caps {
		if !c.EncodeHardware {
			out = append(out, DisabledEncoder{
				Codec:   c.Codec,
				Encoder: c.Codec.hardwareEncoderName(),
				Backend: "hardware",
				Reason:  failReason(c.EncodeHardwareFail, "hardware"),
			})
		}
		if !c.EncodeSoftware {
			out = append(out, DisabledEncoder{
				Codec:   c.Codec,
				Encoder: c.Codec.softwareEncoderName(),
				Backend: "software",
				Reason:  failReason(c.EncodeSoftwareFail, "software"),
			})
		}
	}
	return out
}

// failReason renders the table reason for a disabled lane: the specific probe
// cause when the host reported one, else a backend-appropriate generic phrase.
// The fallback covers a host that predates per-lane reasons (it reports ProbeOK
// with the encode bit clear), so the table never shows an empty reason.
func failReason(r ProbeFailReason, backend string) string {
	if s := r.String(); s != "" {
		return s
	}
	if backend == "hardware" {
		return "no usable VAAPI encoder on this host"
	}
	return "encoder unavailable in this ffmpeg build"
}
