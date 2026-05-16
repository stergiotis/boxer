package logical

type TristateE int8

const (
	TristateFalse TristateE = -1
	TristateNil   TristateE = 0
	TristateTrue  TristateE = 1
)

func (inst TristateE) IsFalse() bool {
	return inst == TristateFalse
}
func (inst TristateE) IsTrue() bool {
	return inst == TristateTrue
}
func (inst TristateE) IsNil() bool {
	return inst == TristateNil
}

func TristateFromBool(b bool) TristateE {
	if b {
		return TristateTrue
	}
	return TristateFalse
}
