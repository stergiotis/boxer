package analysis

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// KindE classifies a statement by what it does to server state.
//
// The zero value is KindUnknown, so a KindE that was never assigned can
// never be mistaken for a read.
type KindE uint8

const (
	// KindUnknown means the statement's kind could not be established. It
	// is NOT a synonym for "harmless": see [ClassifyStatementKind] for the
	// obligation it puts on callers.
	KindUnknown KindE = iota
	// KindReadOnly means the statement was proven to be a read.
	KindReadOnly
	// KindMutating means the statement was recognised as changing server
	// state — data, schema, session, or a running query.
	KindMutating
)

var AllKinds = []KindE{KindUnknown, KindReadOnly, KindMutating}

func (inst KindE) String() (name string) {
	switch inst {
	case KindReadOnly:
		name = "read-only"
	case KindMutating:
		name = "mutating"
	default:
		name = "unknown"
	}
	return
}

// readOnlyLeadingTokens are statement-leading keywords whose statements read
// server state without changing it, but which Grammar1 cannot parse (it
// covers the SELECT surface only). EXPLAIN is here because it reports on the
// statement it wraps rather than executing it.
var readOnlyLeadingTokens = map[int]struct{}{
	grammar1.ClickHouseLexerEXPLAIN:  {},
	grammar1.ClickHouseLexerSHOW:     {},
	grammar1.ClickHouseLexerDESCRIBE: {},
	grammar1.ClickHouseLexerDESC:     {},
	grammar1.ClickHouseLexerEXISTS:   {},
}

// mutatingLeadingTokens are statement-leading keywords that change data,
// schema, session state, or a running query. The list is not exhaustive —
// keywords the lexer does not tokenise (GRANT, BACKUP, UNDROP, …) arrive as
// identifiers and classify as KindUnknown, which callers must already treat
// as mutating, so a gap here is safe rather than silent.
var mutatingLeadingTokens = map[int]struct{}{
	grammar1.ClickHouseLexerINSERT:   {},
	grammar1.ClickHouseLexerCREATE:   {},
	grammar1.ClickHouseLexerDROP:     {},
	grammar1.ClickHouseLexerALTER:    {},
	grammar1.ClickHouseLexerRENAME:   {},
	grammar1.ClickHouseLexerATTACH:   {},
	grammar1.ClickHouseLexerDETACH:   {},
	grammar1.ClickHouseLexerTRUNCATE: {},
	grammar1.ClickHouseLexerOPTIMIZE: {},
	grammar1.ClickHouseLexerKILL:     {},
	grammar1.ClickHouseLexerSET:      {},
	grammar1.ClickHouseLexerUSE:      {},
	grammar1.ClickHouseLexerSYSTEM:   {},
	grammar1.ClickHouseLexerDELETE:   {},
	grammar1.ClickHouseLexerUPDATE:   {},
	grammar1.ClickHouseLexerMOVE:     {},
}

// ClassifyStatementKind reports whether sql reads server state, changes it,
// or could not be established either way.
//
// Consumers MUST treat KindUnknown exactly as they treat KindMutating.
// The classification is default-deny: it answers KindReadOnly only where a
// read is provable, so every path that cannot prove one — unparseable input,
// an unrecognised statement, an oversized payload — lands on KindUnknown and
// must not be granted read-only treatment. Boxer states the kind; what to do
// about it (refuse, reroute, fan out to every replica) is the caller's
// policy.
//
// The proof has two tiers, because Grammar1 parses the SELECT surface and
// nothing else:
//
//   - A successful Grammar1 parse IS the read proof. Its top-level rule is
//     `setStmt* ctes? selectUnionStmt`, so anything it accepts is a SELECT,
//     a WITH…SELECT, or one of those behind a leading SET prelude. That
//     prelude configures the read rather than mutating data, so it stays
//     read-only; a SET on its own does not parse and is classified below.
//   - Everything else is classified by its leading keyword, taken from the
//     lexer so comments and whitespace ahead of it are skipped correctly.
//     Only the two tables above are recognised; anything else is unknown.
//
// Total: no input errors, and no input is rejected.
func ClassifyStatementKind(sql string) (kind KindE) {
	_, err := nanopass.Parse(sql)
	if err == nil {
		kind = KindReadOnly
		return
	}
	// The parse failed, so the read proof is unavailable. Fall back to the
	// leading keyword, under the same input bound Parse enforces — an input
	// past the guard is not worth lexing to answer "unknown".
	guardErr := nanopass.CheckInputGuards(sql)
	if guardErr != nil {
		kind = KindUnknown
		return
	}
	tokenType, ok := leadingTokenType(sql)
	if !ok {
		kind = KindUnknown
		return
	}
	if _, isRead := readOnlyLeadingTokens[tokenType]; isRead {
		kind = KindReadOnly
		return
	}
	if _, isMutating := mutatingLeadingTokens[tokenType]; isMutating {
		kind = KindMutating
		return
	}
	kind = KindUnknown
	return
}

// leadingTokenType returns the type of the first token of sql on the default
// channel — whitespace and comments live on the hidden channel and are
// skipped. ok is false for input that holds no token at all.
func leadingTokenType(sql string) (tokenType int, ok bool) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	// Classification is total and silent: undecodable input is an answer of
	// KindUnknown, not a diagnostic on stderr.
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	tok := stream.LT(1)
	if tok == nil || tok.GetTokenType() == antlr.TokenEOF {
		return
	}
	tokenType = tok.GetTokenType()
	ok = true
	return
}
