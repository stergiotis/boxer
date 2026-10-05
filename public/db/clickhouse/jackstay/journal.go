package jackstay

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// JournalEntry is one line of the sync journal. A "start" entry records, once
// per table and run, the settings the table was begun under and whether the
// run owns the target table's rows; a "chunk" entry records a chunk whose
// copy was verified, with the source digest it was copied at; an "attempt"
// entry records a copy begun into rows the run does not own, which a resumed
// run must not repeat.
type JournalEntry struct {
	Run   string    `json:"run"`
	Table string    `json:"table"`
	Event string    `json:"event"`
	At    time.Time `json:"at"`
	// Owned (start): the target held nothing of the run's when it began, or
	// the run was told to replace; either way the run may clear what it
	// copies. Repair owns only the leaves its diff showed, so it records
	// false.
	Owned bool   `json:"owned,omitempty"`
	Chunk string `json:"chunk,omitempty"`
	// Mode (start, chunk) and Existing (start): the settings the table was
	// begun under. A resumed run must be given the same ones, or restart.
	Mode     string `json:"mode,omitempty"`
	Existing string `json:"existing,omitempty"`
	// Filter (start): the row filter the table was begun under
	// (ADR-0271 §SD1).
	Filter string `json:"filter,omitempty"`
	// SampleNum, SampleDen (start): the sample fraction of a sample sync.
	// Absent from start entries written before they were recorded.
	SampleNum uint32 `json:"sampleNum,omitempty"`
	SampleDen uint32 `json:"sampleDen,omitempty"`
	// N, Kd, Rd (chunk): the source chunk's digest totals when copied.
	N  uint64 `json:"n,omitempty"`
	Kd uint64 `json:"kd,omitempty"`
	Rd uint64 `json:"rd,omitempty"`
}

// isAt reports whether a chunk entry was copied at the source digest d.
func (inst JournalEntry) isAt(d leafDigest) (at bool) {
	return inst.N == d.n && inst.Kd == d.kd && inst.Rd == d.rd
}

type journalKey struct {
	table string
	chunk string
}

// Journal is the local, append-only record of one sync run (ADR-0259 §SD5):
// what a resumed run may skip, and which tables it owns. Lines of other runs in
// the same file are ignored. Each line is durable before the call returns, so
// a chunk is never recorded as done before it is.
type Journal struct {
	files     FilesI
	name      string
	run       string
	writable  bool
	start     map[string]JournalEntry
	done      map[journalKey]JournalEntry
	attempted map[journalKey]bool
}

// OpenJournal reads the entries of run from path, if the file exists, and
// opens it for appending.
func OpenJournal(path string, run string) (j *Journal, err error) {
	return OpenJournalIn(OsFiles{}, path, run)
}

// OpenJournalIn is [OpenJournal] over files. A last line a crash cut short
// is repaired before the first append, so the next entry starts a line of
// its own: a torn line that does not decode is cut off, and one that decodes
// but lacks its newline gets it.
func OpenJournalIn(files FilesI, name string, run string) (j *Journal, err error) {
	var tail journalTail
	j, tail, err = readJournal(files, name, run)
	if err != nil {
		return
	}
	switch {
	case tail.torn:
		err = files.WriteFile(name, tail.data[:tail.at])
	case tail.unterminated:
		err = files.AppendFile(name, []byte{'\n'})
	}
	if err != nil {
		err = eb.Build().Str("name", name).Errorf("unable to repair the journal's last line: %w", err)
		return
	}
	j.writable = true
	return
}

// ReadJournal reads the entries of run from path without opening it for
// writing; a missing file is an empty journal. Record methods fail on it.
func ReadJournal(path string, run string) (j *Journal, err error) {
	return ReadJournalIn(OsFiles{}, path, run)
}

// ReadJournalIn is [ReadJournal] over files.
func ReadJournalIn(files FilesI, name string, run string) (j *Journal, err error) {
	j, _, err = readJournal(files, name, run)
	return
}

// journalTail describes the end of a journal file: a last line that does not
// decode (torn, starting at offset at), or one that decodes without its
// newline (unterminated).
type journalTail struct {
	data         []byte
	at           int
	torn         bool
	unterminated bool
}

