package inprocbus

import "errors"

// ErrPermissionViolation is returned when a Publish, Subscribe, or Request
// targets a subject not covered by the client's declared SubjectFilter
// caps. Shaped to match nats.ErrPermissionViolation for forward-compat with
// the M4 NATS transport.
var ErrPermissionViolation = errors.New("permission violation")

// ErrTimeout is returned when a Request waits longer than the configured
// timeout without receiving a reply.
var ErrTimeout = errors.New("request timeout")

// ErrClosed is returned by every operation on a Client after Close (ADR-0188
// §SD1). The host closes an app instance's client once its window has been
// reaped, so a goroutine that outlives Unmount and still holds the client
// fails loudly here instead of acting under the instance's authority.
// Shaped to match nats.ErrConnectionClosed in role.
var ErrClosed = errors.New("client closed")
