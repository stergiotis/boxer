package ea

type TeeByteReader struct {
	r ByteReadReader
	w ByteBlockWriter
}

func (t *TeeByteReader) ReadByte() (b byte, err error) {
	b, err = t.r.ReadByte()
	if err != nil {
		return
	}
	err = t.w.WriteByte(b)
	return
}

func (t *TeeByteReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if err != nil {
		return
	}
	if n > 0 {
		n, err = t.w.Write(p)
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
