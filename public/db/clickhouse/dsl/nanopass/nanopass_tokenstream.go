package nanopass

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
)

// TokenStream is antlr's CommonTokenStream with its text built in one pass.
//
// antlr's GetTextFromInterval appends one token at a time to an immutable
// string, which copies everything emitted so far for every token: the bytes
// it allocates grow with the square of the interval. Every unedited rewriter
// reaches it (TokenStreamRewriter.GetText hands an empty program to the
// stream), so a pass that changes nothing paid that on every statement; on a
// statement of about twenty thousand tokens it ran a wasm module out of memory.
//
// The methods below replace the four that build text. All four are
// overridden, not only GetTextFromInterval: the embedded stream's own
// GetAllText and its siblings call its GetTextFromInterval directly, which an
// override on the outer type does not reach.
type TokenStream struct {
	*antlr.CommonTokenStream
}

var _ antlr.TokenStream = (*TokenStream)(nil)

// NewTokenStream is antlr.NewCommonTokenStream on the default channel, the
// one every parse in this package uses.
func NewTokenStream(lexer antlr.Lexer) (inst *TokenStream) {
	return &TokenStream{CommonTokenStream: antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)}
}

// GetTextFromInterval returns the text of the tokens in interval, both ends
// included, up to the first EOF — what antlr's method returns, without its
// quadratic copying. Unlike antlr's it leaves the stream's read cursor where
// it was; text does not depend on it.
func (inst *TokenStream) GetTextFromInterval(interval antlr.Interval) string {
	inst.Sync(interval.Stop)
	start, stop := interval.Start, interval.Stop
	if start < 0 || stop < 0 {
		return ""
	}
	tokens := inst.GetAllTokens()
	if stop >= len(tokens) {
		stop = len(tokens) - 1
	}
	var b strings.Builder
	for i := start; i <= stop; i++ {
		t := tokens[i]
		if t.GetTokenType() == antlr.TokenEOF {
			break
		}
		b.WriteString(t.GetText())
	}
	return b.String()
}

// GetAllText returns the text of the whole stream.
func (inst *TokenStream) GetAllText() string {
	inst.Fill()
	return inst.GetTextFromInterval(antlr.NewInterval(0, len(inst.GetAllTokens())-1))
}

// GetTextFromTokens returns the text from start to end, both included.
func (inst *TokenStream) GetTextFromTokens(start, end antlr.Token) string {
	if start == nil || end == nil {
		return ""
	}
	return inst.GetTextFromInterval(antlr.NewInterval(start.GetTokenIndex(), end.GetTokenIndex()))
}

// GetTextFromRuleContext returns the text of the tokens a rule context spans.
func (inst *TokenStream) GetTextFromRuleContext(interval antlr.RuleContext) string {
	return inst.GetTextFromInterval(interval.GetSourceInterval())
}
