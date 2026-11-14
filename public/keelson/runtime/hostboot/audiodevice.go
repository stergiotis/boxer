package hostboot

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/audioout"
	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/sink/lanesink"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostaudio"
)

// laneWait is how long after the client was launched its end of the audio
// lane is waited for before the host concludes the client has no carrier.
const laneWait = 3 * time.Second

// laneOrPulse is the audio device of a host that was told to serve remote
// viewers (ADR-0372, proposed, §SD2). Where the process has an audio lane to
// a carrier, the person is at a viewer's page and the stream goes there.
// Where it has none — FIFOs are not available, or the client that was
// launched has no carrier and never opened its end — the person is at the
// machine, and the stream goes to its sound server as it would without the
// carrier configured.
type laneOrPulse struct {
	lane  *lanesink.Device
	pulse audioout.PulseDevice
}

var _ audioout.DeviceI = (*laneOrPulse)(nil)

// Name implements [audioout.DeviceI]: the kind of device a stream opened now
// would get. It waits as OpenSink does.
func (inst *laneOrPulse) Name() (name string) {
	if hostaudio.Await(laneWait) {
		return inst.lane.Name()
	}
	return inst.pulse.Name()
}

// OpenSink implements [audioout.DeviceI]. Early in the life of the process
// it may wait, at most until laneWait after the launch, for the client to
// open its end of the lane.
func (inst *laneOrPulse) OpenSink(src pcm.SourceI, label string) (s sink.SinkI, err error) {
	if hostaudio.Await(laneWait) {
		return inst.lane.OpenSink(src, label)
	}
	return inst.pulse.OpenSink(src, label)
}
