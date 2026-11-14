// Package hostaudio joins the two ends of the audio lane inside the Go host
// (ADR-0372, proposed, §SD4): the device that plays an app's sound through a
// remote viewer, and the application that launched the imzero2 client and
// holds the pipes to its carrier.
//
// Neither end knows the other, and either may come first. The application
// attaches its lane ([Attach]), says when the client has opened its end
// ([Lane.Connected]) and hands in what arrives ([Deliver]); the device sends
// ([Send]) and says where arrivals go ([SetReceiver]). Until both are there
// a message goes nowhere, which the device reads as nobody listening.
//
// Whether a lane exists is also what a host chooses its device by ([Await]):
// a client that never opens its end has no carrier, and its person is at the
// machine, not at a viewer's page.
package hostaudio

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/science/audio/sink/lanesink"
)

// Lane is one application's audio lane as this package holds it.
type Lane struct {
	send      func(msg []byte)
	started   time.Time
	connected chan struct{}
	once      sync.Once
}

var (
	lane     atomic.Pointer[Lane]
	receiver atomic.Pointer[func(msg []byte)]
)

// Attach makes send the way a lane message leaves toward the client's
// carrier, replacing an earlier lane. The function must not wait and must
// not keep msg. The lane is not connected yet: the client has not opened
// its end.
func Attach(send func(msg []byte)) (l *Lane) {
	l = &Lane{send: send, started: time.Now(), connected: make(chan struct{})}
	lane.Store(l)
	return l
}

// Connected records that the client opened its end of the lane. Idempotent.
func (inst *Lane) Connected() {
	inst.once.Do(func() { close(inst.connected) })
}

// Detach removes the lane, unless a later one has replaced it. Idempotent.
func (inst *Lane) Detach() {
	lane.CompareAndSwap(inst, nil)
}

// Send carries one lane message, without its wire prefix, toward the
// carrier. Without a lane it is dropped.
func Send(msg []byte) {
	if l := lane.Load(); l != nil {
		l.send(msg)
	}
}

// Await reports whether this process has an audio lane to a carrier. With
// no lane attached it says no at once, and with a connected one yes. For a
// lane the client has not opened yet it waits, until the client does or
// until wait has passed since the lane was attached: a client with a
// carrier opens its end as it starts, so one that has not by then has none.
func Await(wait time.Duration) (ok bool) {
	l := lane.Load()
	if l == nil {
		return false
	}
	left := wait - time.Since(l.started)
	if left <= 0 {
		select {
		case <-l.connected:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(left)
	defer timer.Stop()
	select {
	case <-l.connected:
		return true
	case <-timer.C:
		return false
	}
}

// SetReceiver installs where lane messages from the carrier go and returns
// what removes it again, unless a later receiver has replaced it. The
// function is called off the render path.
func SetReceiver(f func(msg []byte)) (release func()) {
	p := &f
	receiver.Store(p)
	return func() { receiver.CompareAndSwap(p, nil) }
}

// Deliver hands in one lane message that arrived from the carrier. Without a
// receiver it is dropped.
func Deliver(msg []byte) {
	if f := receiver.Load(); f != nil {
		(*f)(msg)
	}
}

// Traced returns opts with every played report logged on logger when the
// [lanesink.Trace] variable asks for it, and opts unchanged otherwise. The
// lines are what the audio lane's trial reads.
func Traced(opts lanesink.Options, logger zerolog.Logger) (out lanesink.Options) {
	if !lanesink.Trace.Get() {
		return opts
	}
	opts.OnReport = func(r lanesink.Report) {
		logger.Info().
			Uint32("stream", r.Stream).Uint32("epoch", r.Epoch).Uint32("deviceRate", r.DeviceRate).
			Int64("reported", r.Reported).Int64("estimated", r.Estimated).
			Int64("sent", r.Sent).Int64("target", r.Target).Bool("playing", r.Playing).
			Msg("audio lane report")
	}
	return opts
}

// NewLaneDevice returns an audio lane device on this process's lane to its
// client and makes it the receiver. A message the device refuses is passed
// to onRefused, which may be nil. close closes the device and stops it being
// the receiver.
func NewLaneDevice(opts lanesink.Options, onRefused func(err error)) (dev *lanesink.Device, close func()) {
	dev = lanesink.NewDevice(Send, opts)
	release := SetReceiver(func(msg []byte) {
		if err := dev.Receive(msg); err != nil && onRefused != nil {
			onRefused(err)
		}
	})
	return dev, func() {
		release()
		dev.Close()
	}
}
