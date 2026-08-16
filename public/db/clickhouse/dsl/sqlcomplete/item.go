package sqlcomplete

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
)

// ItemKindE says what a candidate is, for the pane's kind column and for an
// embedder choosing an icon. It is presentation, not semantics: the domain the
// candidate came from is what says where it is valid.
//
//codelint:enum-prefix=Item
type ItemKindE uint8

const (
	ItemUnspecified ItemKindE = iota
	ItemComponentKind
	ItemField
	ItemTable
	ItemDatabase
	ItemColumn
	ItemFunction
	ItemKeyword
	ItemSection
	ItemMembership
	ItemChannel
	ItemSupportRole
	ItemAspect
	ItemCanonicalType
	ItemGloss
	ItemGlossKey
	ItemIdentityTag
	ItemTypeName
	ItemTimeZone
	ItemSetting
	ItemDictionary
	ItemFormat
	ItemAlias
	ItemEnumValue
	ItemParam
)

func (inst ItemKindE) String() string {
	switch inst {
	case ItemUnspecified:
		return "unspecified"
	case ItemComponentKind:
		return "kind"
	case ItemField:
		return "field"
	case ItemTable:
		return "table"
	case ItemDatabase:
		return "database"
	case ItemColumn:
		return "column"
	case ItemFunction:
		return "function"
	case ItemKeyword:
		return "keyword"
	case ItemSection:
		return "section"
	case ItemMembership:
		return "membership"
	case ItemChannel:
		return "channel"
	case ItemSupportRole:
		return "support role"
	case ItemAspect:
		return "aspect"
	case ItemCanonicalType:
		return "canonical type"
	case ItemGloss:
		return "gloss"
	case ItemGlossKey:
		return "gloss key"
	case ItemIdentityTag:
		return "identity tag"
	case ItemTypeName:
		return "type"
	case ItemTimeZone:
		return "time zone"
	case ItemSetting:
		return "setting"
	case ItemDictionary:
		return "dictionary"
	case ItemFormat:
		return "format"
	case ItemAlias:
		return "alias"
	case ItemEnumValue:
		return "enum value"
	case ItemParam:
		return "parameter"
	}
	return "unknown"
}

// Item is one candidate.
type Item struct {
	// Text is the candidate's identity — the value, unquoted: `SysMem`,
	// `TotalBytes`, `system.parts`. It is what a match compares against and
	// what the pane's first column shows.
	Text string
	// Insert is the spelling that names it at the caret's position. It equals
	// Text inside a string literal, and carries the quotes when the position
	// takes a literal and none has been opened. Filled by the engine, not by
	// the provider, because only the engine knows where the caret is.
	Insert string
	Kind   ItemKindE
	// Type is the candidate's ClickHouse type where it has one — a component
	// field, a column. Empty otherwise.
	Type string
	// Doc is one line, for the pane's last column.
	Doc string
	// Source names the provider, so a row can say where it came from (§SD1's
	// provenance requirement).
	Source string
	// Marks are the provisioning marks the Vocabulary tab shows — `✓`,
	// `MISSING`, a dependency note. Carried through rather than filtered on:
	// hiding a MISSING function would hide the provisioning fact (§SD8).
	Marks []string
}

// MatchE is the state of the token under the caret against the candidates.
//
//codelint:enum-prefix=Match
type MatchE uint8

const (
	// MatchNone is nothing typed, or nothing that extends what was typed.
	MatchNone MatchE = iota
	// MatchPrefix is one or more candidates extending the typed text.
	MatchPrefix
	// MatchExact is a candidate equal to the token's whole text — the state
	// §SD9 tints the editor for.
	MatchExact
)

func (inst MatchE) String() string {
	switch inst {
	case MatchNone:
		return "none"
	case MatchPrefix:
		return "prefix"
	case MatchExact:
		return "exact"
	}
	return "unknown"
}

// literalDomains are the domains whose candidates are spelled as string
// literals, so a candidate offered where no quote has been typed carries its
// own.
func literalDomain(k sqlvocab.DomainKindE) bool {
	switch k {
	case sqlvocab.DomainComponentKind, sqlvocab.DomainElementOf,
		sqlvocab.DomainIntrospectionTable, sqlvocab.DomainSection,
		sqlvocab.DomainSectionColumn, sqlvocab.DomainExtractionToken,
		sqlvocab.DomainChannel, sqlvocab.DomainSupportRole, sqlvocab.DomainAspect,
		sqlvocab.DomainCanonicalType, sqlvocab.DomainGloss, sqlvocab.DomainGlossKey,
		sqlvocab.DomainTypeName, sqlvocab.DomainTimeZone, sqlvocab.DomainSetting,
		sqlvocab.DomainDictionary, sqlvocab.DomainDictionaryAttribute,
		sqlvocab.DomainFormat:
		return true
	}
	return false
}

// quoteLiteral spells a candidate as a ClickHouse string literal.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`) + "'"
}
