package ea

import "io"

type SizeMeasureWriter struct {
	Size uint64
}

func (inst *SizeMeasureWriter) Write(p []byte) (n int, err error) {
	l := len(p)
	inst.Size += uint64(l)
	return l, nil
}

func (inst *SizeMeasureWriter) Reset() {
	inst.Size = 0
}

var _ io.Writer = &SizeMeasureWriter{}
