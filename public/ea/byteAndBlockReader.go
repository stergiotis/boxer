package ea

import (
	"bufio"
	"github.com/stergiotis/boxer/public/observability/eh"
	"io"
)

func NewByteBlockReaderDiscardReader(reader interface{}) (ByteBlockDiscardReader, error) {
	{
		probe, ok := reader.(*bufio.Reader)
		if ok {
			return newByteAndBlockReaderBufioReader(probe), nil
		}
	}
	{
		probe, ok := reader.(*bufio.ReadWriter)
		if ok {
			return newByteAndBlockReaderBufioReadWriter(probe), nil
		}
	}
	{
		probe, ok := reader.(ByteReadReader)
		if ok {
			return newByteAndBlockReaderByteReadReader(probe), nil
		}
	}
	return nil, eh.Errorf("unable to create ByteBlockDiscardReader from supplied reader: type=%t", reader)
}

func newByteAndBlockReaderBufioReader(reader *bufio.Reader) ByteBlockDiscardReader {
	return reader
}

func newByteAndBlockReaderBufioReadWriter(reader *bufio.ReadWriter) ByteBlockDiscardReader {
	return reader
}

func newByteAndBlockReaderByteReadReader(reader ByteReadReader) ByteBlockDiscardReader {
	return NewBytesBlockByteReadReader(reader)
}

type BytesBlockByteReadReader struct {
	r   ByteReadReader
	buf []byte
}

var _ ByteBlockDiscardReader = (*BytesBlockByteReadReader)(nil)

const blockSize = 4096

func NewBytesBlockByteReadReader(r ByteReadReader) *BytesBlockByteReadReader {
	return &BytesBlockByteReadReader{
		r:   r,
		buf: make([]byte, 0, blockSize),
	}
}

func (inst *BytesBlockByteReadReader) Discard(n int) (nBytesRead int, err error) {
	buf := inst.buf[:blockSize]
	l := n / blockSize
	r := inst.r
	var u int
	for i := 0; i < l; i++ {
		u, err = io.ReadFull(r, buf)
		nBytesRead += u
		if err != nil {
			return
		}
	}
	n = n - l*blockSize
	if n > 0 {
		buf = buf[:n]
		//u, err = r.Read(buf)
		u, err = io.ReadFull(r, buf)
		nBytesRead += u
		if err != nil {
			return
		}
	}
	return
}

func (inst *BytesBlockByteReadReader) Read(p []byte) (n int, err error) {
	return inst.r.Read(p)
}

func (inst *BytesBlockByteReadReader) ReadByte() (byte, error) {
	return inst.r.ReadByte()
}
