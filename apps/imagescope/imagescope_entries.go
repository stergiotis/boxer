package imagescope

// A file's formats and entries (ADR-0398 §SD6, §SD7, §M5). What opens is
// what the host registered with the decode seam: the window reads through
// a registry — the process's unless a test gives it its own — and lists its
// formats. A file read through a reader may hold several entries — the
// header units of one format, the arrays of an archive of another; the
// window opens the default one, keeps the file's bytes while it shows
// something from that file and the file has another entry to show, and
// reads another entry from them off the frame, as a selection is taken.

import (
	"bytes"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const (
	opListFormats = "list_formats"
	opGetEntries  = "get_entries"
	opSetEntry    = "set_entry"

	// entriesPageMax bounds the entries get_entries lists at once.
	entriesPageMax = 256
)

// registry is the registry the window reads files through: the one a test
// gave it, otherwise the process's.
func (inst *App) registry() *decode.Registry {
	if inst.readers != nil {
		return inst.readers
	}
	return decode.Default()
}

// openedFile is the file the image opened was read from: its format, its
// entries and the one shown, and — when it has more than one — its bytes,
// so another is shown without the person opening it again. The bytes are
// at most the file broker's read cap; they go when the next open lands. It
// is never modified once made.
type openedFile struct {
	name    string
	format  decode.Format
	reader  decode.ReaderI
	entries []decode.Entry
	shown   string
	// variant is the format's variant the entry shown was read as
	// (imaging.Provenance.Variant); empty where the format has none.
	variant string
	body    []byte
}

// withShown is the same file with another entry shown, read as variant.
func (inst *openedFile) withShown(id, variant string) *openedFile {
	out := *inst
	out.shown, out.variant = id, variant
	return &out
}

// index is the position of entry id, -1 when the file has none.
func (inst *openedFile) index(id string) int {
	return slices.IndexFunc(inst.entries, func(e decode.Entry) bool { return e.Id == id })
}

// switchable reports whether another entry can be shown from the bytes
// held.
func (inst *openedFile) switchable() bool {
	return inst != nil && inst.body != nil && inst.reader != nil && len(inst.entries) > 1
}

// defaultEntry is the id of the entry a plain open reads; empty when the
// file marks none.
func defaultEntry(entries []decode.Entry) string {
	if i := slices.IndexFunc(entries, func(e decode.Entry) bool { return e.Default }); i >= 0 {
		return entries[i].Id
	}
	return ""
}

// entryText is an entry in one line: its title, size, planes and kind, as
// far as the file's header says.
func entryText(e decode.Entry) string {
	parts := []string{e.Title}
	if e.Width > 0 && e.Height > 0 {
		parts = append(parts, strconv.Itoa(e.Width)+" × "+strconv.Itoa(e.Height))
	}
	if e.Planes > 0 {
		p := strconv.Itoa(e.Planes) + " planes"
		if e.Planes == 1 {
			p = "1 plane"
		}
		parts = append(parts, p)
	}
	if e.Kind != 0 {
		parts = append(parts, e.Kind.String())
	}
	return strings.Join(parts, " · ")
}

// streamedFile is a PNG's or JPEG's file as the built-in reader of its
// format lists it, for get_view to name; nil when it cannot be listed.
// Its bytes are not kept: it has one entry.
func streamedFile(ctx context.Context, format decode.FormatE, name string, body []byte) *openedFile {
	for _, rd := range decode.BuiltinReaders() {
		f := rd.Format()
		if f.Name != format.String() {
			continue
		}
		entries, err := rd.Entries(ctx, bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return nil
		}
		return &openedFile{name: name, format: f, entries: entries, shown: defaultEntry(entries)}
	}
	return nil
}

// readFile opens a file through the registry: the reader that claims it,
// its entries, and its default entry decoded into a stack. A refusal is
// returned as the reader's or the registry's own error, so its sentence and
// its kind reach the window.
func readFile(ctx context.Context, reg *decode.Registry, report bgjob.Reporter, name string, body []byte) (res *opened, err error) {
	r, size := bytes.NewReader(body), int64(len(body))
	rd, err := reg.Identify(ctx, r, size)
	if err != nil {
		if errorsIsCancelled(err) {
			return nil, err
		}
		return nil, &openError{file: name, what: "was refused", err: err}
	}
	f := rd.Format()
	refused := func(err error) error {
		if errorsIsCancelled(err) {
			return err
		}
		return &openError{file: name, what: "was refused by the reader of " + f.Title, err: err}
	}
	entries, err := rd.Entries(ctx, r, size)
	if err != nil {
		return nil, refused(err)
	}
	report(0, 0, "decoding "+name)
	st, err := rd.Decode(ctx, r, size, decode.Request{SourceName: name})
	if err != nil {
		return nil, refused(err)
	}
	file := &openedFile{name: name, format: f, reader: rd, entries: entries, shown: defaultEntry(entries), variant: st.Provenance().Variant}
	if len(entries) > 1 {
		file.body = body
	}
	if res, err = openedStack(ctx, st, file.shown, report); err != nil {
		return nil, err
	}
	res.file = file
	return res, nil
}

// readEntry decodes another entry of the file held into a stack and the
// store of its default selection: what set_entry runs off the frame.
func readEntry(ctx context.Context, file *openedFile, id string, report bgjob.Reporter) (*selected, error) {
	report(0, 0, "reading "+id)
	st, err := file.reader.Decode(ctx, bytes.NewReader(file.body), int64(len(file.body)), decode.Request{SourceName: file.name, Entry: id})
	if err != nil {
		return nil, err
	}
	o, err := openedStack(ctx, st, id, report)
	if err != nil {
		return nil, err
	}
	return &selected{pic: o.pic, sel: o.stk.shown, stk: o.stk}, nil
}

// errNoFile is the refusal of an entry operation when no file is open.
var errNoFile = app.RefuseOperation("no file is open: get_entries and set_entry are of a file the person opened with Open…; a sample has no entries")

// errEntryPending refuses a change of the selection while another entry of
// the file is being read: it would be of the stack about to go.
var errEntryPending = app.RefuseOperation("another entry of the file is being read; set it once that entry is shown — set_entry settles then, and get_view's Stack.Pending is false")

// entryPending reports an entry asked for that is not shown yet.
func (inst *App) entryPending() bool {
	return inst.stk != nil && inst.want.entry != inst.stk.shown.entry
}

// setEntry asks for another entry of the file open: it is read from the
// bytes held, off the frame, and replaces the image opened when it lands,
// as an open does. The entry shown asks for nothing. It sets, never
// toggles.
func (inst *App) setEntry(id string) error {
	f := inst.file
	if f == nil {
		return errNoFile
	}
	i := f.index(id)
	switch {
	case i < 0:
		ids := make([]string, 0, min(len(f.entries), 16))
		for _, e := range f.entries[:min(len(f.entries), 16)] {
			ids = append(ids, e.Id)
		}
		more := ""
		if len(f.entries) > 16 {
			more = ", …; get_entries lists them all"
		}
		return app.RefuseOperation("the file has no entry " + strconv.Quote(id) + "; its entries are " + quotedList(ids) + more)
	case !f.entries[i].Readable:
		return app.RefuseOperation("the entry " + strconv.Quote(id) + " (" + f.entries[i].Title + ") is one its reader does not read: " +
			f.entries[i].Reason + "; choose a readable one — get_entries lists them")
	case inst.stk == nil:
		return errNoFile
	}
	if id == inst.want.entry {
		return nil
	}
	if !f.switchable() {
		return app.RefuseOperation("the file's bytes are not held, so no other entry can be read; the person opens the file again with Open…")
	}
	want := inst.want.clone()
	want.entry = id
	inst.setWant(want)
	return nil
}

// entryGesture is the person's set_entry.
func (inst *App) entryGesture(id string) {
	inst.viewGesture(opSetEntry, SetEntryArgs{Id: id}, func() error { return inst.setEntry(id) })
}

// Hover help of the entry picker.
const (
	tipEntry = "The image of the file shown. A file can hold several — the header units of one format, the arrays of an archive of another — and the window shows one at a time; choosing another reads it from the file held, keeping the regions and the results. An entry the reader does not read is listed with its reason and cannot be chosen."
)

// renderEntryRow draws the entry picker of a file with more than one: a
// combo box of the entries around the one asked for, the unreadable ones
// as labels whose hover says why, and which of how many is shown.
func (inst *App) renderEntryRow() {
	f := inst.file
	if f == nil || len(f.entries) < 2 || inst.stk == nil {
		return
	}
	asked := max(f.index(inst.want.entry), 0)
	for range c.Horizontal().KeepIter() {
		for range c.IdScope(inst.ids.PrepareStr("entry")) {
			c.Label("Entry").Selectable(false).Send()
			from := max(0, min(asked-maxComboEntries/2, len(f.entries)-maxComboEntries))
			to := min(len(f.entries), from+maxComboEntries)
			for range c.HoverText(tipEntry).KeepIter() {
				for range c.ComboBox(inst.ids.PrepareStr("pick"), c.WidgetText().Text("").Keep(),
					c.WidgetText().Text(entryText(f.entries[asked])).Keep()).KeepIter() {
					for range c.IdScope(inst.ids.PrepareStr("items")) {
						for k := from; k < to; k++ {
							e := f.entries[k]
							if !e.Readable {
								metric(entryText(e)+" — not read", "Its reader does not read this entry: "+e.Reason)
								continue
							}
							if c.Button(inst.ids.PrepareSeq(uint64(k)), c.Atoms().Text(entryText(e)).Keep()).
								Selected(k == asked).SendResp().HasPrimaryClicked() && k != asked {
								inst.entryGesture(e.Id)
							}
						}
					}
				}
			}
			c.Label(strconv.Itoa(asked+1) + " of " + strconv.Itoa(len(f.entries)) + " in " + f.format.Title).Send()
		}
	}
}

// FormatInfo is one format the host reads.
type FormatInfo struct {
	Name       string   `desc:"the format's name in the host's registry; get_view's File.Format and Image.Format carry it"`
	Title      string   `desc:"the format's name for a person"`
	Extensions []string `json:",omitzero" desc:"the endings its files usually carry; a hint, not how a file is recognised"`
	Version    int      `desc:"the version of the reader's own reading: a file read under another version may have decoded to something else"`
	Doc        string   `json:",omitzero" desc:"what the reader reads and what it refuses"`
}

// Formats is list_formats's result.
type Formats struct {
	Formats []FormatInfo `desc:"the formats a file the person opens can be in, by name"`
	Note    string       `desc:"how they are read"`
}

// formatsOf lists a registry's formats.
func formatsOf(reg *decode.Registry) (out Formats) {
	out.Note = "a file is recognised by its first bytes, never by its name; PNG and JPEG are streamed into tiles, every other format is read whole into a stack"
	for _, f := range reg.Formats() {
		out.Formats = append(out.Formats, FormatInfo{Name: f.Name, Title: f.Title, Extensions: f.Extensions, Version: f.Version, Doc: f.Doc})
	}
	return
}

// formatTitles is the registry's formats for a sentence: their titles,
// separated by semicolons, since a title may hold a comma.
func formatTitles(reg *decode.Registry) string {
	fs := reg.Formats()
	titles := make([]string, len(fs))
	for i, f := range fs {
		titles[i] = f.Title
	}
	return strings.Join(titles, "; ")
}

// FileInfo is the file the image opened was read from.
type FileInfo struct {
	Name          string `desc:"the file's name, without its path"`
	Format        string `desc:"the format's name, as list_formats lists it"`
	FormatTitle   string `desc:"the format's name for a person"`
	Variant       string `json:",omitzero" desc:"the variant of the format the entry shown was read as, in the format's own word, where one reader's name covers several; absent otherwise"`
	ReaderVersion int    `desc:"the version of the reading the image came from"`
	Entry         string `desc:"the id of the entry shown"`
	EntryTitle    string `desc:"the entry shown, for a person"`
	Entries       int    `desc:"entries the file holds; get_entries lists them, set_entry shows another"`
	Held          bool   `json:",omitzero" desc:"the file's bytes are held, so set_entry reads another entry without the file being opened again; true when it has more than one entry"`
}

// fileInfo is the file as get_view reports it; nil when none is open.
func fileInfo(f *openedFile) *FileInfo {
	if f == nil {
		return nil
	}
	out := &FileInfo{Name: f.name, Format: f.format.Name, FormatTitle: f.format.Title, Variant: f.variant, ReaderVersion: f.format.Version,
		Entry: f.shown, Entries: len(f.entries), Held: f.switchable()}
	if i := f.index(f.shown); i >= 0 {
		out.EntryTitle = f.entries[i].Title
	}
	return out
}

// EntryInfo is one entry of the file open.
type EntryInfo struct {
	Index    int    `desc:"the entry's place in the file"`
	Id       string `desc:"the id set_entry takes"`
	Title    string `desc:"the entry for a person"`
	Detail   string `json:",omitzero" desc:"the format's own words for it"`
	Width    int    `json:",omitzero" desc:"columns, as the header says; absent when it does not"`
	Height   int    `json:",omitzero" desc:"rows, as the header says"`
	Planes   int    `json:",omitzero" desc:"planes of its stack, as the header says"`
	Kind     string `json:",omitzero" desc:"sample kind, as the header says"`
	Readable bool   `desc:"the reader reads it, limits aside"`
	Refusal  string `json:",omitzero" desc:"for an entry the reader does not read, which refusal opening it is: unsupported (it uses what the reader does not read), limit (it is over a bound of the reader's own) or malformed (it does not follow its format)"`
	Reason   string `json:",omitzero" desc:"why the reader does not read it"`
	Default  bool   `json:",omitzero" desc:"the entry a plain open reads"`
	Shown    bool   `json:",omitzero" desc:"the entry shown"`
}

// GetEntriesArgs names a page of the file's entries.
type GetEntriesArgs struct {
	From  int `json:",omitzero" desc:"the first entry to list, by its place in the file"`
	Count int `json:",omitzero" desc:"how many to list, at most 256; 0 lists 64"`
}

// EntriesPage is get_entries's result.
type EntriesPage struct {
	File      string      `desc:"the file's name"`
	Format    string      `desc:"the format's name"`
	Shown     string      `desc:"the id of the entry shown"`
	Total     int         `desc:"entries the file holds"`
	From      int         `desc:"the place of the first entry listed"`
	Entries   []EntryInfo `desc:"the entries from From, in the file's order"`
	Truncated bool        `json:",omitzero" desc:"the file has entries past the ones listed; ask again from the next"`
}

// entriesPage lists count entries of f from from.
func entriesPage(f *openedFile, from, count int) (out EntriesPage) {
	out = EntriesPage{File: f.name, Format: f.format.Name, Shown: f.shown, Total: len(f.entries), From: from}
	to := min(len(f.entries), from+count)
	for k := from; k < to; k++ {
		e := f.entries[k]
		info := EntryInfo{Index: k, Id: e.Id, Title: e.Title, Detail: e.Detail, Width: e.Width, Height: e.Height, Planes: e.Planes,
			Readable: e.Readable, Reason: e.Reason, Default: e.Default, Shown: e.Id == f.shown}
		if !e.Readable {
			info.Refusal = refusalName(e.Refusal)
		}
		if e.Kind != 0 {
			info.Kind = e.Kind.String()
		}
		out.Entries = append(out.Entries, info)
	}
	out.Truncated = to < len(f.entries)
	return
}

// SetEntryArgs names the entry to show.
type SetEntryArgs struct {
	Id string `desc:"the entry's id, from get_entries"`
}

// entryOperations adds the formats' and the entries' operations to the
// catalog (ADR-0398 §M5).
func entryOperations(s *appops.Set[*App, opsSnap]) {
	appops.Query(s, app.OperationSpec{Name: opListFormats, Version: 1,
		Summary: "list the formats this host reads — a file the person opens can be in one of them: name, title, the endings their files carry, the reader's version and what it reads and refuses",
		Reads:   []string{resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out Formats, err error) {
			return formatsOf(sn.registry), nil
		})

	needsFile := func(sn opsSnap) (ok bool, reason string) {
		if sn.file == nil {
			return false, "no file is open: the person opens one with Open…; a sample has no entries"
		}
		return true, ""
	}
	appops.Query(s, app.OperationSpec{Name: opGetEntries, Version: 1,
		Summary: "list the entries of the file open — the images it holds, a header unit or an array each — with their size, planes and kind as the header says, whether the reader reads them or why not, and which is shown; at most 256 at once",
		Reads:   []string{resImage}, Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetEntriesArgs) (out EntriesPage, err error) {
			if sn.file == nil {
				return out, errNoFile
			}
			if in.From < 0 || in.From >= len(sn.file.entries) {
				return out, app.RefuseOperation("entry " + strconv.Itoa(in.From) + " is outside the file, whose entries are 0 to " +
					strconv.Itoa(len(sn.file.entries)-1))
			}
			count := in.Count
			if count <= 0 {
				count = 64
			}
			return entriesPage(sn.file, in.From, min(count, entriesPageMax)), nil
		})
	s.Available(opGetEntries, needsFile)

	appops.Command(s, app.OperationSpec{Name: opSetEntry, Version: 1,
		Summary: "show another entry of the file open, read from the bytes the window holds, in place of the image opened; the call settles when it is shown",
		Effect:  app.OperationEffectDocument, Writes: []string{resImage, resView}, Agents: true, Async: true,
		Gesture: "the Entry picker of a file with more than one", Rebuild: app.OperationRebuildRefold, Fork: app.OperationForkPerInstance,
		Follows: []string{"the entry opens as a file opens: its default selection, fitted, under the default recipe; the regions and the results stay",
			"an entry the reader does not read is refused with its reason; one it refuses while reading — over a limit, malformed — leaves the entry shown, and the outcome's Error says why",
			"read_pixels, measure and send_to_play name the entry their selection is of"}},
		func(inst *App, _ app.OperationCall, in SetEntryArgs) (out appops.None, err error) {
			err = inst.setEntry(in.Id)
			return
		})
	appops.Settles(s, opSetEntry, settleSelection)
	s.Available(opSetEntry, func(sn opsSnap) (ok bool, reason string) {
		if ok, reason = needsFile(sn); !ok {
			return
		}
		if !sn.file.switchable() {
			return false, "the file open has one entry"
		}
		return true, ""
	})
}
