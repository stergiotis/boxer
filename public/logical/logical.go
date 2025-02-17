package logical

type Tristate int8

const (
	TriFalse Tristate = -1
	TriNil   Tristate = 0
	TriTrue  Tristate = 1
)

func (inst Tristate) IsFalse() bool {
	return inst == TriFalse
}
func (inst Tristate) IsTrue() bool {
	return inst == TriTrue
}
func (inst Tristate) IsNil() bool {
	return inst == TriNil
}

func TriFromBool(b bool) Tristate {
	if b {
		return TriTrue
	}
	return TriFalse
}
