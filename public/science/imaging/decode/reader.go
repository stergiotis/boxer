package decode

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// SniffLen is how many of a file's first bytes a [Registry] hands to
// [ReaderI.Sniff]: fewer only when the file is shorter.
const SniffLen = 4096

// DefaultMaxBytes bounds the samples of a whole stack, every plane, at 1 GiB.
const DefaultMaxBytes int64 = 1 << 30

// DefaultMaxPlanes bounds the planes of a stack, whose per-plane
// bookkeeping MaxBytes does not count.
const DefaultMaxPlanes = 1 << 16

// Format is what a reader says about itself (ADR-0398 §SD6). A registry
// reads it once, when the reader is registered.
type Format struct {
	// Name identifies the format in a registry, where it is unique: a
	// lower-case letter, then lower-case letters, digits and '-', at most
	// 32 in all ("tiff", "fits-gz"). It is the Provenance.Format of every
	// stack the reader makes.
	Name string
	// Title names the format for a person: "TIFF, BigTIFF, GeoTIFF". One
	// line.
	Title string
	// Extensions are the endings its files usually carry, lower-case with
	// the dot (".tif", ".fits.gz"): a hint for a file dialog or a help
	// page, never how a file is recognised.
	Extensions []string
	// Version is the version of the reader's own reading, from 1: bumped
	// when the same file would decode to something else — other samples,
	// another placement, other notes — so that what was derived from an
	// earlier reading can be told apart.
	Version int
	// Doc is a sentence or two for a person: what the reader reads and
	// what it refuses.
	Doc string
}

// maxFormatName bounds a format's name.
const maxFormatName = 32

// Validate refuses a format a registry does not take: a name outside the
// syntax of [Format.Name], an empty or multi-line title, an extension that
// is not lower-case with a leading dot or appears twice, or a version
// below 1. Doc may be empty here; the conformance kit asks for it.
func (inst Format) Validate() error {
	if !isFormatName(inst.Name) {
		return eb.Build().Str("name", inst.Name).
			Errorf("a format's name is a lower-case letter, then lower-case letters, digits and '-', at most 32 in all")
	}
	if strings.TrimSpace(inst.Title) == "" || strings.ContainsAny(inst.Title, "\r\n") {
		return eb.Build().Str("name", inst.Name).Errorf("a format needs a title of one line")
	}
	for i, x := range inst.Extensions {
		if !isExtension(x) {
			return eb.Build().Str("name", inst.Name).Str("extension", x).
				Errorf("an extension is lower-case letters, digits, '.', '+' and '-' after a leading dot")
		}
		if slices.Contains(inst.Extensions[:i], x) {
			return eb.Build().Str("name", inst.Name).Str("extension", x).Errorf("an extension is listed twice")
		}
	}
	if inst.Version < 1 {
		return eb.Build().Str("name", inst.Name).Int("version", inst.Version).Errorf("a format's version starts at 1")
	}
	return nil
}

func (inst Format) clone() Format {
	inst.Extensions = slices.Clone(inst.Extensions)
	return inst
}

func isFormatName(s string) bool {
	if s == "" || len(s) > maxFormatName || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func isExtension(s string) bool {
	if len(s) < 2 || s[0] != '.' {
		return false
	}
	for _, c := range []byte(s[1:]) {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '+' || c == '-') {
			return false
		}
	}
	return true
}

// Limits bound what a decode may make (ADR-0398 §SD5). A field of zero or
// less takes its default. A reader checks them from the file's header,
// before it reads a sample — [Limits.Check] does the arithmetic — and
// refuses with an error wrapping [ErrLimit].
type Limits struct {
	// MaxPixels bounds the pixels of one plane; [DefaultMaxPixels].
	MaxPixels int64
	// MaxBytes bounds the samples of the whole stack, every plane, in
	// their sample kind; [DefaultMaxBytes].
	MaxBytes int64
	// MaxPlanes bounds the planes of the stack; [DefaultMaxPlanes].
	MaxPlanes int
}

// Resolved is the limits with every field of zero or less replaced by its
// default.
func (inst Limits) Resolved() Limits {
	if inst.MaxPixels <= 0 {
		inst.MaxPixels = DefaultMaxPixels
	}
	if inst.MaxBytes <= 0 {
		inst.MaxBytes = DefaultMaxBytes
	}
	if inst.MaxPlanes <= 0 {
		inst.MaxPlanes = DefaultMaxPlanes
	}
	return inst
}

// Check refuses a stack of planes width × height, planes of them, in kind,
// that is over a limit, naming which: the pixels of a plane, then the
// number of planes, then the bytes of the samples. The arithmetic cannot
// overflow, so the header's own numbers may be passed as they stand; a
// negative one is malformed.
func (inst Limits) Check(width, height, planes int64, kind imaging.SampleKindE) error {
	l := inst.Resolved()
	if width < 0 || height < 0 || planes < 0 {
		return Malformed("a negative size (" + strconv.FormatInt(width, 10) + " × " + strconv.FormatInt(height, 10) +
			", " + strconv.FormatInt(planes, 10) + " planes)")
	}
	size := strconv.FormatInt(width, 10) + " × " + strconv.FormatInt(height, 10)
	if width > 0 && height > l.MaxPixels/width {
		return OverLimit("a plane of " + size + " pixels (MaxPixels is " + strconv.FormatInt(l.MaxPixels, 10) + ")")
	}
	if planes > int64(l.MaxPlanes) {
		return OverLimit(strconv.FormatInt(planes, 10) + " planes (MaxPlanes is " + strconv.Itoa(l.MaxPlanes) + ")")
	}
	px, sampleBytes := width*height, int64(max(kind.Bytes(), 1))
	if px > 0 && planes > 0 && (px > l.MaxBytes/sampleBytes || planes > l.MaxBytes/(px*sampleBytes)) {
		return OverLimit(strconv.FormatInt(planes, 10) + " planes of " + size + " " + kind.String() + " samples (MaxBytes is " +
			strconv.FormatInt(l.MaxBytes, 10) + ")")
	}
	return nil
}

