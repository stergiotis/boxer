package keelsonsql

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// unquoteString decodes a ClickHouse single-quoted string literal: the
// quotes are dropped, a doubled quote is one quote, and a backslash escape
// is decoded as ClickHouse decodes it (decodeEscapes).
func unquoteString(s string) string {
	if len(s) < 2 {
		return s
	}
	return decodeEscapes(s[1:len(s)-1], true)
}

// decodeEscapes undoes ClickHouse's backslash escapes in s, as its parser
// does for a string literal and its escaped text format does for a query
// parameter's value: \b \f \n \r \t \v \a \e and \0 are their control
// characters, \xHH is a byte and \N is nothing. An escape of any other
// character keeps the backslash — 'a\%' is a, backslash, percent, for LIKE —
// except that \\ \' \" \` and \/ are the character alone. With
// doubledQuote, a doubled single quote is one, as inside a literal.
func decodeEscapes(s string, doubledQuote bool) string {
	if !strings.ContainsAny(s, `\'`) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && doubledQuote && i+1 < len(s) && s[i+1] == '\'':
			i++
			sb.WriteByte('\'')
		case c != '\\' || i+1 == len(s):
			sb.WriteByte(c)
		default:
			i++
			e := s[i]
			switch e {
			case 'x':
				if i+2 < len(s) {
					if b, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
						sb.WriteByte(byte(b))
						i += 2
						continue
					}
				}
				sb.WriteString(`\x`)
			case 'N':
			case '\\', '\'', '"', '`', '/':
				sb.WriteByte(e)
			default:
				if d, isControl := controlEscape(e); isControl {
					sb.WriteByte(d)
				} else {
					sb.WriteByte('\\')
					sb.WriteByte(e)
				}
			}
		}
	}
	return sb.String()
}

func controlEscape(e byte) (d byte, ok bool) {
	ok = true
	switch e {
	case 'a':
		d = '\a'
	case 'b':
		d = '\b'
	case 'e':
		d = 0x1b
	case 'f':
		d = '\f'
	case 'n':
		d = '\n'
	case 'r':
		d = '\r'
	case 't':
		d = '\t'
	case 'v':
		d = '\v'
	case '0':
		d = 0
	default:
		ok = false
	}
	return
}

// paramText is a query parameter's value as ClickHouse reads it: in its
// escaped text format, where \t is a tab and a raw tab or newline ends the
// value — a value holding one does not parse.
func paramText(v string) (text string, err error) {
	if strings.ContainsAny(v, "\t\n") {
		return "", eb.Build().Str("value", v).Errorf("keelsonsql: a query parameter's value holds a raw tab or newline; escape it as \\t or \\n")
	}
	return decodeEscapes(v, false), nil
}

// TopLevelSets returns the `SET …;` statements in front of a parsed
// statement — the prelude ClickHouse runs before it — or nil.
func TopLevelSets(pr *nanopass.ParseResult) (sets []grammar1.ISetStmtContext) {
	qs, ok := pr.Tree.(*grammar1.QueryStmtContext)
	if !ok {
		return nil
	}
	if q, isQuery := qs.Query().(*grammar1.QueryContext); isQuery && q != nil {
		return q.AllSetStmt()
	}
	if ins := pr.InsertStmt(); ins != nil {
		return ins.AllSetStmt()
	}
	return nil
}

// PreludeParams folds the `SET param_<name> = <value>;` lines of sets into a
// copy of params, as ClickHouse binds them: a later SET wins over an earlier
// one and over the request's own param_<name>. A string literal's value is
// the literal decoded, which paramText then reads again as the parameter's
// text, as ClickHouse does. A SET of anything but a parameter is skipped;
// whether it is allowed is the caller's policy.
func PreludeParams(sets []grammar1.ISetStmtContext, params map[string]string) (out map[string]string, err error) {
	if len(sets) == 0 {
		return params, nil
	}
	out = make(map[string]string, len(params)+len(sets))
	for k, v := range params {
		out[k] = v
	}
	for _, st := range sets {
		for _, se := range st.SettingExprList().AllSettingExpr() {
			slot, isParam := strings.CutPrefix(nanopass.DecodeIdentifier(se.Identifier().GetText()), "param_")
			if !isParam {
				continue
			}
			var v string
			v, err = settingText(se.SettingValue())
			if err != nil {
				return nil, eb.Build().Str("param", slot).Errorf("%w", err)
			}
			out[slot] = v
		}
	}
	return
}

// settingText is a SET's literal value as text: a string decoded, a number
// as written, a hexadecimal one in decimal.
func settingText(sv grammar1.ISettingValueContext) (text string, err error) {
	sl, ok := sv.(*grammar1.SettingLiteralContext)
	if !ok {
		return "", eb.Build().Str("value", sv.GetText()).Errorf("keelsonsql: a SET param_ value must be a literal")
	}
	lit := sl.Literal()
	switch {
	case lit.STRING_LITERAL() != nil:
		return unquoteString(lit.GetText()), nil
	case lit.NumberLiteral() != nil:
		text = lit.GetText()
		if nl := lit.NumberLiteral(); nl.HEXADECIMAL_LITERAL() != nil {
			neg := nl.DASH() != nil
			digits := nl.HEXADECIMAL_LITERAL().GetText()
			u, pErr := strconv.ParseUint(digits, 0, 64)
			if pErr != nil {
				return "", eb.Build().Str("value", text).Errorf("keelsonsql: a SET param_ value does not read as a number: %w", pErr)
			}
			text = strconv.FormatUint(u, 10)
			if neg {
				text = "-" + text
			}
		}
		return text, nil
	default:
		return "", eb.Build().Str("value", lit.GetText()).Errorf("keelsonsql: a SET param_ value cannot be NULL")
	}
}
