package audit

import (
	"context"
	"time"
)

// Origin is who wrote an event as the row records it: the run, the app
// and the app instance (ADR-0277 §SD1). A reader weighs it by the event's
// OriginBy.
type Origin struct {
	Run      string
	App      string
	Instance uint64
}

// Event is one audit event as a forwarder is handed it: the row's id and
// time, its origin, and the event itself. It is a plain value, so a
// carrier adapter needs no store to take it.
type Event struct {
	Id     uint64
	At     time.Time
	Origin Origin
	Row    AuditEvent
}

// ForwarderI is the seam a downstream carrier adapter implements (ADR-0296
// §SD6): it is handed audit events that are durable on the server, never
// buffered ones. Delivery is at least once — a prompt path and a backstop
// can both hand over the same event — and the receiving side deduplicates
// by the event's id. An error is counted and logged by the caller; the
// backstop is what sends the events again.
type ForwarderI interface {
	Forward(ctx context.Context, events []Event) error
}
