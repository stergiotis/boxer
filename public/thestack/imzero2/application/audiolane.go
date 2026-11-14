package application

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/science/audio/sink/lanesink"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostaudio"
)

// The variables the client reads the lane's two FIFOs from
// (rust/imzero2/src/imzero2/audiolane.rs). Mirrored by
// imzero2env.AudioLaneToDevice and imzero2env.AudioLaneFromDevice.
const (
	audioLaneToDeviceEnv   = "IMZERO2_AUDIO_LANE_TO_DEVICE"
	audioLaneFromDeviceEnv = "IMZERO2_AUDIO_LANE_FROM_DEVICE"
)

const (
	// audioLaneQueue bounds the messages waiting to be written to the
	// client. The device's flow control keeps what is in flight far below
	// it; a full queue is a client that stopped reading.
	audioLaneQueue = 256
	// audioLaneMaxMessage is the largest message accepted from the client; a
	// length above it is a broken stream.
	audioLaneMaxMessage = 4 << 20
)

// audioLane is the Go end of the audio lane to the client's carrier
// (ADR-0372, proposed, §SD4): two FIFOs, the shape of the wake channel, each
// message a little-endian u32 length and that many bytes. What the device
// sends ([hostaudio.Send]) is written to one; what the carrier forwards from
// the viewer's page is read from the other and handed in
// ([hostaudio.Deliver]).
type audioLane struct {
	dir      string
	down, up string
	queue    chan []byte
	done     chan struct{}
	close    sync.Once
	// full latches the first dropped message, so the log says it once.
	full atomic.Bool

	mu     sync.Mutex
	reader *os.File
}

// newAudioLane makes the two FIFOs and names them in cmd's environment. It
// returns nil where FIFOs are not available; the session then has no audio
// device.
func newAudioLane(cmd *exec.Cmd) (l *audioLane, err error) {
	if !fifoSupported {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "imzero2-audio-")
	if err != nil {
		return nil, eh.Errorf("unable to make the audio lane's directory: %w", err)
	}
	l = &audioLane{
		dir:   dir,
		down:  filepath.Join(dir, "to-device"),
		up:    filepath.Join(dir, "from-device"),
		queue: make(chan []byte, audioLaneQueue),
		done:  make(chan struct{}),
	}
	for _, p := range []string{l.down, l.up} {
		if err = makeFifo(p); err != nil {
			_ = os.RemoveAll(dir)
			return nil, eh.Errorf("unable to make an audio lane fifo: %w", err)
		}
	}
	cmd.Env = append(cmd.Environ(), audioLaneToDeviceEnv+"="+l.down, audioLaneFromDeviceEnv+"="+l.up)
	return l, nil
}

// start runs the lane's writer and reader and makes the lane the process's
// sender. Each open waits for the client to open the other end; a client
// that never does (one without a carrier) leaves the lane silent.
func (inst *audioLane) start() {
	go inst.write()
	go inst.read()
	hostaudio.SetSender(inst.send)
}

// send queues one message for the client without waiting; msg is copied.
func (inst *audioLane) send(msg []byte) {
	framed := make([]byte, 0, 4+len(msg))
	framed = binary.LittleEndian.AppendUint32(framed, uint32(len(msg)))
	framed = append(framed, msg...)
	select {
	case inst.queue <- framed:
	default:
		if inst.full.CompareAndSwap(false, true) {
			log.Warn().Msg("audio lane: the client is not reading; messages are dropped")
		}
	}
}

func (inst *audioLane) write() {
	f, err := os.OpenFile(inst.down, os.O_WRONLY, 0)
	if err != nil {
		log.Warn().Err(err).Msg("audio lane: unable to open the fifo to the client; the session has no audio device")
		return
	}
	defer func() { _ = f.Close() }()
	for {
		select {
		case <-inst.done:
			return
		case framed := <-inst.queue:
			// One write per message, so a message is never interleaved.
			if _, err = f.Write(framed); err != nil {
				// The client has exited or closed its end.
				return
			}
		}
	}
}

func (inst *audioLane) read() {
	f, err := os.OpenFile(inst.up, os.O_RDONLY, 0)
	if err != nil {
		log.Warn().Err(err).Msg("audio lane: unable to open the fifo from the client; the session has no audio device")
		return
	}
	inst.mu.Lock()
	inst.reader = f
	inst.mu.Unlock()
	defer func() { _ = f.Close() }()
	// With the client gone there is no device: streams go back to the clock.
	defer hostaudio.Deliver(lanesink.AppendDevice(nil, lanesink.DeviceAbsent, 0))
	r := bufio.NewReader(f)
	var head [4]byte
	for {
		if _, err = io.ReadFull(r, head[:]); err != nil {
			return
		}
		n := binary.LittleEndian.Uint32(head[:])
		if n > audioLaneMaxMessage {
			log.Warn().Uint32("bytes", n).Msg("audio lane: message from the client exceeds the limit; the lane is closed")
			return
		}
		msg := make([]byte, n)
		if _, err = io.ReadFull(r, msg); err != nil {
			return
		}
		hostaudio.Deliver(msg)
	}
}

// Close stops the lane and removes the FIFOs. A goroutine still waiting for
// the client to open its end is released by opening the other end here.
func (inst *audioLane) Close() (err error) {
	inst.close.Do(func() {
		hostaudio.SetSender(nil)
		close(inst.done)
		if r, e := os.OpenFile(inst.down, os.O_RDONLY|nonBlockFlag, 0); e == nil {
			_ = r.Close()
		}
		if w, e := os.OpenFile(inst.up, os.O_WRONLY|nonBlockFlag, 0); e == nil {
			_ = w.Close()
		}
		inst.mu.Lock()
		if inst.reader != nil {
			_ = inst.reader.Close()
		}
		inst.mu.Unlock()
		err = os.RemoveAll(inst.dir)
	})
	return err
}
