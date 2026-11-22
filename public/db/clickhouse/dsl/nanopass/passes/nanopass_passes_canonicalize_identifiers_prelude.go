package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// spellPreludeSettingNames spells the session-setting names of the `SET …;`
// prelude as CanonicalizeIdentifiers spells a setting name in the body: bare,
// and double-quoted when the name lexes as a keyword (`SET "limit" = 1;`).
//
// The prelude lines are split off by [grammarenv.Extract] before any pass
// sees the body, so the body walk never reaches them; this edits the
// environment [grammarenv.Environment.Integrate] re-emits them from. Only a
// setting's Name changes — its map key and its Raw value stay as written.
//
// Params are left alone. A `param_` name never lexes as a keyword, and a name
// that binds a `{name:Type}` slot without the prefix is matched to the slot
// by its spelling, so quoting it would turn the binding into a session
// setting.
func spellPreludeSettingNames(e *grammarenv.Environment) {
	if e == nil {
		return
	}
	for k, s := range e.SessionSettings {
		spelled := settingNameSpelling(s.Name)
		if spelled != s.Name {
			s.Name = spelled
			e.SessionSettings[k] = s
		}
	}
}

// settingNameSpelling returns name double-quoted when it is one bare word the
// lexer reads as something other than an identifier — a keyword, or the
// boolean literals — and name unchanged otherwise. A quoted name is already an
// identifier.
func settingNameSpelling(name string) (spelled string) {
	spelled = name
	if name == "" || name[0] == '"' || name[0] == '`' {
		return
	}
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(name))
	lexer.RemoveErrorListeners()
	tok := lexer.NextToken()
	if tok.GetTokenType() == grammar1.ClickHouseLexerIDENTIFIER ||
		tok.GetText() != name ||
		lexer.NextToken().GetTokenType() != antlr.TokenEOF {
		return
	}
	spelled = nanopass.QuoteIdentifier(name)
	return
}
