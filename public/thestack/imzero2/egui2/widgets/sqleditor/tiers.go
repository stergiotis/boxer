package sqleditor

// The syntax-colour tiers (ADR-0130 L1/L2, moved here by ADR-0147 §SD1).
//
// L1 is the lexer, cheap enough to rebuild on every edit. L2 upgrades to full
// semantic colour (table/column/alias/CTE names) once the buffer goes
// quiescent, and runs the expensive part on a background worker.

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
)

// semanticQuiescence is how long the buffer must sit unchanged before the
// semantic pass launches. Long enough that continuous typing never pays the
// parse; short enough that colours upgrade as soon as the user pauses to read.
const semanticQuiescence = 400 * time.Millisecond

// semanticTier is the ADR-0130 L2 tier: it upgrades the lex-only colours to
// full semantic ones once the buffer goes quiescent. The expensive part —
// highlight.Highlight, a full nanopass.Parse + CST walk measured at ~70 ms for
// a 2.5 KB buffer — runs on a bgjob worker goroutine and MUST stay off the
// render thread (ADR-0130 §Consequences); only span→CodeViewJob serialization
// happens here.
//
// Supersession is by content: the run's bgjob Tag carries the exact buffer
// text it parsed, and a drained result is installed only while the buffer
// still equals it. An edit therefore falls back to the lex tier the same
// frame (the caller's fallback path) and the stale result is dropped on
// arrival. All methods are render-thread-only; the zero value is ready.
type semanticTier struct {
	runner bgjob.Runner[[]highlight.Span]

	// lastSrc/lastEditAt implement quiescence detection: lastSrc is the
	// buffer content observed on the previous frame, lastEditAt the time it
	// last differed. First observation counts as an edit, so a freshly
	// seeded buffer upgrades one quiescence window after the first frame —
	// uniform and imperceptible.
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
func (inst *semanticTier) jobFor(src string) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	if inst.now == nil {
		inst.now = time.Now
	}
	if inst.parse == nil {
		inst.parse = highlight.Highlight
	}

	if src != inst.lastSrc {
		inst.lastSrc = src
		inst.lastEditAt = inst.now()
	}

	// Drain a finished run. The tag is the text the worker parsed; a result
	// for anything but the current buffer is stale — drop it (the runner is
	// consume-once, so this also frees the slot for a fresh launch).
	if spans, tag, done := inst.runner.TakeResult(); done && tag == src {
		inst.job = codeview.BuildSqlFromSpans(src, *spans)
		inst.jobSrc = src
		inst.jobOk = true
	}

	if inst.jobOk && inst.jobSrc == src {
		return inst.job, true
	}

	// Launch on quiescence. Start refuses while a run is in flight — a
	// superseded run's result then frees the slot via the drain above, at
	// most one parse behind. An unparseable buffer is fine: Highlight
	// returns lex-tier spans, which install as a visual no-op and stop
	// relaunching for this content.
	if inst.now().Sub(inst.lastEditAt) >= semanticQuiescence {
		parse := inst.parse
		inst.runner.Start(nil, bgjob.Spec{
			Kind:  "sqleditor-semantic-highlight",
			Title: "semantic SQL highlight",
			Tag:   src,
		}, func(_ context.Context) (*[]highlight.Span, error) {
			spans := parse(src)
			return &spans, nil
		})
	}
	return job, false
}

// highlightJob returns the retained CodeViewJob for the buffer, rebuilding the
// lex tier only when the buffer changed since the last frame (~26 µs per
// rebuild at CTE sizes; idle frames re-splice the retained holder for free). An
// empty buffer renders plain — the hint text has no bytes to colour.
func (inst *Editor) highlightJob(src string) (job typed.RetainedFffiHolderTyped[c.CodeViewJobS], ok bool) {
	if src == "" {
		return job, false
	}
	// L2: a quiescent buffer gets the semantic tier (async; see semanticTier).
	// While typing — or while the parse is still in flight — the lex tier
	// below answers.
	if sem, semOk := inst.sem.jobFor(src); semOk {
		return sem, true
	}
	if !inst.lexOk || inst.lexSrc != src {
		inst.lexJob = codeview.BuildSqlLex(src)
		inst.lexSrc = src
		inst.lexOk = true
	}
	return inst.lexJob, true
}
