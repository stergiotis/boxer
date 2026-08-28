package decode

// KindE names the decoder a recording is routed to (ADR-0208 §SD5).
type KindE uint8

const (
	// KindUnknown is the zero value: the header was too short to tell.
	KindUnknown KindE = 0
	// KindWAV is the native RIFF/WAVE, RF64 and BW64 reader in the wavfile
	// package.
	KindWAV KindE = 1
	// KindFfmpeg is the streaming ffmpeg decoder, [FfmpegSource].
	KindFfmpeg KindE = 2
)

// AllKinds lists the decoders [OpenE] can route to, KindUnknown excluded.
var AllKinds = []KindE{KindWAV, KindFfmpeg}

func (inst KindE) String() (s string) {
	switch inst {
	case KindWAV:
		return "wav"
	case KindFfmpeg:
		return "ffmpeg"
	}
	return "unknown"
}

// sniffBytes is how many leading bytes [Sniff] needs: the container four-CC,
// its 32-bit size field and the form type behind it.
const sniffBytes = 12

// Sniff reports which decoder head's container belongs to. It recognises the
// three spellings of the WAVE container the native reader handles and routes
// everything else to ffmpeg; fewer than sniffBytes bytes is [KindUnknown],
// which [OpenE] also hands to ffmpeg. It opens nothing and runs nothing, so a
// test or a tool can ask about bytes it already has.
func Sniff(head []byte) (kind KindE) {
	if len(head) < sniffBytes {
		return KindUnknown
	}
	if string(head[8:12]) != "WAVE" {
		return KindFfmpeg
	}
	switch string(head[0:4]) {
	case "RIFF", "RF64", "BW64":
		return KindWAV
	}
	return KindFfmpeg
}
