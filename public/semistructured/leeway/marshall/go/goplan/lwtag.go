package goplan

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// ParsedLWTag is the structured result of parsing an `lw:` tag value.
// Returned by SplitLW; consumed by ParsePlan and the sibling
// marshallreflect.buildPlan.
type ParsedLWTag struct {
	Membership string
	Section    string
	Column     string
	Flags      mappingplan.FieldFlags
}

// setChannelFlag installs the parsed channel on the in-progress flag
// set, rejecting two channel flags on one tag. Tokens like `,verbatim`
// and `,lowCardVerbatim` both map to MembershipChannelLowCardVerbatim
// (per ADR-0008 D3 SD9); attempting either after a different channel
// already set raises the same "declared twice" error.
func setChannelFlag(flags *mappingplan.FieldFlags, ch mappingplan.MembershipChannel, token string) (err error) {
	if flags.Channel != mappingplan.MembershipChannelLowCardRef {
		err = eb.Build().Str("flag", token).Str("alreadySet", flags.Channel.String()).Errorf("channel flag declared twice on one tag")
		return
	}
	flags.Channel = ch
	return
}

// SplitLW parses a value of the form
//
//	<membership>[,<section>[:<column>]][,<flag>][,<flag>…]
//
// into its components. Empty segments are tolerated and yield zero
// values; unknown flag tokens are an error. Exported so the sibling
// marshallreflect package can reuse the grammar without duplicating
// the parser.
func SplitLW(tag string) (out ParsedLWTag, err error) {
	parts := strings.Split(tag, ",")
	out.Membership = strings.TrimSpace(parts[0])
	if len(parts) >= 2 {
		s := strings.TrimSpace(parts[1])
		if colonIdx := strings.IndexByte(s, ':'); colonIdx >= 0 {
			out.Section = s[:colonIdx]
			out.Column = s[colonIdx+1:]
		} else {
			out.Section = s
		}
	}
	if len(parts) < 3 {
		return
	}
	for _, raw := range parts[2:] {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		// Key=value flags (currently only `const=<value>`).
		if eq := strings.IndexByte(token, '='); eq > 0 {
			key := token[:eq]
			val := token[eq+1:]
			switch key {
			case "const":
				if out.Flags.HasConst {
					err = eb.Build().Str("flag", key).Errorf("flag declared twice")
					return
				}
				out.Flags.HasConst = true
				out.Flags.ConstValue = val
			default:
				err = eb.Build().Str("flag", key).Errorf("unknown key=value flag (recognised: const=<value>)")
				return
			}
			continue
		}
		switch token {
		case "unit":
			if out.Flags.Unit {
				err = eb.Build().Str("flag", token).Errorf("flag declared twice")
				return
			}
			out.Flags.Unit = true
		case "explode":
			if out.Flags.Explode {
				err = eb.Build().Str("flag", token).Errorf("flag declared twice")
				return
			}
			out.Flags.Explode = true
		case "verbatim", "lowCardVerbatim":
			// `,verbatim` retained as alias for `,lowCardVerbatim` per
			// ADR-0008 D3 SD9 — existing DTOs compile unchanged.
			if err = setChannelFlag(&out.Flags, mappingplan.MembershipChannelLowCardVerbatim, token); err != nil {
				return
			}
		case "highCardRef":
			if err = setChannelFlag(&out.Flags, mappingplan.MembershipChannelHighCardRef, token); err != nil {
				return
			}
		case "highCardVerbatim":
			if err = setChannelFlag(&out.Flags, mappingplan.MembershipChannelHighCardVerbatim, token); err != nil {
				return
			}
		case "mixedLowCardRef":
			// ADR-0008 D3 Cut-2: the first carrier-paired channel. Pairs a
			// value field with a marshalltypes.MixedLowCardRef sibling
			// (id + params); see mappingplan.PlanBuilder + the Cut-2 update.
			if err = setChannelFlag(&out.Flags, mappingplan.MembershipChannelMixedLowCardRef, token); err != nil {
				return
			}
		case "mixedLowCardVerbatim":
			// ADR-0008 D3 Cut-2: the verbatim sibling of mixedLowCardRef —
			// pairs a value field with a marshalltypes.MixedLowCardVerbatim
			// sibling (name + params; the label embeds literally on the wire).
			if err = setChannelFlag(&out.Flags, mappingplan.MembershipChannelMixedLowCardVerbatim, token); err != nil {
				return
			}
		case "lowCardRefParametrized":
			// ADR-0008 D3 Cut-2: opaque-blob membership — pairs with a
			// marshalltypes.Parametrized sibling (params only; read via a
			// single Seq[[]byte]).
			if err = setChannelFlag(&out.Flags, mappingplan.MembershipChannelLowCardRefParametrized, token); err != nil {
				return
			}
		case "highCardRefParametrized":
			// ADR-0008 D3 Cut-2: high-card sibling of lowCardRefParametrized.
			if err = setChannelFlag(&out.Flags, mappingplan.MembershipChannelHighCardRefParametrized, token); err != nil {
				return
			}
		default:
			err = eb.Build().Str("flag", token).Errorf("unknown flag token (recognised: unit, explode, verbatim / lowCardVerbatim, highCardRef, highCardVerbatim, const=<value>)")
			return
		}
	}
	return
}
