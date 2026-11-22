package decodetest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
)

// File is a valid file of the reader's format, and what the caller expects
// of it.
type File struct {
	// Name names the file in the subtests and is the request's SourceName.
	Name string
	Data []byte
	// Entries, when not nil, is what Entries must list, in order. When
	// nil, only the contract is checked.
	Entries []Expect
	// HeaderSlack is how many bytes a decode refused over a limit may read
	// beyond what Entries reads of the same file; zero takes
	// decode.SniffLen. A reader that reads a table of offsets before it
	// checks the limits sets it to that table's size. So does a reader of a
	// compressed container: the header of the member a decode reads is
	// inflated from the compressed stream, and a decompressor reads its
	// input a block at a time, so reaching even the first bytes of the
	// member reads a block of the file beyond what listing read — its
	// input buffer's worth, which is no sample read early.
	HeaderSlack int64
}

// Expect is what an entry must be. A zero field is not checked.
type Expect struct {
	Id     string
	Width  int
	Height int
	Planes int
	Kind   imaging.SampleKindE
	// Unreadable says the entry is listed as one the reader does not read.
	Unreadable bool
	// Refusal, when not zero, is the refusal the entry is listed with and
	// refused with — decode.RefusalUnsupported, RefusalLimit or
	// RefusalMalformed — and implies Unreadable.
	Refusal decode.RefusalE
}

// Check runs the conformance kit over a reader (ADR-0398 §SD9): a subtest
// "format", and for each file a subtest of its name holding the nine below,
// each failing with what the contract asks.
//
//   - sniff: Sniff claims the file from its first SniffLen bytes, does not
//     panic on any prefix of them nor change them, and claims neither the
//     empty input, SniffLen zero bytes, a block of noise, nor the first
//     bytes of a PNG or a JPEG (but for the built-in readers themselves).
//   - entries: Entries lists at least one entry; ids are not empty and
//     unique; every entry has a title; a readable one has no Refusal, and
//     an unreadable one a Reason — the refusal's bare words, not ending
//     in its kind's — and a Refusal of decode.RefusalUnsupported,
//     RefusalLimit or RefusalMalformed; exactly one entry is the default,
//     and it is readable unless no entry is; what the file's Entries
//     expect.
//   - decode: every readable entry decodes, by its id, to a stack of the
//     width, height, plane count and kind it announced, with the format's
//     name as Provenance.Format and the file's name as SourceName; an
//     unreadable entry is refused with the refusal it announced; the empty
//     id decodes the default, or, in a file with nothing readable, is
//     refused as the default announced.
//   - unknown-entry: an id the file does not have is refused with
//     decode.ErrNoEntry.
//   - limits: for every readable entry, MaxPixels, MaxBytes and MaxPlanes
//     each set one below what its stack needs is refused with
//     decode.ErrLimit — reading, through a counting io.ReaderAt, no more
//     bytes than Entries reads of the file plus File.HeaderSlack — and all
//     three set to exactly the need decode. A limit that cannot be set one
//     below (one pixel, one plane) is skipped, and so is the read bound
//     when the file is no larger than it; a file with nothing readable is
//     skipped, with a log.
//   - cancel: a decode under a cancelled context returns the context's
//     error; one cancelled at its first read past what Entries reads
//     returns a stack or the context's error, nothing else — skipped, with
//     a log, when the default is not readable.
//   - truncated: every prefix of the file — every length up to 4 KiB, a
//     spread of lengths beyond — makes Entries and Decode return a result
//     or an error wrapping decode.ErrMalformed, ErrUnsupported or ErrLimit:
//     never a panic, never an error without one.
//   - repeat: Entries twice lists the same entries, and Decode twice gives
//     the same digest.
//   - io-error: a decode of the default through an io.ReaderAt that fails,
//     with an error of the kit's own, at the first read past what Entries
//     reads — and at every read after — returns an error that wraps that
//     one and is no refusal: a read that fails says nothing of the file
//     (decode.ReadFullAt, and decode.ReadError over a stream of
//     decode.MarkReadFailures, keep the two apart). A decode that reads no
//     further than Entries passes, with a log.
//
// A panic of the reader is caught and reported in the subtest it happened
// in, with its stack.
func Check(t *testing.T, reader decode.ReaderI, files ...File) {
	t.Helper()
	run(testingT{t}, reader, files)
}

