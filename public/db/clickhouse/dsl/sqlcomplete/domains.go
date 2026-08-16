package sqlcomplete

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
)

// clauseDomains is the coarse fallback: what a caret sitting under a top-level
// keyword, inside no call whose signature we know, may be completing.
//
// Deliberately short and deliberately last. A clause classifier cannot say what
// belongs at an argument position — that is the whole reason ADR-0190 exists —
// so this covers only the positions where the clause really is the answer.
var clauseDomains = map[string]sqlvocab.DomainKindE{
	"SETTINGS": sqlvocab.DomainSetting,
	"SET":      sqlvocab.DomainSetting,
	"FORMAT":   sqlvocab.DomainFormat,
}

// clauseKindFor maps the clauses whose position really is described by the
// clause: a table position and a column position.
func clauseKindFor(clause string) (k sqlvocab.DomainKindE, ok bool) {
	switch clause {
	case "FROM", "JOIN", "INTO":
		return sqlvocab.DomainTable, true
	case "SELECT", "WHERE", "PREWHERE", "HAVING", "ON", "USING", "BY", "ORDER", "GROUP":
		// An expression, not a column: a call is as valid there as a name, and
		// a domain that said "column" would make the pane's heading a lie
		// about half of what it offers.
		return sqlvocab.DomainExpr, true
	}
	if k, hit := clauseDomains[clause]; hit {
		return k, true
	}
	return
}

// repeatingTail reports whether a signature's last parameter repeats.
//
// The rosters spell a repeating tail with an ellipsis in the parameter's own
// display name — `cols…`, `'enc:…/sem:…'…`, `'chan:…'` — because that is how
// they have always rendered it to a reader. Reading the same mark rather than
// adding a flag keeps one spelling of the fact.
//
// The rule is what makes `LW_TAGGED(e,'s','n','t','enc:a','sem:b')` complete at
// the sixth argument, and what keeps `LW_MEMBERSHIP`, whose arity is exactly
// three, from claiming a fourth.
func repeatingTail(f sqlvocab.Function) (d sqlvocab.Domain, ok bool) {
	if len(f.Params) == 0 {
		return
	}
	last := f.Params[len(f.Params)-1]
	if !strings.Contains(last.Name, "…") {
		return
	}
	d = last.Domain
	ok = true
	return
}

// domainAt resolves an argument ordinal against a signature.
func domainAt(f sqlvocab.Function, ordinal int) (d sqlvocab.Domain, ok bool) {
	if ordinal < 0 {
		return
	}
	if ordinal < len(f.Params) {
		d = f.Params[ordinal].Domain
		ok = true
		return
	}
	return repeatingTail(f)
}

// itemKindFor is the presentation kind a domain's rows carry.
func itemKindFor(k sqlvocab.DomainKindE) ItemKindE {
	switch k {
	case sqlvocab.DomainComponentKind:
		return ItemComponentKind
	case sqlvocab.DomainElementOf:
		return ItemField
	case sqlvocab.DomainDatabase:
		return ItemDatabase
	case sqlvocab.DomainTable:
		return ItemTable
	case sqlvocab.DomainColumn:
		return ItemColumn
	case sqlvocab.DomainIntrospectionTable:
		return ItemTable
	case sqlvocab.DomainSection:
		return ItemSection
	case sqlvocab.DomainSectionColumn:
		return ItemColumn
	case sqlvocab.DomainExtractionToken:
		return ItemChannel
	case sqlvocab.DomainMembership:
		return ItemMembership
	case sqlvocab.DomainChannel:
		return ItemChannel
	case sqlvocab.DomainSupportRole:
		return ItemSupportRole
	case sqlvocab.DomainAspect:
		return ItemAspect
	case sqlvocab.DomainCanonicalType:
		return ItemCanonicalType
	case sqlvocab.DomainGloss:
		return ItemGloss
	case sqlvocab.DomainGlossKey:
		return ItemGlossKey
	case sqlvocab.DomainIdentityTag:
		return ItemIdentityTag
	case sqlvocab.DomainTypeName:
		return ItemTypeName
	case sqlvocab.DomainTimeZone:
		return ItemTimeZone
	case sqlvocab.DomainSetting:
		return ItemSetting
	case sqlvocab.DomainDictionary:
		return ItemDictionary
	case sqlvocab.DomainDictionaryAttribute:
		return ItemColumn
	case sqlvocab.DomainFormat:
		return ItemFormat
	case sqlvocab.DomainStatementParam:
		return ItemParam
	}
	return ItemUnspecified
}
