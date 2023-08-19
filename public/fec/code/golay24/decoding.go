package golay24

func DecodeSingle(v uint32) (corrected uint16) {
	v1 := uint16((v >> 12) & 0xfff)
	v2 := uint16(v & 0xfff)
	s2 := syndromTable[v2] ^ v1
	return v1 ^ correctTable[s2]
}

func NumberOfBitErrors(v uint32) (nBitsWrong uint8) {
	v1 := uint16((v >> 12) & 0xfff)
	v2 := uint16(v & 0xfff)
	s2 := syndromTable[v2] ^ v1
	return errorTable[s2]
}

func Syndrome(erro uint32) uint16 {
	v1 := uint16((erro >> 12) & 0xfff)
	v2 := uint16(erro & 0xfff)
	s2 := syndromTable[v2] ^ v1
	return s2
}