// testerI is what the checks need of a test: testing.T in Check, a recorder in
// the kit's own tests.
type testerI interface {
	Helper()
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
	Run(name string, f func(testerI)) bool
}

type testingT struct{ t *testing.T }

func (inst testingT) Helper() { inst.t.Helper() }
func (inst testingT) Errorf(format string, args ...any) {
	inst.t.Helper()
	inst.t.Errorf(format, args...)
}
func (inst testingT) Logf(format string, args ...any) { inst.t.Helper(); inst.t.Logf(format, args...) }
func (inst testingT) Run(name string, f func(testerI)) bool {
	return inst.t.Run(name, func(t *testing.T) { f(testingT{t}) })
}

func run(t testerI, reader decode.ReaderI, files []File) {
	t.Helper()
	format := reader.Format()
	t.Run("format", func(t testerI) { checkFormat(t, reader, format) })
	for _, f := range files {
		t.Run(f.Name, func(t testerI) {
			c := &fileCheck{reader: reader, format: format, file: f}
			c.prepare()
			t.Run("sniff", c.sniff)
			t.Run("entries", c.listEntries)
			t.Run("decode", c.decodeEntries)
			t.Run("unknown-entry", c.unknownEntry)
			t.Run("limits", c.limits)
			t.Run("cancel", c.cancel)
			t.Run("truncated", c.truncated)
			t.Run("repeat", c.repeat)
			t.Run("io-error", c.ioError)
		})
	}
}

func checkFormat(t testerI, reader decode.ReaderI, f decode.Format) {
	if err := f.Validate(); err != nil {
		t.Errorf("the Format must validate (name, title, extensions, version from 1): %v", err)
	}
	if f.Doc == "" {
		t.Errorf("the Format needs a Doc: a sentence or two on what the reader reads and refuses")
	}
	if len(f.Extensions) == 0 {
		t.Errorf("the Format needs the endings its files carry, for a file dialog and a help page")
	}
	if again := reader.Format(); !reflect.DeepEqual(f, again) {
		t.Errorf("Format must return the same value every time: %+v, then %+v", f, again)
	}
	reg := decode.NewRegistry()
	if err := reg.Register(reader); err != nil {
		t.Errorf("the reader must register into an empty registry: %v", err)
		return
	}
	if got, ok := reg.Lookup(f.Name); !ok || got == nil {
		t.Errorf("a registered reader is found by its name %q", f.Name)
	}
	if err := reg.Register(reader); err == nil {
		t.Errorf("a second registration of the name %q must be refused", f.Name)
	}
}

// fileCheck holds what the subtests of one file share: the entries listed
// once, and the stacks decoded once.
type fileCheck struct {
	reader      decode.ReaderI
	format      decode.Format
	file        File
	entries     []decode.Entry
	entriesErr  error
	entriesRead int64
	// decoded holds the stack of each entry that decoded, decodeErrs the
	// error of each that did not.
	decoded    map[string]imaging.Stack
	decodeErrs map[string]error
}

