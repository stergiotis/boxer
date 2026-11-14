package lanesink

import (
	"encoding/binary"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// WirePrefix is the one-byte type prefix lane messages carry on the carrier's
// WebSocket and on the tab worker's port, beside the video, input, session
// and mesh prefixes (ADR-0372 §SD4). The messages below are what
// follows it.
const WirePrefix byte = 0x05

// KindE is the first byte of a lane message. Kinds below 0x80 go toward the
// device, the others toward Go. Integers are little-endian.
type KindE uint8

const (
	// KindOpen announces a stream and the epoch its first chunks carry: u32
	// stream, u32 epoch, u32 sample rate, u8 channels, u8 sample format.
	KindOpen KindE = 0x01
	// KindChunk carries frames of a stream: u32 stream, u32 epoch, u64 the
	// index of its first frame within the epoch, then interleaved samples to
	// the end of the message. The index is what lets a device that missed a
	// chunk know it: it plays the next one behind what it has and counts the
	// frames in between as played, so the two sides agree again on how many
	// are in flight.
	KindChunk KindE = 0x02
	// KindFlush drops what the device holds of a stream and starts the epoch
	// the following chunks carry: u32 stream, u32 epoch.
	KindFlush KindE = 0x03
	// KindClose ends a stream: u32 stream.
	KindClose KindE = 0x04
	// KindWant tells a suspended device that a stream is open and nothing
	// plays it; it has no fields. It is sent only to a device that has
	// announced itself, as everything else is. A page starts its sound machinery
	// on it, and not before, so a session in which no app asks for an output
	// never touches the browser's audio.
	KindWant KindE = 0x05
	// KindProbe asks a device to announce itself again; it has no fields. A
	// carrier writes it when the session's owner became a connection that
	// had announced a device before, since that connection's page may not
	// have seen itself lose the session in between. This package never
	// sends it.
	KindProbe KindE = 0x06

	// KindDevice announces the device, and voids every stream of an earlier
	// announcement: u8 state, u32 sample rate.
	KindDevice KindE = 0x81
	// KindPlayed reports how many frames of a stream's epoch the device has
	// played: u32 stream, u32 epoch, u64 frames.
	KindPlayed KindE = 0x82
)

// SampleFormatE is how a chunk's samples are encoded.
type SampleFormatE uint8

// SampleFormatS16LE is interleaved little-endian signed 16-bit, the one
// format the lane carries (ADR-0372 §SD6).
const SampleFormatS16LE SampleFormatE = 1

// DeviceStateE is what a device says about itself.
type DeviceStateE uint8

const (
	// DeviceAbsent is no device: no viewer, or one that went away.
	DeviceAbsent DeviceStateE = 0
	// DeviceSuspended is a device the browser has not let play yet; it
	// starts suspended until the person acts on the page.
	DeviceSuspended DeviceStateE = 1
	// DeviceRunning is a device that plays what it is sent.
	DeviceRunning DeviceStateE = 2
)

// String returns the state's lowercase name.
func (inst DeviceStateE) String() (s string) {
	switch inst {
	case DeviceAbsent:
		return "absent"
	case DeviceSuspended:
		return "suspended"
	case DeviceRunning:
		return "running"
	}
	return "unknown"
}

// chunkHeaderLen is the length of a chunk message before its samples.
const chunkHeaderLen = 17

// The sample rates a device may announce. Outside them an announcement is
// taken for no device: a rate is a figure a browser reports, and one far off
// is a mistake that would size buffers.
const (
	MinDeviceRate = 8000
	MaxDeviceRate = 384000
)

// AppendOpen appends an open message to dst.
func AppendOpen(dst []byte, stream uint32, epoch uint32, sampleRate uint32, channels uint8) (out []byte) {
	out = append(dst, byte(KindOpen))
	out = binary.LittleEndian.AppendUint32(out, stream)
	out = binary.LittleEndian.AppendUint32(out, epoch)
	out = binary.LittleEndian.AppendUint32(out, sampleRate)
	return append(out, channels, byte(SampleFormatS16LE))
}

// AppendChunk appends a chunk message carrying samples, converted to signed
// 16-bit with clipping, to dst. first is the index of its first frame within
// the epoch.
func AppendChunk(dst []byte, stream uint32, epoch uint32, first uint64, samples []float32) (out []byte) {
	out = append(dst, byte(KindChunk))
	out = binary.LittleEndian.AppendUint32(out, stream)
	out = binary.LittleEndian.AppendUint32(out, epoch)
	out = binary.LittleEndian.AppendUint64(out, first)
	for _, v := range samples {
		out = binary.LittleEndian.AppendUint16(out, uint16(toS16(v)))
	}
	return out
}

// toS16 scales a sample in [-1, 1] to signed 16-bit, clipping what lies
// outside and reading a NaN as silence.
func toS16(v float32) (s int16) {
	f := float64(v) * 32767
	switch {
	case f != f:
		return 0
	case f >= 32767:
		return 32767
	case f <= -32768:
		return -32768
	}
	return int16(math.RoundToEven(f))
}

// AppendFlush appends a flush message to dst.
func AppendFlush(dst []byte, stream uint32, epoch uint32) (out []byte) {
	out = append(dst, byte(KindFlush))
	out = binary.LittleEndian.AppendUint32(out, stream)
	return binary.LittleEndian.AppendUint32(out, epoch)
}

// AppendClose appends a close message to dst.
func AppendClose(dst []byte, stream uint32) (out []byte) {
	out = append(dst, byte(KindClose))
	return binary.LittleEndian.AppendUint32(out, stream)
}

// AppendWant appends a want message to dst.
func AppendWant(dst []byte) (out []byte) { return append(dst, byte(KindWant)) }

// AppendProbe appends a probe message to dst.
func AppendProbe(dst []byte) (out []byte) { return append(dst, byte(KindProbe)) }

// AppendDevice appends a device announcement to dst. A device writes it; so
// does a carrier, with [DeviceAbsent], when the connection that owned the
// device went away.
func AppendDevice(dst []byte, state DeviceStateE, sampleRate uint32) (out []byte) {
	out = append(dst, byte(KindDevice), byte(state))
	return binary.LittleEndian.AppendUint32(out, sampleRate)
}

// AppendPlayed appends a played report to dst.
func AppendPlayed(dst []byte, stream uint32, epoch uint32, frames uint64) (out []byte) {
	out = append(dst, byte(KindPlayed))
	out = binary.LittleEndian.AppendUint32(out, stream)
	out = binary.LittleEndian.AppendUint32(out, epoch)
	return binary.LittleEndian.AppendUint64(out, frames)
}

// Message is one decoded lane message; which fields are set follows Kind.
type Message struct {
	Kind       KindE
	Stream     uint32
	Epoch      uint32
	SampleRate uint32
	Channels   uint8
	Format     SampleFormatE
	State      DeviceStateE
	// Frames is a played report's count, and a chunk's first frame.
	Frames uint64
	// Samples are a chunk's sample bytes, aliasing the decoded buffer.
	Samples []byte
}

// Decode reads one lane message, without its wire prefix.
func Decode(b []byte) (m Message, err error) {
	if len(b) < 1 {
		return m, eh.New("empty lane message")
	}
	m.Kind = KindE(b[0])
	need := func(n int) (ok bool) {
		if len(b) < n {
			err = eb.Build().Uint8("kind", b[0]).Int("len", len(b)).Int("need", n).Errorf("short lane message")
			return false
		}
		return true
	}
	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(b[at:]) }
	switch m.Kind {
	case KindOpen:
		if need(15) {
			m.Stream, m.Epoch, m.SampleRate, m.Channels, m.Format = u32(1), u32(5), u32(9), b[13], SampleFormatE(b[14])
		}
	case KindChunk:
		if need(chunkHeaderLen) {
			m.Stream, m.Epoch, m.Frames, m.Samples = u32(1), u32(5), binary.LittleEndian.Uint64(b[9:]), b[chunkHeaderLen:]
		}
	case KindFlush:
		if need(9) {
			m.Stream, m.Epoch = u32(1), u32(5)
		}
	case KindClose:
		if need(5) {
			m.Stream = u32(1)
		}
	case KindWant, KindProbe:
	case KindDevice:
		if need(6) {
			m.State, m.SampleRate = DeviceStateE(b[1]), u32(2)
		}
	case KindPlayed:
		if need(17) {
			m.Stream, m.Epoch, m.Frames = u32(1), u32(5), binary.LittleEndian.Uint64(b[9:])
		}
	default:
		err = eb.Build().Uint8("kind", b[0]).Errorf("unknown lane message kind")
	}
	return m, err
}
