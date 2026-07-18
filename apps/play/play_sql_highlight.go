package play

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// sqlSemanticQuiescence is how long the editor buffer must sit unchanged
// before the semantic pass launches. Long enough that continuous typing
// never pays the parse; short enough that colors upgrade as soon as the
// user pauses to read.
const sqlSemanticQuiescence = 400 * time.Millisecond

// sqlSemanticHl is the ADR-0130 L2 tier: it upgrades the editor's lex-only
// colors to full semantic ones (table/column/alias/CTE names) once the
// buffer goes quiescent. The expensive part — highlight.Highlight, a full
// nanopass.Parse + CST walk measured at ~70 ms for a 2.5 KB buffer — runs
// on a bgjob worker goroutine and MUST stay off the render thread (ADR-0130
// §Consequences); only span→CodeViewJob serialization happens here.
//
// Supersession is by content: the run's bgjob Tag carries the exact buffer
// text it parsed, and a drained result is installed only while the buffer
// still equals it. An edit therefore falls back to the lex tier the same
// frame (the caller's fallback path) and the stale result is dropped on
// arrival. All methods are render-thread-only; the zero value is ready.
type sqlSemanticHl struct {
	runner bgjob.Runner[[]highlight.Span]

	// lastSrc/lastEditAt implement quiescence detection: lastSrc is the
	// buffer content observed on the previous frame, lastEditAt the time it
	// last differed. First observation counts as an edit, so a freshly
	// seeded buffer (BOXER_PLAY_SQL, session restore) upgrades one
	// quiescence window after the first frame — uniform and imperceptible.
	lastSrc    string
	lastEditAt time.Time

	// job is the installed semantic CodeViewJob describing jobSrc.
	job    typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	jobSrc string
	jobOk  bool

	// now/parse are injection points for tests; nil means time.Now /
	// highlight.Highlight.
	now   func() time.Time
	parse func(string) []highlight.Span
}

// jobFor returns the semantic job when one is installed for exactly this
// buffer content, maintaining the whole tier as a side effect: edit
// tracking, draining a finished background parse, and launching a new one
// on quiescence. ok == false means the caller should fall back to the
// lex tier.
func (s *sqlSemanticHl) jobFor(src string) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	if s.now == nil {
		s.now = time.Now
	}
	if s.parse == nil {
		s.parse = highlight.Highlight
	}

	if src != s.lastSrc {
		s.lastSrc = src
		s.lastEditAt = s.now()
	}

	// Drain a finished run. The tag is the text the worker parsed; a result
	// for anything but the current buffer is stale — drop it (the runner is
	// consume-once, so this also frees the slot for a fresh launch).
	if spans, tag, done := s.runner.TakeResult(); done && tag == src {
		s.job = codeview.BuildSqlFromSpans(src, *spans)
		s.jobSrc = src
		s.jobOk = true
	}

	if s.jobOk && s.jobSrc == src {
		return s.job, true
	}

	// Launch on quiescence. Start refuses while a run is in flight — a
	// superseded run's result then frees the slot via the drain above, at
	// most one parse behind. An unparseable buffer is fine: Highlight
	// returns lex-tier spans, which install as a visual no-op and stop
	// relaunching for this content.
	if s.now().Sub(s.lastEditAt) >= sqlSemanticQuiescence {
		parse := s.parse
		s.runner.Start(nil, bgjob.Spec{
			Kind:  "play-sql-semantic-highlight",
			Title: "semantic SQL highlight",
			Tag:   src,
		}, func(_ context.Context) (*[]highlight.Span, error) {
			spans := parse(src)
			return &spans, nil
		})
	}
	return job, false
}
