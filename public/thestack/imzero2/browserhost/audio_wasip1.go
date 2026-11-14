//go:build wasip1

package browserhost

import "unsafe"

// The tab's audio lane (ADR-0372 §SD4). A worker has no audio
// device; the page around it has. Lane messages cross to the worker through
// two host imports, as the tab's HTTP does, and the worker posts them to the
// page on the port the mesh already uses. The Rust browser host is not
// involved.
//
// Toward the page a message is handed over at once. From the page the worker
// queues what arrives and calls the audio_pump export, which is the one
// place the module reads them: goroutines run only inside an export, and a
// frame may be a long time away.

//go:wasmimport env audio_send
func hostAudioSend(msg unsafe.Pointer, n uint32)

// audio_recv copies the next queued message into dst and returns its length,
// 0 when none is queued. A message longer than capacity is dropped.
//
//go:wasmimport env audio_recv
func hostAudioRecv(dst unsafe.Pointer, capacity uint32) uint32

// audioReceive is where audio_pump delivers; nil until AudioLane is called.
var audioReceive func(msg []byte)

// AudioLane wires the tab's audio lane: receive is given each message the
// page sends, on the goroutine of the audio_pump export, and send carries
// one toward the page. Neither carries the wire prefix. ok is false where
// there is no page — a native run of the tab binary.
func AudioLane(receive func(msg []byte)) (send func(msg []byte), ok bool) {
	audioReceive = receive
	return func(msg []byte) {
		if len(msg) > 0 {
			hostAudioSend(unsafe.Pointer(&msg[0]), uint32(len(msg)))
		}
	}, true
}

// audioPump delivers the lane messages the page sent since the last call.
// It runs no frame: the worker calls it when the page reported, between
// frames, so sound does not wait for the next pass. Returns the number of
// messages delivered.
//
//go:wasmexport audio_pump
func audioPump() int32 {
	beginExport()
	defer endExport()
	// What a device sends is a few integers; the buffer is generous.
	var buf [256]byte
	var n int32
	for {
		k := hostAudioRecv(unsafe.Pointer(&buf[0]), uint32(len(buf)))
		if k == 0 {
			return n
		}
		if audioReceive != nil {
			audioReceive(buf[:k])
		}
		n++
	}
}
