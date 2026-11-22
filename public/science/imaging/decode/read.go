package decode

import (
	"context"
	"errors"
	"io"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// A file that ends before what its reader reads is malformed; a read that
// fails for another reason — a disk, a network, a closed source — says
// nothing of the file, and is no refusal (ADR-0398 §SD8). A reader keeps
// the two apart by reading through [ReadFullAt], and, where it reads a
// stream — a section of the file, a decompressor over it — by reading from
// [MarkReadFailures] of the file and passing what the stream returns to
// [ReadError].

// readFailure is a read of the file that failed for a reason other than
// the file's end: the io.ReaderAt's own error, marked so that it is told
// apart from what a stream over it reports of the bytes.
type readFailure struct{ err error }

func (inst *readFailure) Error() string { return "a read of the file failed: " + inst.err.Error() }

func (inst *readFailure) Unwrap() error { return inst.err }

// isEnd reports the end of the file, as io.ReaderAt and io.ReadFull say it.
func isEnd(err error) bool { return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) }

// marked is err marked as a failed read of the file, unless it is the
// file's end or marked already.
func marked(err error) error {
	var rf *readFailure
	if err == nil || isEnd(err) || errors.As(err, &rf) {
		return err
	}
	return &readFailure{err: err}
}

// errBeforeStart is a read at a negative offset: an offset computed from
// the file's own numbers, so a defect of the file and never a failed read.
var errBeforeStart = errors.New("a read before the start of the file")

type markedReader struct{ r io.ReaderAt }

func (inst markedReader) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, errBeforeStart
	}
	n, err = inst.r.ReadAt(p, off)
	return n, marked(err)
}

// MarkReadFailures is r with every error of a read marked as a failed read
// of the file, but the file's end: io.EOF passes as it is, and a read at a
// negative offset, which only a number taken from the file can ask for,
// fails unmarked, a defect. The mark
// survives a decompressor or any other stream read over r, which hands its
// source's error on, so that [ReadError] can tell a read that failed from
// a defect the stream found in the bytes. A reader that reads the file
// through a stream wraps the io.ReaderAt it was given once, first.
func MarkReadFailures(r io.ReaderAt) io.ReaderAt {
	if m, ok := r.(markedReader); ok {
		return m
	}
	return markedReader{r: r}
}

// ReadFullAt reads len(p) bytes of the file at off, what naming them for a
// person ("the directory at 1200"). A read the file ends before is the
// refusal Malformed(what + " cut short"), and one at a negative offset is
// malformed too; a read that fails otherwise is
// returned wrapped, no refusal, so that errors.Is finds its cause and
// [RefusalOf] is none: an app must not call a file malformed because a
// disk failed.
func ReadFullAt(r io.ReaderAt, p []byte, off int64, what string) error {
	if off < 0 {
		return Malformed(what + " lies before the start of the file")
	}
	n, err := r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return ReadError(what, marked(err))
}

// ReadError is what a reader returns for a failed read of what from a
// stream over the file — io.ReadFull over a section of it, or over a
// decompressor reading [MarkReadFailures] of it. It is nil for nil; err
// itself for a refusal of the seam or a context's error; a failed read of
// the file, wrapped and no refusal; Malformed(what + " cut short") for the
// stream's end; and anything else the stream reports — a corrupt block, a
// checksum, an invalid code — is a defect of the bytes,
// MalformedWith(what, err). A stream over a reader that is not marked
// reports a failed read as a defect, which the conformance kit's io-error
// check finds.
func ReadError(what string, err error) error {
	var rf *readFailure
	switch {
	case err == nil:
		return nil
	case RefusalOf(err) != 0:
		return err
	case errors.As(err, &rf):
		return eb.Build().Str("read", what).Errorf("%w", err)
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return err
	case isEnd(err):
		return Malformed(what + " cut short")
	}
	return MalformedWith(what, err)
}