func readJournal(files FilesI, name string, run string) (j *Journal, tail journalTail, err error) {
	j = &Journal{files: files, name: name, run: run, start: make(map[string]JournalEntry, 8), done: make(map[journalKey]JournalEntry, 64), attempted: make(map[journalKey]bool, 8)}
	var data []byte
	data, err = files.ReadFile(name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = nil
		return
	case err != nil:
		err = eb.Build().Str("name", name).Errorf("unable to open journal: %w", err)
		return
	}
	tail.data = data
	line := 0
	for at := 0; at < len(data); {
		end := bytes.IndexByte(data[at:], '\n')
		last := end < 0
		if last {
			end = len(data)
		} else {
			end += at
		}
		line++
		var e JournalEntry
		if uerr := json.Unmarshal(data[at:end], &e); uerr != nil {
			// A line cut short by a crash is the last line; anything
			// else is not a journal this code wrote.
			if last || end+1 == len(data) {
				tail.at, tail.torn = at, true
				return
			}
			err = eb.Build().Str("name", name).Int("line", line).Errorf("unable to decode journal line: %w", uerr)
			return
		}
		tail.unterminated = last
		at = end + 1
		if e.Run != run {
			continue
		}
		switch e.Event {
		case "start":
			j.start[e.Table] = e
		case "chunk":
			j.done[journalKey{e.Table, e.Chunk}] = e
		case "attempt":
			j.attempted[journalKey{e.Table, e.Chunk}] = true
		}
	}
	return
}

// DoneChunks counts a table's verified chunks in this run and the source rows
// they held.
func (inst *Journal) DoneChunks(table string) (chunks int, rows uint64) {
	for k, e := range inst.done {
		if k.table == table {
			chunks++
			rows += e.N
		}
	}
	return
}

// Close ends appending; every line is already durable.
func (inst *Journal) Close() (err error) {
	inst.writable = false
	return
}

func (inst *Journal) write(e JournalEntry) (err error) {
	if !inst.writable {
		return eb.Build().Str("name", inst.name).Errorf("journal is not open for writing")
	}
	e.Run = inst.run
	var data []byte
	data, err = json.Marshal(e, json.Deterministic(true))
	if err != nil {
		return eh.Errorf("unable to encode journal entry: %w", err)
	}
	data = append(data, '\n')
	err = inst.files.AppendFile(inst.name, data)
	if err != nil {
		err = eb.Build().Str("name", inst.name).Errorf("unable to write journal: %w", err)
	}
	return
}

// Started returns the run's start entry for table, if the run has begun it.
func (inst *Journal) Started(table string) (e JournalEntry, started bool) {
	e, started = inst.start[table]
	return
}

// RecordStart records that the run begins table under ts and the row filter,
// owning the target's rows (its slice, under a filter) or not.
func (inst *Journal) RecordStart(table string, owned bool, ts TableSync, filter string, now time.Time) (err error) {
	e := JournalEntry{Table: table, Event: "start", Owned: owned, Mode: ts.Mode.String(), Existing: ts.Existing.String(), Filter: filter, At: now.UTC()}
	if ts.Mode == SyncModeSample {
		e.SampleNum, e.SampleDen = ts.SampleNum, ts.SampleDen
	}
	err = inst.write(e)
	if err == nil {
		inst.start[table] = e
	}
	return
}

// Done returns the entry of a verified chunk of this run.
func (inst *Journal) Done(table string, chunk string) (e JournalEntry, ok bool) {
	e, ok = inst.done[journalKey{table, chunk}]
	return
}

func (inst *Journal) RecordChunk(table string, chunk string, mode SyncModeE, d leafDigest, now time.Time) (err error) {
	e := JournalEntry{Table: table, Event: "chunk", Chunk: chunk, Mode: mode.String(), N: d.n, Kd: d.kd, Rd: d.rd, At: now.UTC()}
	err = inst.write(e)
	if err == nil {
		inst.done[journalKey{table, chunk}] = e
	}
	return
}

// Attempted reports whether a copy into rows the run does not own was begun
// for the chunk.
func (inst *Journal) Attempted(table string, chunk string) (ok bool) {
	return inst.attempted[journalKey{table, chunk}]
}

func (inst *Journal) RecordAttempt(table string, chunk string, now time.Time) (err error) {
	err = inst.write(JournalEntry{Table: table, Event: "attempt", Chunk: chunk, At: now.UTC()})
	if err == nil {
		inst.attempted[journalKey{table, chunk}] = true
	}
	return
}
