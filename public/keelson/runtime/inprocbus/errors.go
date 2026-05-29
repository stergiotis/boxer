//go:build llm_generated_opus47

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
