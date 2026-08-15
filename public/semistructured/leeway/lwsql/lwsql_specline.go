package lwsql

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// This file is the read-direction dual of Composer (ADR-0181 §SD6): it
// writes an existing column OUT as the token spelling an author would type to
// mint it — ADR-0186 §SD3's "spec line", the subject a display rule's regex
// matches against.

// Spec-line token prefixes beyond the ADR-0181 spec tokens (item:/enc:/sem:/
// use:, which are reused verbatim). A host appends its own tokens after
// these — play adds `arrow:<type>` last.
const (
	SpecLinePrefixName    = "name:"
	SpecLinePrefixSection = "section:"
	SpecLinePrefixRole    = "role:"
	SpecLinePrefixCT      = "ct:"
)

// SpecLines writes each column of a result as one line of space-separated
// tokens, in a fixed order:
//
//	name:<column> [section:<section>] [role:<role>] [item:<item>] [ct:<type>] enc:<a>… sem:<a>… use:<a>…
//
// A leeway-shaped set (per the same classification handle resolution uses)
// gets the full line per physical name: tagged columns carry section:, role:
// and use:; backbone columns carry item: and no section:/use:. Aspects are
// one token each, in vocabulary order, spelled by their String() — the
// spellings ParsePlainSpecTokens / ParseTaggedSpecTokens accept. Any other
// set — a plain SQL result, an aggregation — yields `name:<column>` per
// column, so a rule that keys on the name works on both.
//
// Columns that classify but fail an extraction keep whatever tokens were
// extracted before the failure; a leeway-shaped set never yields fewer lines
// than names.
func SpecLines(columnNames []string) (lines []string) {
	lines = make([]string, len(columnNames))
	for i, n := range columnNames {
		lines[i] = SpecLinePrefixName + n
	}
	if len(columnNames) == 0 {
		return lines
	}
	sep := detectSeparator(columnNames)
	conv, err := ddl.NewHumanReadableNamingConvention(sep)
	if err != nil {
		return lines
	}
	phys, err := conv.ParseColumns(columnNames)
	if err != nil {
		return lines
	}
	if _, _, ok := classifyColumns(columnNames); !ok {
		return lines
	}
	var b strings.Builder
	for i, phy := range phys {
		b.Reset()
		col, colErr := conv.ExtractLeewayColumnName(phy)
		if colErr != nil {
			continue
		}
		b.WriteString(SpecLinePrefixName)
		b.WriteString(string(col))
		pit, pitErr := conv.ExtractPlainItemType(phy)
		if pitErr != nil {
			lines[i] = b.String()
			continue
		}
		tagged := pit == common.PlainItemTypeNone
		if tagged {
			if sec, secErr := conv.ExtractSectionName(phy); secErr == nil && sec != "" {
				b.WriteString(" " + SpecLinePrefixSection)
				b.WriteString(string(sec))
			}
			if role, roleErr := conv.ExtractColumnRole(phy); roleErr == nil && role != common.ColumnRoleUnspecific {
				b.WriteString(" " + SpecLinePrefixRole)
				b.WriteString(string(role))
			}
		} else if item := plainItemPrefix(pit); item != "" {
			b.WriteString(" " + SpecTokenPrefixItem)
			b.WriteString(item)
		}
		if ct, ctErr := conv.ExtractCanonicalType(phy); ctErr == nil && ct != nil {
			b.WriteString(" " + SpecLinePrefixCT)
			b.WriteString(ct.String())
		}
		if enc, encErr := conv.ExtractEncodingHints(phy); encErr == nil {
			if known, _, decErr := enc.DecodeAspects(); decErr == nil {
				for _, a := range known {
					b.WriteString(" " + SpecTokenPrefixEnc)
					b.WriteString(encodingaspects.AspectE(a).String())
				}
			}
		}
		if sem, semErr := conv.ExtractValueSemantics(phy); semErr == nil {
			if known, _, decErr := sem.DecodeAspects(); decErr == nil {
				for _, a := range known {
					b.WriteString(" " + SpecTokenPrefixSem)
					b.WriteString(valueaspects.AspectE(a).String())
				}
			}
		}
		if tagged {
			if use, useErr := conv.ExtractUseAspects(phy); useErr == nil {
				if known, _, decErr := use.DecodeAspects(); decErr == nil {
					for _, a := range known {
						b.WriteString(" " + SpecTokenPrefixUse)
						b.WriteString(useaspects.AspectE(a).String())
					}
				}
			}
		}
		lines[i] = b.String()
	}
	return lines
}

// plainItemPrefix is the inverse of parsePlainItemType: the physical-name
// prefix spelling of a backbone item type, which is also its `item:` token.
func plainItemPrefix(pit common.PlainItemTypeE) string {
	switch pit {
	case common.PlainItemTypeEntityId:
		return ddl.IdPrefix
	case common.PlainItemTypeEntityTimestamp:
		return ddl.TimestampPrefix
	case common.PlainItemTypeEntityRouting:
		return ddl.RoutingPrefix
	case common.PlainItemTypeEntityLifecycle:
		return ddl.LifecyclePrefix
	case common.PlainItemTypeTransaction:
		return ddl.TransactionPrefix
	case common.PlainItemTypeOpaque:
		return ddl.OpaquePrefix
	}
	return ""
}
