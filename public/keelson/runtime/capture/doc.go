// Package capture is the host's one way to capture windows for a product
// path (ADR-0281): a policy enforcement point in front of the renderer.
//
// A Request names windows and a format. A PolicyI — the policy decision
// point — answers it with a Decision: permit or deny, and the obligations a
// permitted capture carries. The Service enforces them and never decides:
// every obligation needs a handler for the request's format or the capture
// is denied; the handlers run in phase order — scope, transform, encode —
// and the Service holds the bytes until the artifact is written.
//
// The Service renders through a SourceI, the window host, which draws only
// the windows the scope names (ADR-0281 §SD4).
package capture
