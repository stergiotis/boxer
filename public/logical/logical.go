package logical

type Tristate int8

const (
	TriFalse Tristate = -1
	TriNil   Tristate = 0
	TriTrue  Tristate = 1
)
