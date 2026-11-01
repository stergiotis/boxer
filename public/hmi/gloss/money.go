package gloss

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// MediaTypeMoney is an amount of a currency, shown compactly. `unit` is the
// ISO 4217 alphabetic code of the stored currency and is required: an amount
// without a currency is a number, and no currency is a default another would
// forgive. It formats and never converts — a rate is data, not a unit factor.
const MediaTypeMoney = "gloss/money"

// iso4217Codes is ISO 4217 List One as published 2026-09-17, then the six
// codes withdrawn most recently (ANG BGN CUC HRK SLL ZWL), which amounts
// already on record still carry. A closed list rather than "three upper-case
// letters", so `unit=EUD` is refused where it would otherwise render as a
// currency nobody issues.
const iso4217Codes = "AED AFN ALL AMD AOA ARS AUD AWG AZN BAM BBD BDT BHD BIF BMD BND BOB BOV BRL BSD BTN BWP BYN BZD " +
	"CAD CDF CHE CHF CHW CLF CLP CNY COP COU CRC CUP CVE CZK DJF DKK DOP DZD EGP ERN ETB EUR FJD FKP GBP GEL GHS GIP GMD GNF GTQ GYD " +
	"HKD HNL HTG HUF IDR ILS INR IQD IRR ISK JMD JOD JPY KES KGS KHR KMF KPW KRW KWD KYD KZT LAK LBP LKR LRD LSL LYD " +
	"MAD MDL MGA MKD MMK MNT MOP MRU MUR MVR MWK MXN MXV MYR MZN NAD NGN NIO NOK NPR NZD OMR PAB PEN PGK PHP PKR PLN PYG QAR " +
	"RON RSD RUB RWF SAR SBD SCR SDG SEK SGD SHP SLE SOS SRD SSP STN SVC SYP SZL THB TJS TMT TND TOP TRY TTD TWD TZS " +
	"UAH UGX USD USN UYI UYU UYW UZS VED VES VND VUV WST XAD XAF XAG XAU XBA XBB XBC XBD XCD XCG XDR XOF XPD XPF XPT XSU XTS XUA XXX " +
	"YER ZAR ZMW ZWG " +
	"ANG BGN CUC HRK SLL ZWL"

var iso4217Set = sync.OnceValue(func() map[string]bool {
	codes := strings.Fields(iso4217Codes)
	out := make(map[string]bool, len(codes))
	for _, c := range codes {
		out[c] = true
	}
	return out
})

// IsCurrencyCode reports whether s is an ISO 4217 alphabetic code the catalog
// accepts. Case-sensitive, as every closed parameter value in the catalog is.
func IsCurrencyCode(s string) bool { return iso4217Set()[s] }

// currencySymbols holds the only symbols the face uses: those that name one
// currency unqualified wherever the result is read. Every other code shows as
// the code. `$` without a qualifier is read as USD and `¥` as JPY in the
// sources this catalog meets, so CAD, AUD, HKD, MXN, … and CNY, which share
// those signs, stay codes; so do SEK, NOK and DKK, which share `kr`. A symbol
// is never guessed from a code, and one outside Latin-1 and the euro sign
// would also make a cell depend on the font carrying it.
var currencySymbols = map[string]string{
	"EUR": "€",
	"USD": "$",
	"GBP": "£",
	"JPY": "¥",
}

// FormatMoney writes an amount of a currency: the symbol tight against the
// figure where the currency has one, the code and a space otherwise, the sign
// in front of both — "€12.5k", "-$3m", "CHF 950".
func FormatMoney(code string, v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	if sym, ok := currencySymbols[code]; ok {
		return sign + sym + FormatCompactAmount(v)
	}
	return sign + code + " " + FormatCompactAmount(v)
}

// FormatCompactAmount is an amount to three significant figures with a
// k / m / bn / tn suffix: "950", "1.25k", "12.5m", "3bn": the financial
// suffixes, not the SI prefixes.
func FormatCompactAmount(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	// Round first, then pick the suffix, so 999.6k reads "1m" and not "1000k".
	r := roundSigFig3(v)
	for _, s := range [...]struct {
		div    float64
		suffix string
	}{{1e12, "tn"}, {1e9, "bn"}, {1e6, "m"}, {1e3, "k"}} {
		if r >= s.div {
			return sign + strconv.FormatFloat(roundSigFig3(r/s.div), 'f', -1, 64) + s.suffix
		}
	}
	return sign + strconv.FormatFloat(r, 'f', -1, 64)
}

// roundSigFig3 rounds a positive v to three significant figures; the division
// by a power of ten keeps 12.5 from printing as 12.500000000000002.
func roundSigFig3(v float64) float64 {
	if v <= 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return v
	}
	e := math.Floor(math.Log10(v)) - 2
	if e < 0 {
		m := math.Pow(10, -e)
		return math.Round(v*m) / m
	}
	m := math.Pow(10, e)
	return math.Round(v/m) * m
}

type moneyGloss struct{}

var _ GlossI = moneyGloss{}

func (moneyGloss) MediaType() string { return MediaTypeMoney }
func (moneyGloss) Doc() string {
	return "an amount of a currency, compact: €12.5k, $3m, CHF 950; a symbol for EUR, USD, GBP and JPY only, the ISO 4217 code otherwise"
}
func (moneyGloss) Params() []ParamSpec {
	return []ParamSpec{{Name: ParamUnit, Doc: "the stored currency, an ISO 4217 alphabetic code", Sample: "EUR"}}
}
func (moneyGloss) Affinities() []string { return nil }

// Bind checks the code here rather than through ParamSpec.Values: the closed
// set is some 180 codes, which a Values list would spell out in every refusal
// and on the Glosses tab's row.
func (moneyGloss) Bind(params map[string]string) (InstanceI, error) {
	code, ok := params[ParamUnit]
	if !ok {
		return nil, eb.Build().Str("mediaType", MediaTypeMoney).
			Errorf(MediaTypeMoney + " requires " + ParamUnit + "=<ISO 4217 code>, the stored currency, such as EUR")
	}
	if !IsCurrencyCode(code) {
		msg := ParamUnit + "=" + strconv.Quote(code) + " is not an ISO 4217 code"
		if upper := strings.ToUpper(code); upper != code && IsCurrencyCode(upper) {
			msg += "; the codes are upper case: " + upper
		}
		return nil, eb.Build().Str("mediaType", MediaTypeMoney).Errorf("%s", msg)
	}
	return &moneyInstance{params: params, code: code}, nil
}

type moneyInstance struct {
	params map[string]string
	code   string
}

var _ InstanceI = (*moneyInstance)(nil)

func (inst *moneyInstance) Gloss() GlossI             { return moneyGloss{} }
func (inst *moneyInstance) Params() map[string]string { return inst.params }
func (inst *moneyInstance) Accepts(kind ValueKindE) (bool, string) {
	return acceptsKind(MediaTypeMoney, numericOnly, kind)
}
func (inst *moneyInstance) Inline(cell CellI) Inline {
	v, ok := cell.Float64()
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return Inline{Text: cell.Text()}
	}
	return Inline{Text: FormatMoney(inst.code, v)}
}
