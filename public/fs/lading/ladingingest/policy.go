// Package ladingingest walks an `fs.FS` and writes it into the lading store as
// one snapshot (ADR-0198 §SD5, §SD6).
//
// One call to [Snapshot] is one voyage: it fixes the snapshot's instant and
// its expiry, walks the tree once, writes an entry row per node and a block
// row per stored block, and signs the whole thing off with the root row —
// which it writes last, after everything else is durable. A walk that fails
// part way leaves rows nothing can see and `TTL` removes them unaided; a retry
// is a new snapshot, never a repair of the old one.
package ladingingest

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TtlClassE is a mount's retention class, in whole days.
//
// A class rather than a free duration, and whole days rather than any
// duration, because the tables partition by expiry *day* and drop whole parts:
// every row of a partition has to expire at the same instant for that to work.
// A sub-day class would leave partitions partially expired, and a partially
// expired part keeps its expired rows through every background merge under
// `ttl_only_drop_parts = 1` — only an explicit OPTIMIZE FINAL clears them
// (measured, ADR-0198 `## Updates` 2026-08-19). The macro's `expiresAt > now()`
// cutoff still hides them from results; what leaks is disk.
//
// Effective retention is therefore `[days, days + 1)`: expiry is the end of
// the snapshot's day plus the class.
type TtlClassE uint16

// The classes ADR-0198 §SD1 names. A store may use another whole-day value;
// these are the ones with a spelling everything agrees on.
const (
	TtlClass7d  TtlClassE = 7
	TtlClass30d TtlClassE = 30
	TtlClass90d TtlClassE = 90
)

// String is the class as it is recorded on a snapshot's root row and in a
// mount's policy record — "7d", "30d", "90d".
func (inst TtlClassE) String() string { return fmt.Sprintf("%dd", uint16(inst)) }

// expiryOf is `toStartOfDay(snap) + 1 DAY + class` (ADR-0198 §SD4): the end of
// the snapshot's UTC day, plus the class. Computed from the calendar date
// rather than by rounding a duration, so it lands on midnight UTC for every
// input and every partition of a class expires as a unit.
func (inst TtlClassE) expiryOf(snap time.Time) time.Time {
	y, m, d := snap.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1+int(inst))
}

// TextRuleE decides which files are cut at newlines instead of at a fixed
// offset. The cut is what makes a line-oriented query over a file's blocks
// boundary-safe, so this is a correctness knob for the SQL surface, not a
// storage optimisation.
type TextRuleE uint8

const (
	// TextRuleNever cuts every file at fixed block boundaries. The right rule for
	// a mount of opaque payloads, and the one that costs nothing to decide.
	TextRuleNever TextRuleE = iota
	// TextRuleSniff calls a file text when its first 8 KiB decode as UTF-8 and
	// carry no NUL byte. It is the cheap heuristic, and it is deliberately not
	// extension-based: an extension says what a file is meant to be, and this
	// has to say what its bytes are.
	TextRuleSniff
)

// String is the rule as it is recorded on a snapshot's root row.
func (inst TextRuleE) String() string {
	switch inst {
	case TextRuleNever:
		return "never"
	case TextRuleSniff:
		return "sniff"
	}
	return "unknown"
}

// Policy is what one mount's snapshots are taken under: how long they are
// kept, how much content is stored, and how it is cut.
//
// It is recorded twice and the two are different things. [RecordPolicy] writes
// it to `boxer.facts` as the mount's *declared* policy — mutable runtime state
// that outlives every snapshot. [Snapshot] writes what it *applied* onto the
// snapshot's root row, so a snapshot stays interpretable after the declaration
// changes.
type Policy struct {
	// Ttl is the retention class. Zero is not a duration — see [Policy.check].
	Ttl TtlClassE
	// InlineMax is the size in bytes up to which a file's content is stored.
	// Above it the entry records `ref` and the content is fetched from the
	// live source on read.
	//
	// It is also the walker's memory bound per file: a stored file is held
	// whole while it is cut, because whether it is text is a property of the
	// whole file (see [Policy.Text]) and re-cutting a stream is not free. A
	// `ref` file is only streamed through the hasher, at one block at a time.
	InlineMax uint64
	// Text is the classification rule.
	Text TextRuleE
	// Profile fixes the table parameters — block size, granularity, whether
	// each block carries its own digest.
	Profile ladingschema.Profile
	// MetaOnly stores no content at all: every entry records `none` and no
	// block rows are written. A stat-only mount still answers `find`-shaped
	// questions, diffs by mtime and size, and costs one row per node.
	MetaOnly bool
}

// DefaultPolicy is a corpus-profile mount kept for 30 days, storing files up
// to 4 MiB and cutting text at newlines.
func DefaultPolicy() Policy {
	return Policy{
		Ttl:       TtlClass30d,
		InlineMax: 4 << 20,
		Text:      TextRuleSniff,
		Profile:   ladingschema.ProfileCorpus,
	}
}

// check refuses a policy that would write rows the store cannot retain or read
// back. Every condition here is one the tables cannot express and would
// therefore fail late, at merge time or at read time, rather than at the call.
func (inst Policy) check() (err error) {
	switch {
	case inst.Ttl == 0:
		err = eb.Build().Errorf("policy has no retention class; a zero class would expire every row at the end of the day after the walk")
	case inst.Profile.BlockSize == 0:
		err = eb.Build().Str("profile", inst.Profile.Name).
			Errorf("policy profile has no block size")
	case !inst.MetaOnly && inst.InlineMax == 0:
		err = eb.Build().Errorf("policy stores content but InlineMax is 0; set MetaOnly to take a stat-only snapshot")
	}
	return
}
