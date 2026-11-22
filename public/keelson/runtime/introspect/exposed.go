package introspect

import "io"

// An exposed dataset (ADR-0407, proposed) is one a person, or a task under its
// grant, made readable by programs on this machine: the loopback listener
// that serves this registry answers a request naming the dataset's alias
// and presenting the exposure's token. The registry carries the seam
// between the two, so neither the listener nor the dataset service has to
// know the other: the service sets itself as the source, the listener sets
// the base URL it serves the registry at.

// ExposedStatusE is what a source answers a request for an exposed dataset.
type ExposedStatusE uint8

const (
	// ExposedNotFound covers every request that does not open a dataset: an
	// unknown alias, a missing or wrong token, a lapsed or revoked
	// exposure. A listener answers them alike, so the answer confirms no
	// token ever existed.
	ExposedNotFound ExposedStatusE = iota
	// ExposedOpen is a request that opened the dataset.
	ExposedOpen
	// ExposedBusy is a request presenting a valid token while the
	// exposure already has as many reads open as it admits.
	ExposedBusy
)

// ExposedFormatE is the format a request for an exposed dataset asks for.
type ExposedFormatE uint8

const (
	// ExposedFormatArrowStream is the dataset's Arrow IPC stream as sealed.
	ExposedFormatArrowStream ExposedFormatE = iota
	// ExposedFormatCSV is the dataset as CSV with a header line.
	ExposedFormatCSV
)

func (inst ExposedFormatE) String() (s string) {
	if inst == ExposedFormatCSV {
		return "csv"
	}
	return "arrowstream"
}

// ExposedRequest is what a listener tells a source about one request. Remote
// and UserAgent are what the request declared about itself; they identify
// no one.
type ExposedRequest struct {
	Head      bool
	Format    ExposedFormatE
	Range     string
	Remote    string
	UserAgent string
}

// ExposedRead is an exposed dataset opened for one request: its plaintext
// Arrow IPC stream, the revision the stream belongs to, and Done, which
// the listener calls once with the bytes it sent and the error the request
// ended with, if any. Done closes Reader.
type ExposedRead struct {
	Reader   io.ReadSeekCloser
	Revision uint64
	Done     func(sent uint64, err error)
}

// ExposureSourceI opens exposed datasets for a listener. A source must not
// record or log the token.
type ExposureSourceI interface {
	OpenExposed(alias string, token string, req ExposedRequest) (rd ExposedRead, status ExposedStatusE)
}

// SetExposureSource makes src the source of the datasets exposed through
// this registry; nil clears it.
func (inst *Registry) SetExposureSource(src ExposureSourceI) {
	if src == nil {
		inst.exposures.Store(nil)
		return
	}
	inst.exposures.Store(&src)
}

// ExposureSource returns the source of exposed datasets, ok false when none
// is set.
func (inst *Registry) ExposureSource() (src ExposureSourceI, ok bool) {
	if p := inst.exposures.Load(); p != nil {
		return *p, true
	}
	return
}

// SetServedAt records the http:// base URL of the loopback listener serving
// this registry; empty clears it.
func (inst *Registry) SetServedAt(baseURL string) {
	if baseURL == "" {
		inst.servedAt.Store(nil)
		return
	}
	inst.servedAt.Store(&baseURL)
}

// ClearServedAt clears the base URL if it is still baseURL, so a listener
// that stops does not clear one that started after it.
func (inst *Registry) ClearServedAt(baseURL string) {
	if p := inst.servedAt.Load(); p != nil && *p == baseURL {
		inst.servedAt.CompareAndSwap(p, nil)
	}
}

// ServedAt is the base URL of the loopback listener serving this registry,
// "" when none serves it.
func (inst *Registry) ServedAt() (baseURL string) {
	if p := inst.servedAt.Load(); p != nil {
		baseURL = *p
	}
	return
}