// Entry is one image a file holds (ADR-0398 §SD7): a directory of a TIFF,
// a header unit of a FITS file, an array of an archive. Its shape is as far
// as the header says; zero is unknown.
type Entry struct {
	// Id names the entry, stably for that file: what [Request.Entry] takes.
	// It is not empty, and no two entries of a file share one.
	Id string
	// Title names it for a person: "page 2", "HDU 1 SCI".
	Title string
	// Detail is the format's own words for it ("BITPIX -32, 3 axes"); may
	// be empty.
	Detail string
	Width  int
	Height int
	// Planes is how many planes the entry's stack has.
	Planes int
	Kind   imaging.SampleKindE
	// Readable reports that the reader reads the entry, limits aside.
	Readable bool
	// Refusal is, for an entry the reader does not read, the refusal a
	// decode of it is: [RefusalUnsupported], [RefusalLimit] or
	// [RefusalMalformed]. It is zero for a readable entry.
	Refusal RefusalE
	// Reason is, for an entry the reader does not read, the refusal's bare
	// words: what it does not read, the size over a bound, the defect —
	// "compression 7 (JPEG)", without the kind's closing words, which
	// [SelectEntry] adds. [Entry.Refused] fills Reason and Refusal from a
	// refusal.
	Reason string
	// Default marks the entry a plain open reads: exactly one entry of a
	// file. Where the file has a readable entry the default is one; where
	// it has none, the default is the entry a plain open would have read —
	// the one meant — and its refusal is the file's.
	Default bool
}

// Refused is the entry marked unreadable for err, a refusal of the seam:
// Readable false, and Reason and Refusal the refusal's bare words and kind
// ([ReasonOf]). Default is left as it is. An err that wraps no refusal
// leaves Refusal zero and its message as the Reason, which the conformance
// kit reports: a reader lists an entry as unreadable for a refusal, and
// returns any other error.
func (inst Entry) Refused(err error) Entry {
	inst.Readable = false
	inst.Reason, inst.Refusal = ReasonOf(err)
	if inst.Refusal == 0 && err != nil {
		inst.Reason = err.Error()
	}
	return inst
}

// Request is what a decode is asked for.
type Request struct {
	// SourceName names the source for a person — a file name, not a path —
	// and is recorded in the provenance.
	SourceName string
	// Entry is the id of the entry to read; empty reads the default.
	Entry  string
	Limits Limits
}

// ReaderI reads one format into stacks (ADR-0398 §SD5–§SD8). The package
// doc is the contract; the conformance kit in decode/decodetest checks it.
// A reader is used from several goroutines at once.
type ReaderI interface {
	// Format describes the reader. It returns the same value every time.
	Format() Format
	// Sniff reports whether a file is this format, from its first bytes:
	// head holds SniffLen of them, or the whole file when it is shorter.
	// It claims no file shorter than its signature, the empty one among
	// them, and reads nothing beyond head.
	Sniff(head []byte) bool
	// Entries lists what the file holds, from its headers, without reading
	// samples or applying limits. ctx is honoured.
	Entries(ctx context.Context, r io.ReaderAt, size int64) ([]Entry, error)
	// Decode reads one entry into a stack, refusing it over req.Limits
	// before a sample is read. ctx is honoured between reads of samples.
	Decode(ctx context.Context, r io.ReaderAt, size int64, req Request) (imaging.Stack, error)
}

// SelectEntry is the entry id names among a file's entries, for a reader's
// Decode to use: the default for the empty id. It refuses an id the file
// does not have with [ErrNoEntry], and an entry that is not readable —
// the default of a file with nothing readable among them — with the
// entry's Refusal ([RefusalUnsupported] where it is zero), the sentence
// its title and its Reason. A file that lists no entry, or no default, is
// refused as unsupported.
func SelectEntry(entries []Entry, id string) (e Entry, err error) {
	i := -1
	if id == "" {
		if i = slices.IndexFunc(entries, func(e Entry) bool { return e.Default }); i < 0 {
			if len(entries) == 0 {
				return e, Unsupported("a file that holds no image")
			}
			return e, Unsupported("a file whose reader names no default entry")
		}
	} else if i = slices.IndexFunc(entries, func(e Entry) bool { return e.Id == id }); i < 0 {
		return e, NoEntry(id)
	}
	e = entries[i]
	if !e.Readable {
		reason := e.Reason
		if reason == "" {
			reason = "an entry it lists as unreadable"
		}
		return e, Refuse(e.Refusal, entryName(e)+": "+reason)
	}
	return e, nil
}

// entryName names an entry in a sentence: its title, or its id.
func entryName(e Entry) string {
	if e.Title != "" {
		return e.Title
	}
	return strconv.Quote(e.Id)
}
