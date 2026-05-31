package mappingplan

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ParsedLWTag is the structured result of parsing an `lw:` tag value.
// Returned by SplitLW; consumed by ParsePlan and the sibling
// marshallreflect.buildPlan.
type ParsedLWTag struct {
	Membership string
	Section    string
	Column     string
	Flags      FieldFlags
}

// setChannelFlag installs the parsed channel on the in-progress flag
// set, rejecting two channel flags on one tag. Tokens like `,verbatim`
// and `,lowCardVerbatim` both map to MembershipChannelLowCardVerbatim
// (per ADR-0008 D3 SD9); attempting either after a different channel
// already set raises the same "declared twice" error.
func setChannelFlag(flags *FieldFlags, ch MembershipChannel, token string) (err error) {
	if flags.Channel != MembershipChannelLowCardRef {
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
			if err = setChannelFlag(&out.Flags, MembershipChannelLowCardVerbatim, token); err != nil {
				return
			}
		case "highCardRef":
			if err = setChannelFlag(&out.Flags, MembershipChannelHighCardRef, token); err != nil {
				return
			}
		case "highCardVerbatim":
			if err = setChannelFlag(&out.Flags, MembershipChannelHighCardVerbatim, token); err != nil {
				return
			}
		case "lowCardRefParametrized", "highCardRefParametrized", "mixedLowCardRef", "mixedLowCardVerbatim":
			// ADR-0008 D3 stages these four "complex" channels for a
			// follow-up commit — the parametrized/mixed shapes require
			// a two-field DTO pairing the section value with a sibling
			// carrier, which is non-trivial. Parse-time rejection so
			// DTO authors get a clear signal rather than misleading
			// emit-time failures.
			err = eb.Build().Str("flag", token).Errorf("lw: channel flag %q is recognised but not yet implemented — see ADR-0008 D3 staged-rollout note", token)
			return
		default:
			err = eb.Build().Str("flag", token).Errorf("unknown flag token (recognised: unit, explode, verbatim / lowCardVerbatim, highCardRef, highCardVerbatim, const=<value>)")
			return
		}
	}
	return
}
