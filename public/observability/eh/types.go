package eh

import "github.com/pkg/errors"

type stack []uintptr

type stackTracer interface {
	StackTrace() errors.StackTrace
}
type unwrapableSingle interface {
	error
	Unwrap() error
}
type unwrapableMulti interface {
	error
	Unwrap() []error
}
type ErrorWithStructuredData interface {
	error
	SetCBORStructuredData(p []byte)
	GetCBORStructuredData() []byte
}
