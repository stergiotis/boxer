package jackstay

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// JournalEntry is one line of the sync journal. A "start" entry records, once
// per table and run, whether the run owns the target table's rows; a "chunk"
// entry records a chunk whose copy was verified, with the source digest it was
// copied at; an "attempt" entry records a copy begun into rows the run does
// not own, which a resumed run must not repeat.
type JournalEntry struct {
	Run   string    `json:"run"`
	Table string    `json:"table"`
	Event string    `json:"event"`
	At    time.Time `json:"at"`
	// Owned (start): the target held nothing of the run's when it began, or
	// the run was told to replace; either way the run may clear what it
	// copies.
	Owned bool   `json:"owned,omitempty"`
	Chunk string `json:"chunk,omitempty"`
	Mode  string `json:"mode,omitempty"`
	// N, Kd, Rd (chunk): the source chunk's digest totals when copied.
	N  uint64 `json:"n,omitempty"`
	Kd uint64 `json:"kd,omitempty"`
	Rd uint64 `json:"rd,omitempty"`
}

type journalKey struct {
	table string
	chunk string
}

// Journal is the local, append-only record of one sync run (ADR-0259 §SD5):
// what a resumed run may skip, and which tables it owns. Lines of other runs in
// the same file are ignored. Each line is synced to disk before the call
// returns, so a chunk is never recorded as done before it is.
type Journal struct {
	path      string
	run       string
	f         *os.File
	start     map[string]JournalEntry
	done      map[journalKey]JournalEntry
	attempted map[journalKey]bool
}

// OpenJournal reads the entries of run from path, if the file exists, and
// opens it for appending.
func OpenJournal(path string, run string) (j *Journal, err error) {
	j, err = ReadJournal(path, run)
	if err != nil {
		return
	}
	j.f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to open journal for appending: %w", err)
	}
	return
}

// ReadJournal reads the entries of run from path without opening it for
// writing; a missing file is an empty journal. Record methods fail on it.
func ReadJournal(path string, run string) (j *Journal, err error) {
	j = &Journal{path: path, run: run, start: make(map[string]JournalEntry, 8), done: make(map[journalKey]JournalEntry, 64), attempted: make(map[journalKey]bool, 8)}
	var rf *os.File
	rf, err = os.Open(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err = nil
	case err != nil:
		err = eb.Build().Str("path", path).Errorf("unable to open journal: %w", err)
		return
	default:
		sc := bufio.NewScanner(rf)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for sc.Scan() {
			line++
			var e JournalEntry
			if uerr := json.Unmarshal(sc.Bytes(), &e); uerr != nil {
				// A line cut short by a crash is the last line; anything
				// else is not a journal this code wrote.
				if !sc.Scan() {
					break
				}
				_ = rf.Close()
				err = eb.Build().Str("path", path).Int("line", line).Errorf("unable to decode journal line: %w", uerr)
				return
			}
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
		_ = rf.Close()
		err = sc.Err()
		if err != nil {
			err = eb.Build().Str("path", path).Errorf("unable to read journal: %w", err)
			return
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

func (inst *Journal) Close() (err error) {
	if inst.f == nil {
		return
	}
	err = inst.f.Close()
	inst.f = nil
	return
}

func (inst *Journal) write(e JournalEntry) (err error) {
	if inst.f == nil {
		return eb.Build().Str("path", inst.path).Errorf("journal is not open for writing")
	}
	e.Run = inst.run
	var data []byte
	data, err = json.Marshal(e, json.Deterministic(true))
	if err != nil {
		return eh.Errorf("unable to encode journal entry: %w", err)
	}
	data = append(data, '\n')
	_, err = inst.f.Write(data)
	if err == nil {
		err = inst.f.Sync()
	}
	if err != nil {
		err = eb.Build().Str("path", inst.path).Errorf("unable to write journal: %w", err)
	}
	return
}

// Started reports whether the run has begun table, and if so whether it owns
// the target's rows.
func (inst *Journal) Started(table string) (owned bool, started bool) {
	e, started := inst.start[table]
	return e.Owned, started
}

func (inst *Journal) RecordStart(table string, owned bool, now time.Time) (err error) {
	e := JournalEntry{Table: table, Event: "start", Owned: owned, At: now.UTC()}
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
