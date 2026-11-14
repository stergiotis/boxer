// Package hostaudio joins the two ends of the audio lane inside the Go host
// (ADR-0372, proposed, §SD4): the device that plays an app's sound through a
// remote viewer, and the application that launched the imzero2 client and
// holds the pipes to its carrier.
//
// Neither end knows the other, and either may come first. The application
// installs how a message leaves ([SetSender]) and hands in what arrives
// ([Deliver]); the device sends ([Send]) and says where arrivals go
// ([SetReceiver]). Until both are there a message goes nowhere, which the
// device reads as nobody listening.
package hostaudio

import (
	"sync/atomic"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/science/audio/sink/lanesink"
)

var (
	sender   atomic.Pointer[func(msg []byte)]
	receiver atomic.Pointer[func(msg []byte)]
)

// SetSender installs how a lane message leaves toward the client's carrier;
// nil removes it. The function must not wait and must not keep msg.
func SetSender(f func(msg []byte)) {
	if f == nil {
		sender.Store(nil)
		return
	}
	sender.Store(&f)
}

// Send carries one lane message, without its wire prefix, toward the
// carrier. Without a sender it is dropped.
func Send(msg []byte) {
	if f := sender.Load(); f != nil {
		(*f)(msg)
	}
}

// SetReceiver installs where lane messages from the carrier go; nil removes
// it. The function is called off the render path.
func SetReceiver(f func(msg []byte)) {
	if f == nil {
		receiver.Store(nil)
		return
	}
	receiver.Store(&f)
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
// to onRefused, which may be nil.
func NewLaneDevice(opts lanesink.Options, onRefused func(err error)) (dev *lanesink.Device) {
	dev = lanesink.NewDevice(Send, opts)
	SetReceiver(func(msg []byte) {
		if err := dev.Receive(msg); err != nil && onRefused != nil {
			onRefused(err)
		}
	})
	return dev
}
