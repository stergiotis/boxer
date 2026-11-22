package decode

import (
	"errors"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The seam's refusals (ADR-0398 §SD8), for errors.Is. A registry returns
// the first two; a reader returns an error wrapping one of the next three
// for every file it does not read, built with [Unsupported], [OverLimit] or
// [Malformed], and [NoEntry] for an entry id the file does not have. The
// message of such an error is a sentence a person can act on: show it as it
// stands.
var (
	// ErrUnknownFormat: no reader of the registry claims the file.
	ErrUnknownFormat = errors.New("no reader claims the file")
	// ErrAmbiguous: more than one reader of the registry claims the file;
	// none is chosen.
	ErrAmbiguous = errors.New("more than one reader claims the file")
	// ErrUnsupported: the file uses something its reader does not read — a
	// compression, a sample type, a projection — or holds no image it reads.
	ErrUnsupported = errors.New("this reader does not read it")
	// ErrLimit: the file is over a limit of the request, or over one the
	// reader keeps for itself; refused from the header, before samples
	// are read.
	ErrLimit = errors.New("over a decode limit")
	// ErrMalformed: the file does not follow its format — cut short, a
	// length that points outside it, a value the format does not define.
	ErrMalformed = errors.New("the file is malformed")
	// ErrNoEntry: the request names an entry the file does not have.
	ErrNoEntry = errors.New("the file holds no such entry")
)

// refusal is an error whose message is a whole sentence naming what was
// refused, and which of the seam's refusals it is. The sentence is built by
// concatenation, not by a format string, so the value it names stays in
// the message an app shows (CODINGSTANDARDS, "When a value has to stay in
// the message": it crosses a text-only boundary). what is the sentence
// without the kind's closing words, which [ReasonOf] returns.
type refusal struct {
	msg   string
	what  string
	kind  error
	cause error
}

func (inst *refusal) Error() string { return inst.msg }

func (inst *refusal) Unwrap() []error {
	if inst.cause == nil {
		return []error{inst.kind}
	}
	return []error{inst.kind, inst.cause}
}

// Refuse is the seam's refusal of the given kind, what naming the feature,
// the size and its limit, or the defect: the sentence "what: " and the
// kind's own words, wrapping the kind's sentinel. [Unsupported],
// [OverLimit] and [Malformed] spell it for one kind each; Refuse is for a
// kind held as a value, as an [Entry] holds its Refusal. A kind of zero, or
// one outside [AllRefusals], is taken as [RefusalUnsupported].
func Refuse(kind RefusalE, what string) error {
	sentinel := kind.Sentinel()
	if sentinel == nil {
		kind, sentinel = RefusalUnsupported, ErrUnsupported
	}
	return eb.Build().Str(factKey(kind), what).Errorf("%w", &refusal{msg: what + ": " + sentinel.Error(), what: what, kind: sentinel})
}

// factKey is the structured field a refusal of kind records its what
// under.
func factKey(kind RefusalE) string {
	switch kind {
	case RefusalLimit:
		return "limit"
	case RefusalMalformed:
		return "defect"
	case RefusalNoEntry:
		return "entry"
	}
	return "feature"
}

// Unsupported is the refusal of a file that uses what its reader does not
// read, what naming it: Unsupported("compression 7 (JPEG)") reads
// "compression 7 (JPEG): this reader does not read it".
func Unsupported(what string) error { return Refuse(RefusalUnsupported, what) }

// OverLimit is the refusal of a file over a limit, what naming the size
// and the limit: OverLimit("a plane of 9000 × 9000 pixels (MaxPixels is
// 67108864)"). [Limits.Check] builds it for the three limits of a request;
// a reader uses it directly for a bound of its own.
func OverLimit(what string) error { return Refuse(RefusalLimit, what) }

// Malformed is the refusal of a file that does not follow its format, what
// naming the defect: Malformed("the directory at 1200 is cut short") reads
// "the directory at 1200 is cut short: the file is malformed".
func Malformed(what string) error { return Refuse(RefusalMalformed, what) }

// MalformedWith is [Malformed] for a defect another decoder found: cause's
// message joins the sentence, and errors.Is matches cause as well as
// [ErrMalformed]. MalformedWith("strip 3", err) reads "strip 3: flate:
// corrupt input before offset 5: the file is malformed".
func MalformedWith(what string, cause error) error {
	if cause == nil {
		return Malformed(what)
	}
	full := what + ": " + cause.Error()
	return eb.Build().Str("defect", what).Errorf("%w", &refusal{msg: full + ": " + ErrMalformed.Error(), what: full,
		kind: ErrMalformed, cause: cause})
}

// ReasonOf is the bare words of the seam refusal err wraps — what it was
// built with, without the kind's closing words — and which refusal it is:
// ReasonOf(Unsupported("compression 7 (JPEG)")) is "compression 7 (JPEG)"
// and [RefusalUnsupported]. It is what an [Entry] lists as its Reason and
// Refusal ([Entry.Refused] fills both). For a refusal of a registry or
// [NoEntry], whose sentence has no closing words, it is the whole
// sentence. An error that wraps no refusal of the seam is "" and zero.
func ReasonOf(err error) (what string, kind RefusalE) {
	var r *refusal
	if !errors.As(err, &r) {
		return "", 0
	}
	for _, k := range AllRefusals {
		if k.Sentinel() == r.kind {
			return r.what, k
		}
	}
	return r.what, 0
}

// NoEntry is the refusal of a request for an entry id the file does not
// have.
func NoEntry(id string) error {
	msg := "the file holds no entry " + strconv.Quote(id)
	return eb.Build().Str("entry", id).Errorf("%w", &refusal{msg: msg, what: msg, kind: ErrNoEntry})
}

// RefusalE says which of the seam's refusals an error is. The zero value is
// none: an error that is no refusal — a failed read, a cancel.
type RefusalE uint8

const (
	RefusalUnknownFormat RefusalE = iota + 1
	RefusalAmbiguous
	RefusalNoEntry
	RefusalLimit
	RefusalUnsupported
	RefusalMalformed
)

// AllRefusals lists the refusals, in the order [RefusalOf] tries them.
var AllRefusals = []RefusalE{RefusalUnknownFormat, RefusalAmbiguous, RefusalNoEntry, RefusalLimit, RefusalUnsupported, RefusalMalformed}

func (inst RefusalE) String() string {
	switch inst {
	case RefusalUnknownFormat:
		return "unknown-format"
	case RefusalAmbiguous:
		return "ambiguous"
	case RefusalNoEntry:
		return "no-entry"
	case RefusalLimit:
		return "limit"
	case RefusalUnsupported:
		return "unsupported"
	case RefusalMalformed:
		return "malformed"
	}
	return "none"
}

// Sentinel is the error errors.Is matches for the refusal; nil for none.
func (inst RefusalE) Sentinel() error {
	switch inst {
	case RefusalUnknownFormat:
		return ErrUnknownFormat
	case RefusalAmbiguous:
		return ErrAmbiguous
	case RefusalNoEntry:
		return ErrNoEntry
	case RefusalLimit:
		return ErrLimit
	case RefusalUnsupported:
		return ErrUnsupported
	case RefusalMalformed:
		return ErrMalformed
	}
	return nil
}

// RefusalOf is which refusal err is: the first of [AllRefusals] it wraps,
// or zero when it wraps none.
func RefusalOf(err error) RefusalE {
	if err == nil {
		return 0
	}
	for _, r := range AllRefusals {
		if errors.Is(err, r.Sentinel()) {
			return r
		}
	}
	return 0
}