// prepare lists the entries, counting the bytes Entries reads, and
// decodes every entry once, so that each subtest can run alone.
func (inst *fileCheck) prepare() {
	inst.entries, inst.entriesErr = guard(func() ([]decode.Entry, error) {
		cr := &countingReader{r: bytes.NewReader(inst.data())}
		entries, err := inst.reader.Entries(context.Background(), cr, inst.size())
		inst.entriesRead = cr.n.Load()
		return entries, err
	})
	inst.decoded, inst.decodeErrs = make(map[string]imaging.Stack, len(inst.entries)), make(map[string]error, len(inst.entries))
	for _, e := range inst.entries {
		st, err := inst.decodeWith(context.Background(), bytes.NewReader(inst.data()), inst.size(), inst.request(e.Id, decode.Limits{}))
		if err == nil && !st.IsZero() {
			inst.decoded[e.Id] = st
		} else {
			inst.decodeErrs[e.Id] = err
		}
	}
}

// needEntries reports whether the entries were listed; a subtest that
// needs them is skipped, with a log, when they were not.
func (inst *fileCheck) needEntries(t testerI) bool {
	if inst.entriesErr != nil {
		t.Logf("skipped: Entries failed on the whole file (see the entries subtest)")
		return false
	}
	return true
}

func (inst *fileCheck) data() []byte { return inst.file.Data }

func (inst *fileCheck) isDecoded(id string) bool { _, ok := inst.decoded[id]; return ok }

func (inst *fileCheck) size() int64 { return int64(len(inst.file.Data)) }

func (inst *fileCheck) request(id string, l decode.Limits) decode.Request {
	return decode.Request{SourceName: inst.file.Name, Entry: id, Limits: l}
}

