package chstore

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// recentLogsColumnExprs gathers the envelope projections of a log row that
// the log reads share.
type recentLogsColumnExprs struct {
	id      string
	tsSec   string
	appId   string
	level   string
	caller  string
	service string
	message string
	errStr  string
	stack   string
}

func buildColumnExprs() (e recentLogsColumnExprs) {
	const (
		symLR      = "`tv:symbol:lr:lr:u64:1247:::0::data`"
		symValue   = "`tv:symbol:value:val:s:124::I:0::data`"
		symLRCard  = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
		strLR      = "`tv:stringArray:lr:lr:u64:1247:::0::data`"
		strValue   = "`tv:stringArray:value:val:sh:4::8:0::data`"
		strLRCard  = "`tv:stringArray:lrcard:lrcard:u64:4E:::0::data`"
		textLR     = "`tv:textArray:lr:lr:u64:1247:::0::data`"
		textValue  = "`tv:textArray:value:val:sh:5::7:0::data`"
		textLRCard = "`tv:textArray:lrcard:lrcard:u64:4E:::0::data`"
		idCol      = "`id:id:u64:47::0:`"
		tsCol      = "`ts:ts:z64:47::0:`"
	)
	e.id = idCol
	// `ts` is z64 — emitted by the leeway DDL pipeline as
	// DateTime64(9,'UTC') (nano precision). We coerce to unix-seconds
	// so the Go side can re-hydrate via time.Unix without the
	// timezone-decode ambiguity TabSeparated otherwise has. Sub-second
	// precision is discarded here by design; callers needing per-event
	// ordering fall back to the `id` column.
	e.tsSec = fmt.Sprintf("toUnixTimestamp(%s)", tsCol)
	// AppId is a Mixed-Low-Card-Ref membership: the mrhp array (high-
	// card parameter) is parallel to lmr (same length, same order), so
	// arrayFirst zips cleanly.
	e.appId = pickLcrString(symValue, symLR, symLRCard, vocab.MembRuntimeApp.GetId().Value())
	// Envelope LCR fields: value[] is parallel to *card[] (one entry
	// per attribute), but lr[] is a SUBSET array (only LCR attrs). We
	// map an LCR id → position in lr → position in value via cumulative
	// sums on lrcard.
	e.level = pickLcrString(symValue, symLR, symLRCard, vocab.MembLogLevel.GetId().Value())
	e.caller = pickLcrString(symValue, symLR, symLRCard, vocab.MembLogCaller.GetId().Value())
	e.service = pickLcrString(symValue, symLR, symLRCard, vocab.MembLogService.GetId().Value())
	e.message = pickLcrString(strValue, strLR, strLRCard, vocab.MembLogMessage.GetId().Value())
	e.errStr = pickLcrString(strValue, strLR, strLRCard, vocab.MembLogError.GetId().Value())
	e.stack = pickLcrString(textValue, textLR, textLRCard, vocab.MembLogStack.GetId().Value())
	return
}

// pickLcrString emits the expression that returns the value carried by
// the attribute tagged with low-card-ref `membershipId`. The leeway
// encoding is non-trivial: `value` has one entry per attribute, but
// `lr` only contains the LCR membership ids — different sizes (any
// attribute can also be HCR/LMR-tagged). The `lrcard` array is
// parallel to `value` and counts LCR memberships at each position; the
// running prefix-sum of lrcard yields the lr-index for each value
// position. Looking up our membership id in lr gives an lr-index;
// finding that index in the cumulative-sum array yields the value
// position. The whole pipeline is wrapped in an `if` on `indexOf > 0`
// falling back to the empty string, because `indexOf(lr, missing) = 0`
// would otherwise lead arrayElement to misbehave on edge cases.
func pickLcrString(valueArr, lrArr, lrCardArr string, membershipId uint64) (expr string) {
	idxInLr := fmt.Sprintf("indexOf(%s, %d)", lrArr, membershipId)
	expr = fmt.Sprintf("if(%s > 0, arrayElement(%s, indexOf(arrayCumSum(%s), %s)), '')",
		idxInLr, valueArr, lrCardArr, idxInLr)
	return
}

// unescapeTabSeparated reverses ClickHouse's TabSeparated string
// escaping: \\ → backslash, \t → tab, \n → newline, \0 → NUL, \' → quote,
// \b, \f, \r → their control characters — the set the server writes. Other
// backslash-escapes pass through unchanged so unknown sequences don't
// corrupt the data silently.
func unescapeTabSeparated(s string) (out string) {
	if !strings.ContainsRune(s, '\\') {
		out = s
		return
	}
	b := strings.Builder{}
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '0':
			b.WriteByte(0)
		case '\'':
			b.WriteByte('\'')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		default:
			b.WriteByte(s[i])
			b.WriteByte(s[i+1])
		}
		i++
	}
	out = b.String()
	return
}
