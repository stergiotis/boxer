package gloss

import "strings"

// Spec is a column's spec line (ADR-0186 §SD3) taken apart: the tokens
// lwsql.SpecLines writes — name:, section:, role:, item:, ct:, enc:, sem:,
// use: — and the host's trailing arrow:, each by its prefix, plus the line
// itself for a rule that matches the text. It is what a Predicate reads.
type Spec struct {
	Line    string
	Name    string
	Section string
	Role    string
	Item    string
	CT      string
	Arrow   string
	Enc     []string
	Sem     []string
	Use     []string
}

// The spec-line prefixes, as lwsql spells them (SpecLinePrefix* and the
// ADR-0181 SpecTokenPrefix*) plus the host's arrow: token. Kept here as
// strings so this package does not import the writer.
const (
	specPrefixName    = "name:"
	specPrefixSection = "section:"
	specPrefixRole    = "role:"
	specPrefixItem    = "item:"
	specPrefixCT      = "ct:"
	specPrefixEnc     = "enc:"
	specPrefixSem     = "sem:"
	specPrefixUse     = "use:"
	specPrefixArrow   = "arrow:"
)

// ParseSpec takes a spec line apart. Tokens are space-separated; a token
// without a known prefix continues the previous token's value — a name with
// a space in it, an Arrow type spelled `list<item: float64, nullable>` — so
// a value is whatever ran until the next prefixed token. Tokens before the
// first prefixed one are dropped.
func ParseSpec(line string) (s Spec) {
	s.Line = line
	var cur *string // the value the next unprefixed token continues
	for _, tok := range strings.Fields(line) {
		switch {
		case strings.HasPrefix(tok, specPrefixName):
			s.Name = tok[len(specPrefixName):]
			cur = &s.Name
		case strings.HasPrefix(tok, specPrefixSection):
			s.Section = tok[len(specPrefixSection):]
			cur = &s.Section
		case strings.HasPrefix(tok, specPrefixRole):
			s.Role = tok[len(specPrefixRole):]
			cur = &s.Role
		case strings.HasPrefix(tok, specPrefixItem):
			s.Item = tok[len(specPrefixItem):]
			cur = &s.Item
		case strings.HasPrefix(tok, specPrefixCT):
			s.CT = tok[len(specPrefixCT):]
			cur = &s.CT
		case strings.HasPrefix(tok, specPrefixArrow):
			s.Arrow = tok[len(specPrefixArrow):]
			cur = &s.Arrow
		case strings.HasPrefix(tok, specPrefixEnc):
			s.Enc = append(s.Enc, tok[len(specPrefixEnc):])
			cur = &s.Enc[len(s.Enc)-1]
		case strings.HasPrefix(tok, specPrefixSem):
			s.Sem = append(s.Sem, tok[len(specPrefixSem):])
			cur = &s.Sem[len(s.Sem)-1]
		case strings.HasPrefix(tok, specPrefixUse):
			s.Use = append(s.Use, tok[len(specPrefixUse):])
			cur = &s.Use[len(s.Use)-1]
		default:
			if cur != nil {
				*cur += " " + tok
			}
		}
	}
	return s
}