// decodeWith decodes the file through r under ctx, catching a panic.
func (inst *fileCheck) decodeWith(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (imaging.Stack, error) {
	return guard(func() (imaging.Stack, error) { return inst.reader.Decode(ctx, r, size, req) })
}

func (inst *fileCheck) sniff(t testerI) {
	head := slices.Clone(inst.data()[:min(len(inst.data()), decode.SniffLen)])
	kept := slices.Clone(head)
	claimed, err := guard(func() (bool, error) { return inst.reader.Sniff(head), nil })
	switch {
	case err != nil:
		t.Errorf("Sniff panicked on the file's first %d bytes: %s", len(head), panicOf(err))
	case !claimed:
		t.Errorf("Sniff must claim a file of its format from its first %d bytes", len(head))
	}
	if !bytes.Equal(head, kept) {
		t.Errorf("Sniff must not change the bytes it is given")
	}
	for n := range len(head) {
		if _, err := guard(func() (bool, error) { return inst.reader.Sniff(head[:n:n]), nil }); err != nil {
			t.Errorf("Sniff must not panic on any prefix of a file: it did on the first %d bytes: %s", n, panicOf(err))
			break
		}
	}
	noise := make([]byte, decode.SniffLen)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range noise {
		noise[i] = byte(rng.Uint32())
	}
	others := []struct {
		what string
		head []byte
	}{
		{"the empty input", nil},
		{"SniffLen zero bytes", make([]byte, decode.SniffLen)},
		{"SniffLen bytes of noise", noise},
	}
	if inst.format.Name != decode.FormatPNG.String() {
		others = append(others, struct {
			what string
			head []byte
		}{"a PNG's first bytes, which the built-in PNG reader claims", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")})
	}
	if inst.format.Name != decode.FormatJPEG.String() {
		others = append(others, struct {
			what string
			head []byte
		}{"a JPEG's first bytes, which the built-in JPEG reader claims", []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0}})
	}
	for _, o := range others {
		claimed, err := guard(func() (bool, error) { return inst.reader.Sniff(o.head), nil })
		switch {
		case err != nil:
			t.Errorf("Sniff panicked on %s: %s", o.what, panicOf(err))
		case claimed:
			t.Errorf("Sniff must not claim %s: a file two readers claim is refused", o.what)
		}
	}
}

func (inst *fileCheck) listEntries(t testerI) {
	switch {
	case panicOf(inst.entriesErr) != "":
		t.Errorf("Entries panicked on a valid file: %s", panicOf(inst.entriesErr))
		return
	case inst.entriesErr != nil:
		t.Errorf("Entries must list the entries of a valid file: %v", inst.entriesErr)
		return
	case len(inst.entries) == 0:
		t.Errorf("Entries must list at least one entry of a valid file")
		return
	}
	defaults, readable := 0, 0
	for i, e := range inst.entries {
		switch {
		case e.Id == "":
			t.Errorf("entry %d: an entry needs an id, which Request.Entry names it by", i)
		case slices.ContainsFunc(inst.entries[:i], func(o decode.Entry) bool { return o.Id == e.Id }):
			t.Errorf("entry %d: the id %q is listed twice; ids are unique in a file", i, e.Id)
		}
		if e.Title == "" {
			t.Errorf("entry %q: an entry needs a title for a person", e.Id)
		}
		if e.Width < 0 || e.Height < 0 || e.Planes < 0 {
			t.Errorf("entry %q: a size of %d × %d, %d planes; zero is unknown, nothing is negative", e.Id, e.Width, e.Height, e.Planes)
		}
		if e.Kind != 0 && !slices.Contains(imaging.AllSampleKinds, e.Kind) {
			t.Errorf("entry %q: the kind %d is none of imaging.AllSampleKinds; zero is unknown", e.Id, e.Kind)
		}
		switch {
		case e.Readable:
			readable++
			if e.Refusal != 0 {
				t.Errorf("entry %q: a readable entry has no Refusal; it has %s", e.Id, e.Refusal)
			}
		case e.Reason == "":
			t.Errorf("entry %q: an unreadable entry needs a Reason naming what the reader does not read", e.Id)
		case !isEntryRefusal(e.Refusal):
			t.Errorf("entry %q: an unreadable entry names its refusal — decode.RefusalUnsupported, RefusalLimit or RefusalMalformed "+
				"(Entry.Refused fills it from the refusal); it has %s", e.Id, e.Refusal)
		case strings.HasSuffix(e.Reason, ": "+e.Refusal.Sentinel().Error()):
			t.Errorf("entry %q: the Reason %q ends in its kind's own words; it is the refusal's bare words (decode.ReasonOf, Entry.Refused), "+
				"to which decode.SelectEntry adds them", e.Id, e.Reason)
		}
		if e.Default {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("exactly one entry of a file is the default — the one a plain open reads, or in a file with nothing readable the one it would have read; %d are", defaults)
	} else if d, _ := inst.theDefault(); !d.Readable && readable > 0 {
		t.Errorf("entry %q: the default is unreadable while %d entries are readable; the default of a file with a readable entry is readable", d.Id, readable)
	}
	if inst.file.Entries == nil {
		return
	}
	if len(inst.entries) != len(inst.file.Entries) {
		t.Errorf("Entries listed %d entries; the file has %d", len(inst.entries), len(inst.file.Entries))
		return
	}
	for i, want := range inst.file.Entries {
		got := inst.entries[i]
		if got.Id != want.Id {
			t.Errorf("entry %d: id %q, expected %q", i, got.Id, want.Id)
		}
		inst.matchShape(t, "entry "+strconv.Quote(want.Id)+" as listed", want.Width, want.Height, want.Planes, want.Kind,
			got.Width, got.Height, got.Planes, got.Kind)
		unreadable := want.Unreadable || want.Refusal != 0
		if got.Readable == unreadable {
			t.Errorf("entry %q: readable %v, expected %v", want.Id, got.Readable, !unreadable)
		}
		if want.Refusal != 0 && got.Refusal != want.Refusal {
			t.Errorf("entry %q: listed with the refusal %s, expected %s", want.Id, got.Refusal, want.Refusal)
		}
	}
}

// isEntryRefusal reports a refusal an unreadable entry can be listed with.
func isEntryRefusal(r decode.RefusalE) bool {
	return r == decode.RefusalUnsupported || r == decode.RefusalLimit || r == decode.RefusalMalformed
}

// theDefault is the file's one default entry; false when the file has
// none or several, which the entries subtest reports.
func (inst *fileCheck) theDefault() (e decode.Entry, ok bool) {
	i := slices.IndexFunc(inst.entries, func(e decode.Entry) bool { return e.Default })
	if i < 0 || slices.IndexFunc(inst.entries[i+1:], func(e decode.Entry) bool { return e.Default }) >= 0 {
		return e, false
	}
	return inst.entries[i], true
}

// anyReadable reports whether the file lists a readable entry.
func (inst *fileCheck) anyReadable() bool {
	return slices.ContainsFunc(inst.entries, func(e decode.Entry) bool { return e.Readable })
}

// matchShape reports each nonzero expected field that differs.
func (inst *fileCheck) matchShape(t testerI, what string, w, h, planes int, kind imaging.SampleKindE, gw, gh, gplanes int, gkind imaging.SampleKindE) {
	t.Helper()
	if w != 0 && gw != w {
		t.Errorf("%s: width %d, expected %d", what, gw, w)
	}
	if h != 0 && gh != h {
		t.Errorf("%s: height %d, expected %d", what, gh, h)
	}
	if planes != 0 && gplanes != planes {
		t.Errorf("%s: %d planes, expected %d", what, gplanes, planes)
	}
	if kind != 0 && gkind != kind {
		t.Errorf("%s: kind %s, expected %s", what, gkind, kind)
	}
}

func (inst *fileCheck) decodeEntries(t testerI) {
	if !inst.needEntries(t) {
		return
	}
	ctx, r := context.Background(), bytes.NewReader(inst.data())
	for _, e := range inst.entries {
		st, err := inst.decoded[e.Id], inst.decodeErrs[e.Id]
		switch {
		case panicOf(err) != "":
			t.Errorf("Decode of entry %q panicked: %s", e.Id, panicOf(err))
			continue
		case !e.Readable:
			switch got := decode.RefusalOf(err); {
			case err == nil:
				t.Errorf("entry %q is listed as unreadable (%s), yet it decodes", e.Id, e.Reason)
			case got == 0:
				t.Errorf("Decode of the unreadable entry %q must refuse with one of the seam's refusals: %v", e.Id, err)
			case isEntryRefusal(e.Refusal) && got != e.Refusal:
				t.Errorf("entry %q is listed with the refusal %s, and its decode is refused as %s (decode.SelectEntry keeps the kind): %v",
					e.Id, e.Refusal, got, err)
			}
			continue
		case err != nil:
			t.Errorf("a readable entry decodes; %q did not: %v", e.Id, err)
			continue
		case st.IsZero():
			t.Errorf("Decode of entry %q returned no error and an empty stack", e.Id)
			continue
		}
		inst.matchShape(t, "the stack of entry "+strconv.Quote(e.Id)+" against what Entries announced", e.Width, e.Height, e.Planes, e.Kind,
			st.Width(), st.Height(), st.PlaneCount(), st.Kind())
		prov := st.Provenance()
		if prov.Format != inst.format.Name {
			t.Errorf("entry %q: Provenance.Format is %q; it is the format's name, %q", e.Id, prov.Format, inst.format.Name)
		}
		if prov.SourceName != inst.file.Name {
			t.Errorf("entry %q: Provenance.SourceName is %q; it is the request's SourceName, %q", e.Id, prov.SourceName, inst.file.Name)
		}
	}
	d, ok := inst.theDefault()
	if !ok {
		return // no one default: the entries subtest says so
	}
	st, err := inst.decodeWith(ctx, r, inst.size(), inst.request("", decode.Limits{}))
	switch got := decode.RefusalOf(err); {
	case panicOf(err) != "":
		t.Errorf("Decode of the default entry panicked: %s", panicOf(err))
	case !d.Readable && err == nil:
		t.Errorf("the default entry %q is listed as unreadable (%s), yet the empty id decodes", d.Id, d.Reason)
	case !d.Readable && isEntryRefusal(d.Refusal) && got != d.Refusal:
		t.Errorf("the empty id is refused as the default entry %q is listed, with the refusal %s; it was refused as %s: %v", d.Id, d.Refusal, got, err)
	case !d.Readable:
	case err != nil:
		t.Errorf("the empty id decodes the default entry %q: %v", d.Id, err)
	case !inst.isDecoded(d.Id):
	case st.Hash() != inst.decoded[d.Id].Hash():
		t.Errorf("the empty id decodes the default entry %q; it decoded another stack", d.Id)
	}
}

func (inst *fileCheck) unknownEntry(t testerI) {
	if !inst.needEntries(t) {
		return
	}
	id := "decodetest: no such entry"
	_, err := inst.decodeWith(context.Background(), bytes.NewReader(inst.data()), inst.size(), inst.request(id, decode.Limits{}))
	switch {
	case panicOf(err) != "":
		t.Errorf("Decode of an unknown entry panicked: %s", panicOf(err))
	case !errors.Is(err, decode.ErrNoEntry):
		t.Errorf("an id the file does not have is refused with decode.ErrNoEntry (decode.NoEntry builds it); got %v", err)
	}
}

func (inst *fileCheck) limits(t testerI) {
	if !inst.needEntries(t) {
		return
	}
	if !inst.anyReadable() {
		t.Logf("skipped: the file lists no readable entry, so no decode reaches a limit")
		return
	}
	slack := inst.file.HeaderSlack
	if slack <= 0 {
		slack = decode.SniffLen
	}
	bound := inst.entriesRead + slack
	if inst.size() <= bound {
		t.Logf("the file is no larger than Entries' reading plus the slack (%d bytes): the read bound says nothing of it; give a larger file", bound)
	}
	for _, e := range inst.entries {
		st, ok := inst.decoded[e.Id]
		if !ok {
			continue
		}
		pixels := int64(st.Width()) * int64(st.Height())
		planes := st.PlaneCount()
		need := pixels * int64(planes) * int64(st.Kind().Bytes())
		type limitCase struct {
			name   string
			limits decode.Limits
		}
		var cases []limitCase
		if pixels > 1 {
			cases = append(cases, limitCase{"MaxPixels", decode.Limits{MaxPixels: pixels - 1}})
		} else {
			t.Logf("entry %q: one pixel a plane; MaxPixels cannot be set below it", e.Id)
		}
		if need > 1 {
			cases = append(cases, limitCase{"MaxBytes", decode.Limits{MaxBytes: need - 1}})
		}
		if planes > 1 {
			cases = append(cases, limitCase{"MaxPlanes", decode.Limits{MaxPlanes: planes - 1}})
		} else {
			t.Logf("entry %q: one plane; MaxPlanes cannot be set below it", e.Id)
		}
		for _, c := range cases {
			cr := &countingReader{r: bytes.NewReader(inst.data())}
			_, err := inst.decodeWith(context.Background(), cr, inst.size(), inst.request(e.Id, c.limits))
			read := cr.n.Load()
			switch {
			case panicOf(err) != "":
				t.Errorf("entry %q: Decode under %s one below the need panicked: %s", e.Id, c.name, panicOf(err))
			case !errors.Is(err, decode.ErrLimit):
				t.Errorf("entry %q: %s set one below what the stack needs must be refused with decode.ErrLimit; got %v", e.Id, c.name, err)
			case read > bound && inst.size() > bound:
				t.Errorf("entry %q: the refusal over %s read %d bytes, Entries reads %d and the slack is %d: a limit is checked from the header, before samples are read",
					e.Id, c.name, read, inst.entriesRead, slack)
			}
		}
		exact := decode.Limits{MaxPixels: max(pixels, 1), MaxBytes: max(need, 1), MaxPlanes: max(planes, 1)}
		if _, err := inst.decodeWith(context.Background(), bytes.NewReader(inst.data()), inst.size(), inst.request(e.Id, exact)); err != nil {
			t.Errorf("entry %q: limits of exactly what the stack needs (%d pixels a plane, %d bytes, %d planes) must decode: %v",
				e.Id, exact.MaxPixels, exact.MaxBytes, exact.MaxPlanes, err)
		}
	}
}

func (inst *fileCheck) cancel(t testerI) {
	if !inst.needEntries(t) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := inst.decodeWith(ctx, bytes.NewReader(inst.data()), inst.size(), inst.request("", decode.Limits{}))
	switch {
	case panicOf(err) != "":
		t.Errorf("Decode under a cancelled context panicked: %s", panicOf(err))
	case !errors.Is(err, context.Canceled):
		t.Errorf("a decode under a cancelled context returns the context's error; got %v", err)
	}

	if d, ok := inst.theDefault(); !ok || !d.Readable {
		t.Logf("skipped the cancel while reading: the default entry is not readable, so a decode reads no samples")
		return
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	cr := &countingReader{r: bytes.NewReader(inst.data()), onRead: func(total int64) {
		if total > inst.entriesRead {
			cancel()
		}
	}}
	_, err = inst.decodeWith(ctx, cr, inst.size(), inst.request("", decode.Limits{}))
	switch {
	case panicOf(err) != "":
		t.Errorf("Decode cancelled while reading panicked: %s", panicOf(err))
	case err != nil && !errors.Is(err, context.Canceled):
		t.Errorf("a decode cancelled while it reads returns a stack or the context's error; got %v", err)
	}
}

// truncationLengths are the prefix lengths the truncated subtest tries:
// every one up to 4 KiB, then 512 spread over the rest and the last 64.
func truncationLengths(n int) []int {
	const every, spread, tail = 4096, 512, 64
	out := make([]int, 0, min(n, every)+spread+tail)
	for l := range min(n, every) {
		out = append(out, l)
	}
	if n <= every {
		return out
	}
	for k := range spread {
		out = append(out, every+int(int64(n-every)*int64(k)/spread))
	}
	for l := max(every, n-tail); l < n; l++ {
		out = append(out, l)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (inst *fileCheck) truncated(t testerI) {
	const report = 5
	failures := 0
	fail := func(format string, args ...any) {
		failures++
		if failures <= report {
			t.Errorf(format, args...)
		}
	}
	isRefusal := func(err error) bool {
		return errors.Is(err, decode.ErrMalformed) || errors.Is(err, decode.ErrUnsupported) || errors.Is(err, decode.ErrLimit)
	}
	ctx := context.Background()
	for _, n := range truncationLengths(len(inst.data())) {
		prefix := inst.data()[:n:n]
		_, err := guard(func() ([]decode.Entry, error) { return inst.reader.Entries(ctx, bytes.NewReader(prefix), int64(n)) })
		switch {
		case panicOf(err) != "":
			fail("Entries panicked on the file cut to %d bytes: %s", n, panicOf(err))
		case err != nil && !isRefusal(err):
			fail("Entries on the file cut to %d bytes returned an error that wraps none of the seam's refusals (decode.Malformed, Unsupported, OverLimit build them): %v", n, err)
		}
		_, err = inst.decodeWith(ctx, bytes.NewReader(prefix), int64(n), inst.request("", decode.Limits{}))
		switch {
		case panicOf(err) != "":
			fail("Decode panicked on the file cut to %d bytes: %s", n, panicOf(err))
		case err != nil && !isRefusal(err):
			fail("Decode on the file cut to %d bytes returned an error that wraps none of the seam's refusals (decode.Malformed, Unsupported, OverLimit build them): %v", n, err)
		}
	}
	if failures > report {
		t.Errorf("… and %d more truncations failed", failures-report)
	}
}

func (inst *fileCheck) repeat(t testerI) {
	if !inst.needEntries(t) {
		return
	}
	again, err := guard(func() ([]decode.Entry, error) {
		return inst.reader.Entries(context.Background(), bytes.NewReader(inst.data()), inst.size())
	})
	if err != nil || !reflect.DeepEqual(again, inst.entries) {
		t.Errorf("Entries of the same bytes lists the same entries: %+v, then %+v (%v)", inst.entries, again, err)
	}
	for _, e := range inst.entries {
		first, ok := inst.decoded[e.Id]
		if !ok {
			continue
		}
		st, err := inst.decodeWith(context.Background(), bytes.NewReader(inst.data()), inst.size(), inst.request(e.Id, decode.Limits{}))
		if err != nil || st.Hash() != first.Hash() {
			t.Errorf("entry %q: Decode of the same bytes gives the same digest (%v)", e.Id, err)
		}
	}
}

// errInjected is the failure of the io-error subtest's io.ReaderAt: no
// reader returns it of its own.
var errInjected = errors.New("decodetest: a read failed on purpose")

func (inst *fileCheck) ioError(t testerI) {
	if !inst.needEntries(t) {
		return
	}
	fr := &failingReader{r: bytes.NewReader(inst.data()), after: inst.entriesRead}
	_, err := inst.decodeWith(context.Background(), fr, inst.size(), inst.request("", decode.Limits{}))
	switch {
	case panicOf(err) != "":
		t.Errorf("Decode panicked when a read failed: %s", panicOf(err))
	case !fr.hasFailed():
		t.Logf("the decode read no more than Entries reads (%d bytes), so no read failed: the check says nothing of this file", inst.entriesRead)
	case !errors.Is(err, errInjected):
		t.Errorf("a read that fails reaches the caller: Decode returns an error wrapping the io.ReaderAt's own (decode.ReadFullAt passes it through); got %v", err)
	case decode.RefusalOf(err) != 0:
		t.Errorf("a read that fails is no refusal: it says nothing of the file, and %s says the file is at fault "+
			"(read through decode.ReadFullAt; read a stream over decode.MarkReadFailures of the file and pass its errors to decode.ReadError); got %v",
			decode.RefusalOf(err), err)
	}
}

// panicked is a panic of the reader, caught by guard.
type panicked struct{ text string }

func (inst *panicked) Error() string { return inst.text }

// panicOf is the panic err carries, with its stack; empty when it is none.
func panicOf(err error) string {
	var p *panicked
	if errors.As(err, &p) {
		return p.text
	}
	return ""
}

// guard runs f, turning a panic into a panicked error with its stack.
func guard[T any](f func() (T, error)) (out T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &panicked{text: "panic: " + panicText(r) + "\n" + string(debug.Stack())}
		}
	}()
	return f()
}

func panicText(r any) string {
	switch v := r.(type) {
	case error:
		return v.Error()
	case string:
		return v
	}
	return reflect.TypeOf(r).String()
}

// countingReader counts the bytes read through it, and calls onRead with
// the running total after each read.
type countingReader struct {
	r      io.ReaderAt
	n      atomic.Int64
	onRead func(total int64)
}

func (inst *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := inst.r.ReadAt(p, off)
	total := inst.n.Add(int64(n))
	if inst.onRead != nil {
		inst.onRead(total)
	}
	return n, err
}

// failingReader reads through r until a read would take the bytes read past
// after; that read and every later one fail with errInjected.
type failingReader struct {
	r      io.ReaderAt
	after  int64
	mu     sync.Mutex
	n      int64
	failed bool
}

func (inst *failingReader) ReadAt(p []byte, off int64) (int, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.failed || inst.n+int64(len(p)) > inst.after {
		inst.failed = true
		return 0, errInjected
	}
	n, err := inst.r.ReadAt(p, off)
	inst.n += int64(n)
	return n, err
}

func (inst *failingReader) hasFailed() bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.failed
}
