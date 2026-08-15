package gloss

import "strings"

// MediaTypeLuhn is the check-digit gloss for card-style numbers: the digits
// in groups of four with the middle groups masked, and a ✓ or ✗ in the
// success or error tone by the Luhn (ISO/IEC 7812-1) check. Masked by
// default because a primary account number is one; showing more is a
// parameter for later, not a default.
const MediaTypeLuhn = "gloss/luhn"

func luhnGloss() GlossI {
	return &simpleGloss{
		mediaType: MediaTypeLuhn,
		doc:       "a card-style number, masked in groups of four, with its Luhn check-digit verdict",
		accepts:   []ValueKindE{ValueKindText, ValueKindNumeric},
		inline:    luhnFace,
	}
}

// luhnFace reads the digits of the cell (spaces and dashes are grouping,
// anything else makes the value not-a-number: shown as-is, warning tone).
func luhnFace(cell CellI) Inline {
	digits, ok := luhnDigits(cell.Text())
	if !ok || len(digits) < 2 {
		return Inline{Text: cell.Text(), Tone: ToneWarning}
	}
	var b strings.Builder
	b.Grow(len(digits) + len(digits)/4 + 3)
	groups := (len(digits) + 3) / 4
	for g := 0; g < groups; g++ {
		if g > 0 {
			b.WriteByte(' ')
		}
		lo, hi := g*4, min((g+1)*4, len(digits))
		if g == 0 || g == groups-1 {
			b.WriteString(digits[lo:hi])
		} else {
			for range hi - lo {
				b.WriteRune('•')
			}
		}
	}
	if LuhnValid(digits) {
		b.WriteString(" ✓")
		return Inline{Text: b.String(), Tone: ToneSuccess}
	}
	b.WriteString(" ✗")
	return Inline{Text: b.String(), Tone: ToneError}
}

// luhnDigits strips grouping characters and refuses anything but digits.
func luhnDigits(s string) (digits string, ok bool) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= '0' && ch <= '9':
			b.WriteByte(ch)
		case ch == ' ' || ch == '-':
		default:
			return "", false
		}
	}
	return b.String(), true
}

// LuhnValid is the Luhn mod-10 check over a digit string (digits only; the
// caller has stripped grouping). The rightmost digit is the check digit.
func LuhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
