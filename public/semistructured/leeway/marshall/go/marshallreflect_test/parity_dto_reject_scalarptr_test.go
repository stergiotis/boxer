package marshallreflect_test

// parityRejectScalarPtr: a non-roaring pointer-typed scalar — BOTH front-ends
// reject with the same message (option.Option[T] is the ZeroToOne spelling).
type parityRejectScalarPtr struct {
	_  struct{} `kind:"parityRejectScalarPtr"`
	ID uint64   `lw:",id"`
	N  *uint64  `lw:"n,u64Array"`
}
