package fairlead

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/zeebo/xxh3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// JournalDir switches the statement journal on (ADR-0367 §SD4).
var JournalDir = env.NewPath(env.Spec{
	Name:        "BOXER_FAIRLEAD_JOURNAL_DIR",
	Category:    env.CategoryDev,
	Description: "directory each process appends one JSON line per distinct SQL statement it sends to (file `<pid>-<start>.jsonl`); empty records nothing. Statement text is written as sent, bound values excluded and inlined literals included, so the directory is as sensitive as the queries",
})

// maxJournalStatements bounds one process's journal. A loop that inlines a
// fresh literal per statement would otherwise grow the file and the seen-set
// without limit; past the bound further distinct statements are counted and
// not written.
const maxJournalStatements = 1 << 16

// JournalEntry is one line of a journal: a statement text seen at a site.
type JournalEntry struct {
	Site string `json:"site"`
	Exit string `json:"exit"`
	SQL  string `json:"sql"`
}

// Journal is the pass that records each distinct statement once
// (ADR-0367 §SD4). It never changes a statement and never refuses one: a
// write that fails is counted and the statement goes out regardless.
//
// It records text, not shape. Two statements that differ in an inlined
// literal are two entries; folding them is the reader's business, done with
// the parser the journal deliberately does not link.
type Journal struct {
	mu      sync.Mutex
	w       io.Writer
	seen    map[uint64]struct{}
	dropped int64
	failed  int64
}

var _ PassI = (*Journal)(nil)

func NewJournal(w io.Writer) (inst *Journal) {
	inst = &Journal{w: w, seen: make(map[uint64]struct{}, 256)}
	return
}

func (inst *Journal) Lead(_ context.Context, st Statement) (sql string, err error) {
	sql = st.SQL
	h := xxh3.New()
	_, _ = h.WriteString(st.Site)
	_, _ = h.Write([]byte{0, byte(st.Exit)})
	_, _ = h.WriteString(st.SQL)
	key := h.Sum64()

	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, dup := inst.seen[key]; dup {
		return
	}
	if len(inst.seen) >= maxJournalStatements {
		inst.dropped++
		return
	}
	inst.seen[key] = struct{}{}
	line, mErr := json.Marshal(JournalEntry{Site: st.Site, Exit: st.Exit.String(), SQL: st.SQL})
	if mErr == nil {
		_, mErr = inst.w.Write(append(line, '\n'))
	}
	if mErr != nil {
		inst.failed++
	}
	return
}

// Stats reports how many distinct statements were written, how many were
// left out past the bound, and how many writes failed.
func (inst *Journal) Stats() (written int64, dropped int64, failed int64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	written = int64(len(inst.seen)) - inst.failed
	dropped = inst.dropped
	failed = inst.failed
	return
}

// ReadJournal yields the entries of one journal file in order. A line that
// does not decode ends the sequence with its error: a process killed
// mid-write leaves a torn last line, and what came before it is good.
func ReadJournal(r io.Reader) iter.Seq2[JournalEntry, error] {
	return func(yield func(JournalEntry, error) bool) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var e JournalEntry
			if err := json.Unmarshal(line, &e); err != nil {
				yield(JournalEntry{}, eh.Errorf("decode journal line: %w", err))
				return
			}
			if !yield(e, nil) {
				return
			}
		}
		if err := sc.Err(); err != nil {
			yield(JournalEntry{}, eh.Errorf("read journal: %w", err))
		}
	}
}

// installFromEnv installs a file-backed journal when [JournalDir] is set. It
// runs once, on the first statement, so a process that sends none creates no
// file. The file stays open for the life of the process.
func installFromEnv() {
	dir := JournalDir.Get()
	if dir == "" {
		return
	}
	_, err := installJournalIn(dir)
	if err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("fairlead: statement journal not started")
	}
}

func installJournalIn(dir string) (remove func(), err error) {
	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		err = eh.Errorf("create journal directory: %w", err)
		return
	}
	name := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".jsonl"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		err = eh.Errorf("open journal file: %w", err)
		return
	}
	uninstall := Install(NewJournal(f))
	remove = func() {
		uninstall()
		_ = f.Close()
	}
	return
}
