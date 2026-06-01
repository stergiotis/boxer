package imztop

import "github.com/stergiotis/boxer/public/observability/slidingwindow"

// SlidingWindow aliases the shared observability sliding-window buffer, lifted
// out of imztop + imzrt per ADR-0061 SD13. See [slidingwindow.Window] for
// semantics (memmove-on-full, stable backing, per-tick copy-out; not safe for
// concurrent use).
type SlidingWindow[T any] = slidingwindow.Window[T]

// NewSlidingWindow constructs a SlidingWindow holding at most capacity values
// (clamped to a minimum of 1).
func NewSlidingWindow[T any](capacity int32) *SlidingWindow[T] {
	return slidingwindow.New[T](capacity)
}
