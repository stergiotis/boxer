package marshallreflect_test

// parityRejectUntagged: a field with no tag at all — BOTH front-ends must
// reject. The error texts differ by construction: the AST side distinguishes
// a missing tag literal from a tag without an `lw:` key; reflect cannot see
// the difference.
type parityRejectUntagged struct {
	_    struct{} `kind:"parityRejectUntagged"`
	ID   uint64   `lw:",id"`
	Note string
}
