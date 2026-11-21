package recordstore

import (
	"encoding/hex"
	"regexp"
	"strings"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TableLayout is the physical layout of a MergeTree-family table: the
// engine, the partition key and the sorting key. None of the three can be
// changed in place, so a table keeps the layout it was created with; a
// build that creates tables with another one reports the difference and
// leaves the table as it is (ADR-0397).
//
// The keys are compared reduced by [NormalizeKeyExpr], since the server
// spells them with backticks and without the parentheses of a tuple, and a
// DDL composer with double quotes and with them.
type TableLayout struct {
	// Engine is the engine's name, without arguments: "MergeTree".
	Engine string `json:"engine,omitempty"`
	// PartitionKey is the PARTITION BY expression; "" for none.
	PartitionKey string `json:"partitionKey,omitempty"`
	// SortingKey is the ORDER BY expression; "" for tuple().
	SortingKey string `json:"sortingKey,omitempty"`
}

// IsZero reports a layout nothing was declared for.
func (inst TableLayout) IsZero() bool {
	return inst.Engine == "" && inst.PartitionKey == "" && inst.SortingKey == ""
}

// Normalized is the layout with each part reduced for comparison.
func (inst TableLayout) Normalized() (n TableLayout) {
	n.Engine = engineName(inst.Engine)
	n.PartitionKey = NormalizeKeyExpr(inst.PartitionKey)
	n.SortingKey = NormalizeKeyExpr(inst.SortingKey)
	return
}

// Equal compares two layouts after normalization.
func (inst TableLayout) Equal(other TableLayout) bool {
	return inst.Normalized() == other.Normalized()
}

// String renders the layout for an operator: engine, partition key and
// sorting key as given.
func (inst TableLayout) String() string {
	var b strings.Builder
	b.WriteString(inst.Engine)
	if inst.PartitionKey == "" {
		b.WriteString(", unpartitioned")
	} else {
		b.WriteString(", partition by " + inst.PartitionKey)
	}
	if inst.SortingKey == "" {
		b.WriteString(", order by tuple()")
	} else {
		b.WriteString(", order by " + inst.SortingKey)
	}
	return b.String()
}

// Digest is a short digest of the normalized layout, the value a table
// stamp records under `layout=`.
func (inst TableLayout) Digest() string {
	n := inst.Normalized()
	h := blake3.New(32, nil)
	_, _ = h.Write([]byte(n.Engine))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(n.PartitionKey))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(n.SortingKey))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// engineName is an engine spelling without its argument list.
func engineName(engine string) string {
	engine = strings.TrimSpace(engine)
	if i := strings.IndexByte(engine, '('); i >= 0 {
		engine = engine[:i]
	}
	return strings.TrimSpace(engine)
}

// engineClauseKeyword matches the clauses an engine clause may carry after
// the engine, at the start of a word outside quotes and parentheses.
var engineClauseKeyword = regexp.MustCompile(`(?i)^(PARTITION\s+BY|ORDER\s+BY|PRIMARY\s+KEY|SAMPLE\s+BY|TTL|SETTINGS)\b`)

// ParseEngineClause reads the layout out of a MergeTree engine clause as a
// CREATE TABLE carries it after its column list — `MergeTree() PARTITION BY
// … ORDER BY …`, optionally led by `ENGINE` and `=`. Clauses it does not
// read (PRIMARY KEY, SAMPLE BY, TTL) are skipped; settings is the
// SETTINGS clause's text, "" when there is none.
func ParseEngineClause(clause string) (l TableLayout, settings string, err error) {
	s := strings.TrimSpace(clause)
	if len(s) >= len("ENGINE") && strings.EqualFold(s[:len("ENGINE")], "ENGINE") {
		s = strings.TrimSpace(s[len("ENGINE"):])
		s = strings.TrimSpace(strings.TrimPrefix(s, "="))
	}
	var cuts []clauseCut
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			if ch == '\\' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
		case ch == '(':
			depth++
		case ch == ')':
			depth--
		case depth == 0 && (i == 0 || isSpace(s[i-1])):
			if m := engineClauseKeyword.FindString(s[i:]); m != "" {
				kw := strings.ToUpper(strings.Join(strings.Fields(m), " "))
				cuts = append(cuts, clauseCut{at: i, end: i + len(m), kw: kw})
				i += len(m) - 1
			}
		}
	}
	if quote != 0 || depth != 0 {
		err = eb.Build().Str("clause", clause).Errorf("engine clause has an unclosed quote or parenthesis")
		return
	}
	head := s
	if len(cuts) > 0 {
		head = s[:cuts[0].at]
	}
	l.Engine = engineName(head)
	if l.Engine == "" {
		err = eb.Build().Str("clause", clause).Errorf("engine clause names no engine")
		return
	}
	for i, c := range cuts {
		end := len(s)
		if i+1 < len(cuts) {
			end = cuts[i+1].at
		}
		body := strings.TrimSpace(s[c.end:end])
		switch c.kw {
		case "PARTITION BY":
			l.PartitionKey = body
		case "ORDER BY":
			l.SortingKey = body
		case "SETTINGS":
			settings = body
		}
	}
	return
}

// clauseCut is where one clause of an engine clause starts: its keyword's
// span and its canonical spelling.
type clauseCut struct {
	at  int
	end int
	kw  string
}

func isSpace(ch byte) bool { return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' }

// NormalizeKeyExpr reduces a partition or sorting key expression for
// comparison: whitespace outside string literals and identifier quotes
// (backticks, double quotes) removed, one pair of parentheses around the
// whole expression dropped, and `tuple()` read as the empty key. The server
// writes `a, b` for `ORDER BY ("a", "b")`; both reduce to `a,b`.
func NormalizeKeyExpr(expr string) (n string) {
	var b strings.Builder
	b.Grow(len(expr))
	var quote byte
	for i := 0; i < len(expr); i++ {
		ch := expr[i]
		if quote != 0 {
			if ch == '\\' && i+1 < len(expr) {
				if quote == '\'' {
					b.WriteByte(ch)
				}
				i++
				b.WriteByte(expr[i])
				continue
			}
			if ch == quote {
				if quote == '\'' {
					b.WriteByte(ch)
				}
				quote = 0
				continue
			}
			b.WriteByte(ch)
			continue
		}
		switch ch {
		case '\'':
			quote = ch
			b.WriteByte(ch)
		case '"', '`':
			quote = ch
		case ' ', '\t', '\n', '\r':
		default:
			b.WriteByte(ch)
		}
	}
	n = b.String()
	if strings.EqualFold(n, "tuple()") {
		return ""
	}
	if inner, ok := balanced(n); ok && len(inner)+2 == len(n) {
		n = inner
	}
	return
}
