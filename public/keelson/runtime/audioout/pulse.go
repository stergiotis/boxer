package audioout

import (
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/pulsesink"
)

// PulseDevice opens every stream on the PulseAudio (or PipeWire pulse)
// server the process finds, the device of a host whose person sits at the
// machine (ADR-0208 §SD6). Each stream connects on its own, so a host with
// no server answers every open with the reason.
type PulseDevice struct {
	// Server is the PulseAudio server string; empty is the default lookup.
	Server string
}

var _ DeviceI = PulseDevice{}

// Name implements [DeviceI].
func (inst PulseDevice) Name() (name string) { return "pulse" }

// OpenSink implements [DeviceI]. The server's mixer shows the stream under
// the app's id.
func (inst PulseDevice) OpenSink(src pcm.SourceI, label string) (s sink.SinkI, err error) {
	name := "boxer"
	if label != "" {
		name += " " + label
	}
	out, err := pulsesink.Open(src, pulsesink.Options{AppName: name, Server: inst.Server})
	if err != nil {
		return nil, err
	}
	return out, nil
}
