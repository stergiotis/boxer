package application

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

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
	// audioLaneMaxMessage is the largest message accepted from the client.
	// What a device sends is a few integers; a length above this is a
	// broken stream.
	audioLaneMaxMessage = 4096
	// audioLanePoll is how often an end the client has not opened yet is
	// tried again.
	audioLanePoll = 20 * time.Millisecond
)

// audioLane is the Go end of the audio lane to the client's carrier
// (ADR-0372, proposed, §SD4): two FIFOs, each message a little-endian u32
// length and that many bytes. What the device sends ([hostaudio.Send]) is
// written to one; what the carrier forwards from the viewer's page is read
// from the other and handed in ([hostaudio.Deliver]).
//
// Neither end is opened with a call that waits for the client: an end the
// client has not opened is tried again until it has or the lane is closed.
// A client that has no carrier never opens them, and Close then has nothing
// to release.
type audioLane struct {
	dir      string
	down, up string
	queue    chan []byte
	done     chan struct{}
	close    sync.Once
	handle   *hostaudio.Lane
	// full latches while the queue is full, so the log says it once per
	// episode.
	full atomic.Bool

	mu     sync.Mutex
	files  []*os.File
	closed bool
}

// newAudioLane makes the two FIFOs and names them in cmd's environment. It
// returns nil where FIFOs are not available; the host then has no lane and
// plays through what the machine has.
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

// start runs the lane's writer and reader and makes the lane the process's.
func (inst *audioLane) start() {
	inst.handle = hostaudio.Attach(inst.send)
	go inst.write()
	go inst.read()
}

// send queues one message for the client without waiting; msg is copied. A
// message that finds the queue full is lost, which the device's flow control
// recovers from once the client reads again.
func (inst *audioLane) send(msg []byte) {
	framed := make([]byte, 0, 4+len(msg))
	framed = binary.LittleEndian.AppendUint32(framed, uint32(len(msg)))
	framed = append(framed, msg...)
	select {
	case inst.queue <- framed:
		inst.full.Store(false)
	default:
		if inst.full.CompareAndSwap(false, true) {
			log.Warn().Msg("audio lane: the client is not reading; messages are dropped")
		}
	}
}

// keep records a file for Close to close; it reports false, having closed
// the file, when the lane is closed already.
func (inst *audioLane) keep(f *os.File) (ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed {
		_ = f.Close()
		return false
	}
	inst.files = append(inst.files, f)
	return true
}

// pause waits one poll interval; it reports false once the lane is closed.
func (inst *audioLane) pause() (ok bool) {
	select {
	case <-inst.done:
		return false
	case <-time.After(audioLanePoll):
		return true
	}
}

func (inst *audioLane) write() {
	// Opened without waiting: with no reader on the FIFO this fails, and is
	// tried again, until the client has opened its end.
	var f *os.File
	for {
		var err error
		f, err = os.OpenFile(inst.down, os.O_WRONLY|nonBlockFlag, 0)
		if err == nil {
			break
		}
		if !errors.Is(err, errNoReader) {
			log.Warn().Err(err).Msg("audio lane: unable to open the fifo to the client; the session has no audio device")
			return
		}
		if !inst.pause() {
			return
		}
	}
	if !inst.keep(f) {
		return
	}
	// The client reads the lane: it has a carrier.
	inst.handle.Connected()
	for {
		select {
		case <-inst.done:
			return
		case framed := <-inst.queue:
			// One write per message, so a message is never interleaved.
			if _, err := f.Write(framed); err != nil {
				// The client has exited or closed its end, or the lane was
				// closed. There is no device behind it any more.
				inst.handle.Detach()
				hostaudio.Deliver(lanesink.AppendDevice(nil, lanesink.DeviceAbsent, 0))
				return
			}
		}
	}
}

func (inst *audioLane) read() {
	f, err := os.OpenFile(inst.up, os.O_RDONLY|nonBlockFlag, 0)
	if err != nil {
		log.Warn().Err(err).Msg("audio lane: unable to open the fifo from the client; the session has no audio device")
		return
	}
	if !inst.keep(f) {
		return
	}
	// With the client gone there is no device: streams go back to the clock.
	defer hostaudio.Deliver(lanesink.AppendDevice(nil, lanesink.DeviceAbsent, 0))
	r := bufio.NewReader(f)
	var head [4]byte
	connected := false
	for {
		if _, err = io.ReadFull(r, head[:]); err != nil {
			// A FIFO nobody has opened for writing reads as ended. Until
			// the client has sent something that is "not yet".
			if errors.Is(err, io.EOF) && !connected && inst.pause() {
				continue
			}
			return
		}
		connected = true
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

// Close stops the lane and removes the FIFOs. Closing the files releases a
// goroutine waiting in a read or a write.
func (inst *audioLane) Close() (err error) {
	inst.close.Do(func() {
		if inst.handle != nil {
			inst.handle.Detach()
		}
		close(inst.done)
		inst.mu.Lock()
		inst.closed = true
		for _, f := range inst.files {
			_ = f.Close()
		}
		inst.mu.Unlock()
		err = os.RemoveAll(inst.dir)
	})
	return err
}
