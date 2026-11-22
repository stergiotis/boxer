package decode

import (
	"context"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// Registry holds readers by their format's name (ADR-0398 §SD6) and hands a
// file to the one that claims it. It is safe for concurrent use; the zero
// value is an empty registry.
type Registry struct {
	mu sync.RWMutex
	// readers and formats are parallel, ordered by name; a format is read
	// from its reader once, when it is registered.
	readers []ReaderI
	formats []Format
}

// NewRegistry is an empty registry: a test's, or a host's that wants no
// built-in reader. [Default] is the process's.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a reader. It refuses one whose [Format] does not validate
// ([Format.Validate]) and one whose name is registered already: a name is
// registered once, and a host that wants another reader of a format
// registers it under another name.
func (inst *Registry) Register(r ReaderI) error {
	if r == nil {
		return eb.Build().Errorf("a nil reader")
	}
	f := r.Format().clone()
	if err := f.Validate(); err != nil {
		return eb.Build().Str("name", f.Name).Errorf("register a reader: %w", err)
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	i, found := slices.BinarySearchFunc(inst.formats, f.Name, func(a Format, name string) int { return strings.Compare(a.Name, name) })
	if found {
		return eb.Build().Str("name", f.Name).Str("title", inst.formats[i].Title).Errorf("a reader of that name is registered already")
	}
	inst.readers = slices.Insert(inst.readers, i, r)
	inst.formats = slices.Insert(inst.formats, i, f)
	return nil
}

// Formats lists the formats registered, by name.
func (inst *Registry) Formats() []Format {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	out := make([]Format, len(inst.formats))
	for i, f := range inst.formats {
		out[i] = f.clone()
	}
	return out
}

// Lookup is the reader registered under name.
func (inst *Registry) Lookup(name string) (r ReaderI, ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	if i, found := slices.BinarySearchFunc(inst.formats, name, func(a Format, name string) int { return strings.Compare(a.Name, name) }); found {
		return inst.readers[i], true
	}
	return nil, false
}

// Sniff is the reader that claims a file from its first bytes, of which it
// reads SniffLen at most. It refuses a file no reader claims with
// [ErrUnknownFormat], naming the formats registered, and one that two or
// more claim with [ErrAmbiguous], naming every claimant: no order of
// registration decides.
func (inst *Registry) Sniff(head []byte) (r ReaderI, err error) {
	head = head[:min(len(head), SniffLen)]
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	var claimed []int
	for i, rd := range inst.readers {
		if rd.Sniff(head) {
			claimed = append(claimed, i)
		}
	}
	switch len(claimed) {
	case 0:
		return nil, inst.unknownFormat()
	case 1:
		return inst.readers[claimed[0]], nil
	}
	names := make([]string, len(claimed))
	for k, i := range claimed {
		names[k] = inst.formats[i].Title + " (" + inst.formats[i].Name + ")"
	}
	msg := ErrAmbiguous.Error() + ": " + strings.Join(names, ", ") + "; the host has to register only one of them"
	return nil, eb.Build().Strs("formats", names).Errorf("%w", &refusal{msg: msg, what: msg, kind: ErrAmbiguous})
}

// unknownFormat is the refusal of a file no reader claims, naming what the
// registry reads. The caller holds the lock.
func (inst *Registry) unknownFormat() error {
	titles := make([]string, len(inst.formats))
	for i, f := range inst.formats {
		titles[i] = f.Title
	}
	msg := ErrUnknownFormat.Error() + "; no format is read here"
	if len(titles) > 0 {
		msg = ErrUnknownFormat.Error() + " (the formats read here: " + strings.Join(titles, "; ") + ")"
	}
	return eb.Build().Strs("formats", titles).Errorf("%w", &refusal{msg: msg, what: msg, kind: ErrUnknownFormat})
}

// Identify reads a file's first bytes and is [Registry.Sniff] of them. An
// error reading them is returned as it is, no refusal.
func (inst *Registry) Identify(ctx context.Context, r io.ReaderAt, size int64) (rd ReaderI, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if size < 0 {
		return nil, eb.Build().Int64("size", size).Errorf("a negative file size")
	}
	head := make([]byte, min(size, SniffLen))
	if n, rerr := r.ReadAt(head, 0); n < len(head) {
		return nil, eb.Build().Int("read", n).Int("want", len(head)).Errorf("read the file's first bytes: %w", rerr)
	}
	return inst.Sniff(head)
}

// Entries lists what a file holds, through the reader that claims it, and
// that reader's format.
func (inst *Registry) Entries(ctx context.Context, r io.ReaderAt, size int64) (f Format, entries []Entry, err error) {
	rd, err := inst.Identify(ctx, r, size)
	if err != nil {
		return
	}
	f = rd.Format()
	entries, err = rd.Entries(ctx, r, size)
	if err != nil {
		err = eb.Build().Str("format", f.Name).Errorf("%w", err)
	}
	return
}

// Decode reads an entry of a file into a stack, through the reader that
// claims it. The reader's refusal is returned with its sentence unchanged.
func (inst *Registry) Decode(ctx context.Context, r io.ReaderAt, size int64, req Request) (stack imaging.Stack, err error) {
	rd, err := inst.Identify(ctx, r, size)
	if err != nil {
		return
	}
	stack, err = rd.Decode(ctx, r, size, req)
	if err != nil {
		err = eb.Build().Str("format", rd.Format().Name).Str("source", req.SourceName).Str("entry", req.Entry).Errorf("%w", err)
	}
	return
}

// defaultRegistry is the process's registry. It holds the built-in readers
// and nothing else until the host registers its own. The built-ins' formats
// are constants a test validates, so their registration cannot fail.
var defaultRegistry = func() *Registry {
	r := NewRegistry()
	for _, b := range BuiltinReaders() {
		_ = r.Register(b)
	}
	return r
}()

// Default is the process's registry: PNG and JPEG from the start, and what
// the host registers where it wires its apps (see the package doc).
func Default() *Registry { return defaultRegistry }

// Register adds a reader to the process's registry ([Registry.Register]).
// A host calls it where it wires its apps, in one visible place, before
// they open files — not from an init function of the reader's package: a
// reader is a parser of untrusted bytes, and which ones a deployment
// exposes should be readable at one site rather than follow from what
// happens to be linked (ADR-0398 §SD6).
func Register(r ReaderI) error { return defaultRegistry.Register(r) }
