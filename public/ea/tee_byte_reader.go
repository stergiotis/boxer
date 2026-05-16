package ea

type TeeByteReader struct {
	r ByteReadReader
	w ByteBlockWriter
}

func (inst *TeeByteReader) ReadByte() (b byte, err error) {
	b, err = inst.r.ReadByte()
	if err != nil {
		return
	}
	err = inst.w.WriteByte(b)
	return
}

func (inst *TeeByteReader) Read(p []byte) (n int, err error) {
	n, err = inst.r.Read(p)
	if err != nil {
		return
	}
	if n > 0 {
		n, err = inst.w.Write(p)
	}
	return
}

var _ ByteReadReader = (*TeeByteReader)(nil)

func NewTeeByteReader(r ByteReadReader, w ByteBlockWriter) *TeeByteReader {
	return &TeeByteReader{
		r: r,
		w: w,
	}
}
