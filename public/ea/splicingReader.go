package ea

import (
	"io"

	"github.com/stergiotis/boxer/public/observability/eh"
)

type SplicingReader struct {
	primary   io.Reader
	secondary io.Reader
}

func NewSplicingReader(primary io.Reader) *SplicingReader {
	return &SplicingReader{
		primary:   primary,
		secondary: nil,
	}
}

var ErrSplicingReaderAlreadySet = eh.Errorf("splicing reader is already set")

func (inst *SplicingReader) SpliceReader(r io.Reader) (err error) {
	if inst.secondary != nil {
		err = ErrSplicingReaderAlreadySet
		return
	}
	inst.secondary = r
	return
}

func (inst *SplicingReader) IsSpliceReaderSet() bool {
	return inst.secondary != nil
}

func (inst *SplicingReader) Reset() {
	inst.secondary = nil
}

func (inst *SplicingReader) Read(p []byte) (n int, err error) {
	sec := inst.secondary
	if sec != nil {
		n, err = sec.Read(p)
		if err == nil {
			return
		} else if err == io.EOF {
			inst.secondary = nil
			if n == 0 {
				return inst.primary.Read(p)
			} else {
				err = nil
				return
			}
		} else {
			return
		}
	}
	return inst.primary.Read(p)
}

var _ io.Reader = (*SplicingReader)(nil)
