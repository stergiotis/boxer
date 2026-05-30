package imzrt

import "github.com/stergiotis/boxer/public/observability/slidingwindow"

// SlidingWindow aliases the shared observability sliding-window buffer. It was
// formerly a verbatim copy of imztop's type; ADR-0061 SD13 (open question 3)
// tracked lifting it into a shared package, done here. See
// [slidingwindow.Window] for semantics (memmove-on-full, stable backing,
// per-tick copy-out; not safe for concurrent use).
type SlidingWindow[T any] = slidingwindow.Window[T]

// NewSlidingWindow constructs a SlidingWindow holding at most capacity values
// (clamped to a minimum of 1).
func NewSlidingWindow[T any](capacity int32) *SlidingWindow[T] {
	return slidingwindow.New[T](capacity)
}
