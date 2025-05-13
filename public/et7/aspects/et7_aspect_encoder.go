package aspects

import (
	"iter"
	"math/big"
	"math/bits"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const EmptyAspectSet = EncodedEt7AspectSet("0")

func countEncodedAspect(num uint64) (n int) {
	n = bits.OnesCount64(num)
	return
}
func maxEncodedAspect(num uint64) (maxEncoded DataAspectE) {
	maxEncoded = DataAspectE(64 - bits.LeadingZeros64(num) - 1)
	return
}
func decode(encoded EncodedEt7AspectSet) (num uint64, valid bool) {
	var dec big.Int
	_, valid = dec.SetString(string(encoded), 62)
	if !valid {
		return
	}
	num = dec.Uint64()
	valid = num == 0 || maxEncodedAspect(num).IsValid()
	return
}

func (inst EncodedEt7AspectSet) String() string {
	return string(inst)
}

func (inst EncodedEt7AspectSet) IsValid() bool {
	if inst == "" {
		return false
	}
	_, valid := decode(inst)
	return valid
}

var ErrInvalidEncoding = eh.Errorf("encoding is wrong")
var ErrEmptySet = eh.Errorf("encoding contains empty set")

func NewCanonicalEt7AspectCoder() *CanonicalEt7AspectCoder {
	return &CanonicalEt7AspectCoder{}
}
func (inst *CanonicalEt7AspectCoder) Encode(aspects ...DataAspectE) (encoded EncodedEt7AspectSet, err error) {
	var t uint64
	for i, a := range aspects {
		if !a.IsValid() {
			err = eb.Build().Uint8("aspect", uint8(a)).Int("index", i).Errorf("found invalid aspect in supplied arguments")
			return
		}
		t |= uint64(1) << a
	}
	var enc big.Int
	enc.SetUint64(t)
	encoded = EncodedEt7AspectSet(enc.Text(62))
	return
}
func (inst *CanonicalEt7AspectCoder) IsEmpty(encoded EncodedEt7AspectSet) bool {
	num, valid := decode(encoded)
	return valid && num == 0
}
func (inst *CanonicalEt7AspectCoder) MaxEncodedAspect(encoded EncodedEt7AspectSet) (aspect DataAspectE, err error) {
	num, valid := decode(encoded)
	if !valid {
		err = ErrInvalidEncoding
		return
	}
	if num == 0 {
		err = ErrEmptySet
		return
	}
	aspect = maxEncodedAspect(num)
	return
}
func (inst *CanonicalEt7AspectCoder) CountEncodedAspects(encoded EncodedEt7AspectSet) (n int, err error) {
	num, valid := decode(encoded)
	if !valid {
		err = ErrInvalidEncoding
		return
	}
	n = countEncodedAspect(num)
	return
}
func (inst *CanonicalEt7AspectCoder) IterateAspects(encoded EncodedEt7AspectSet) iter.Seq2[int, DataAspectE] {
	num, valid := decode(encoded)
	if !valid {
		return nil
	}
	return func(yield func(int, DataAspectE) bool) {
		j := 0
		for i := uint8(0); i < uint8(MaxDataAspectExcl); i++ {
			if num&(uint64(1)<<i) != 0 {
				if !yield(j, DataAspectE(i)) {
					return
				}
				j++
			}
		}
	}
}
