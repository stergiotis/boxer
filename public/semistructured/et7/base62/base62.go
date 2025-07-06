package base62

import "math/big"

type Base62Num string

func (inst Base62Num) Decode() (num uint64, valid bool) {
	return Decode(inst)
}
func IsValid(encoded Base62Num) (valid bool) {
	var dec big.Int
	l := len(encoded)
	if l == 0 || encoded[0] == '-' || (l > 1 && encoded[0] == '0') {
		return
	}
	_, valid = dec.SetString(string(encoded), 62)
	return
}
func Decode(encoded Base62Num) (num uint64, valid bool) {
	var dec big.Int
	l := len(encoded)
	if l == 0 || encoded[0] == '-' || (l > 1 && encoded[0] == '0') {
		return
	}
	_, valid = dec.SetString(string(encoded), 62)
	if !valid {
		return
	}
	num = dec.Uint64()
	return
}
func Encode(num uint64) (n Base62Num) {
	var enc big.Int
	enc.SetUint64(num)
	n = Base62Num(enc.Text(62))
	return
}
