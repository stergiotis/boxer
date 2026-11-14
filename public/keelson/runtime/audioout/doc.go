// Package audioout is the runtime's audio-output capability (ADR-0372,
// proposed, §SD1 and §SD2): an app asks the host for an output over a
// source and is handed a
// [github.com/stergiotis/boxer/public/science/audio/sink.SinkI]; the host
// binary decides what device is behind it.
//
// The request and the samples take different routes. [Open] makes a bus
// request on [SubjectOpen], which is what the bus's capability check and its
// audit see, so an app that did not declare the subject in its manifest is
// refused there. The reply carries a one-use token; [Open] redeems it in
// process for a sink on the service's [DeviceI]. Samples never travel on the
// bus.
//
// [Open] always returns a sink. Where the request is refused, nothing serves
// the subject, or the device does not open, it is the device-less
// [github.com/stergiotis/boxer/public/science/audio/sink.Null] together with
// the reason, so a player still shows a moving playhead and says why nothing
// is heard (ADR-0208 §SD6).
//
// # Limits
//
// A token is redeemable only in the process that runs the [Service]. A grant
// that arrives from a service in another process — a bus that is not the
// in-process one — cannot be redeemed, and [Open] returns the null sink with
// that reason; how a granted stream's samples cross such a boundary is
// deferred by the ADR.
package audioout
