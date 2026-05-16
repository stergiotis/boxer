package ea

import (
	"io"

	"github.com/stergiotis/boxer/public/ea"
)

type MessageReaderI interface {
	ea.ByteBlockDiscardReaderI
	// BytesRead return how many bytes have been consumed from the underlying reader
	BytesRead() int
	// BytesPeeked return how many bytes have been peeked from the underlying reader (in-message)
	BytesPeeked() int
	Discard(nBytes int) (n int, err error)
	MessageAccepted() (err error)
	MessageRejected(reason error) (err error)
	DetectedBitErrors() int
}

type MessageWriterI interface {
	io.ByteWriter
	io.Writer
	BeginMessage() (n int, err error)
	EndMessage() (paddingBitsBeforeEncoding int, err error)
}
