// Package planebus holds the payload-independent half of a one-way
// publish/subscribe data plane: the wire codec seam, a Consumer that decodes
// each message and hands it on, a Bridge that relays a subject between two
// buses, and a LatestHolder that keeps the newest value per subject key.
//
// A plane instantiates these over its own payload type and keeps what is
// specific to it — its subjects, its producer and how that producer schedules
// sampling — in its own package. All of it speaks app.BusI, so the same code
// runs over inprocbus and over NATS core.
//
// The dataflow is strictly one way: nothing here replies to a publisher. A
// plane that needs control uses a separate request/reply subject family.
//
// Handlers run on whatever goroutine the bus dispatches on. Under inprocbus
// that is the publisher's goroutine, so a handler that is slow — decoding a
// large payload included — delays the publisher; such a consumer hands the
// work off.
package planebus
