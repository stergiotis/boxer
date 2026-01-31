package ea

import "io"

type AnonymousCloseWriter struct {
	f func() error
	w func(p []byte) (n int, err error)
}

func (inst *AnonymousCloseWriter) Write(p []byte) (n int, err error) {
	if inst.w != nil {
		return inst.w(p)
	}
	return
}

func (inst *AnonymousCloseWriter) Close() error {
	if inst.f != nil {
		return inst.f()
	}
	return nil
}

func NewAnonymousCloseWriter(f func() error, w func(p []byte) (n int, err error)) *AnonymousCloseWriter {
	return &AnonymousCloseWriter{
		f: f,
		w: w,
	}
}

var _ io.Closer = (*AnonymousCloseWriter)(nil)
var _ io.Writer = (*AnonymousCloseWriter)(nil)

