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
func unquoteString(s string) (text string, err error) {
	if len(s) < 2 {
		return s, nil
	}
	return decodeEscapes(s[1:len(s)-1], true)
}

// decodeEscapes undoes ClickHouse's backslash escapes in s, as its parser
// does for a string literal and its escaped text format does for a query
// parameter's value — a table measured against clickhouse-local for every
// printable character: \b \f \n \r \t \v \a \e and \0 are their control
// characters, \xHH is a byte and \N is nothing; \\ \' \" \` \/ and \= are the
// character alone; an escape of any other character keeps the backslash —
// 'a\%' is a, backslash, percent, for LIKE. A \x without two hex digits, or
// a trailing backslash, is not modelled (ClickHouse reads a garbage byte, or
// fails) and is ErrNotConstant. With doubledQuote, a doubled single quote
// is one, as inside a literal.
func decodeEscapes(s string, doubledQuote bool) (text string, err error) {
	if !strings.ContainsAny(s, `\'`) {
		return s, nil
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && doubledQuote && i+1 < len(s) && s[i+1] == '\'':
			i++
			sb.WriteByte('\'')
		case c != '\\':
			sb.WriteByte(c)
		case i+1 == len(s):
			return "", notConstant("a string ending in a backslash")
		default:
			i++
			e := s[i]
			switch e {
			case 'x':
				b, hErr := uint64(0), error(nil)
				if i+2 < len(s) {
					b, hErr = strconv.ParseUint(s[i+1:i+3], 16, 8)
				}
				if i+2 >= len(s) || hErr != nil {
					return "", notConstant("a string with a \\x escape that is not two hex digits")
				}
				sb.WriteByte(byte(b))
				i += 2
			case 'N':
			case '\\', '\'', '"', '`', '/', '=':
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
	return sb.String(), nil
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
	return decodeEscapes(v, false)
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
// text, as ClickHouse does; a number's is its value as ClickHouse spells it
// (1.50 binds 1.5, 1e3 binds 1000.). A value that is not a scalar literal —
// an array, a tuple, NULL — is not modelled: its slot is returned in opaque
// instead, so that only a constant that reads it fails, and the statement
// otherwise runs. A SET of anything but a parameter is skipped; whether it
// is allowed is the caller's policy.
func PreludeParams(sets []grammar1.ISetStmtContext, params map[string]string) (out map[string]string, opaque map[string]struct{}, err error) {
	if len(sets) == 0 {
		return params, nil, nil
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
			v, scalar, sErr := settingText(se.SettingValue())
			if sErr != nil {
				return nil, nil, eb.Build().Str("param", slot).Errorf("%w", sErr)
			}
			if !scalar {
				delete(out, slot)
				if opaque == nil {
					opaque = make(map[string]struct{})
				}
				opaque[slot] = struct{}{}
				continue
			}
			delete(opaque, slot)
			out[slot] = v
		}
	}
	return
}

// settingText is a SET's scalar literal value as the text ClickHouse binds:
// a string decoded, a number as its value spelled — an integer in decimal,
// a float as FloatFieldText. scalar is false for any other value.
func settingText(sv grammar1.ISettingValueContext) (text string, scalar bool, err error) {
	sl, ok := sv.(*grammar1.SettingLiteralContext)
	if !ok {
		return "", false, nil
	}
	lit := sl.Literal()
	switch {
	case lit.STRING_LITERAL() != nil:
		text, err = unquoteString(lit.GetText())
		return text, err == nil, err
	case lit.NumberLiteral() != nil:
		c, nErr := numberConstant(lit.GetText())
		if nErr != nil {
			return "", false, eb.Build().Str("value", lit.GetText()).Errorf("keelsonsql: a SET param_ value does not read as a number: %w", nErr)
		}
		if c.Type.IsFloat() {
			return FloatFieldText(c.Float), true, nil
		}
		text, err = c.Text()
		return text, true, err
	}
	return "", false, nil
}
