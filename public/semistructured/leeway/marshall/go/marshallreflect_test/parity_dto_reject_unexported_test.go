package marshallreflect_test

// parityRejectUnexported: an unexported tagged field — BOTH front-ends must
// reject at plan-build. Reflect cannot read or set the field; a codegen
// accept would compile in-package while silently diverging from reflect's
// accept set (ADR-0113 review fallout).
type parityRejectUnexported struct {
	_      struct{} `kind:"parityRejectUnexported"`
	ID     uint64   `lw:",id"`
	status string   `lw:"status,symbol"`
}
