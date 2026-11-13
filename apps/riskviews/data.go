package riskviews

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// The synthetic register and every computation the views draw from, ported
// from the HTML prototype's <script> so the numbers match: the same eight
// risks held as 90% intervals, the same seeded Monte Carlo (mulberry32 plus
// Box–Muller), the same tolerance curve, indicators, bow-tie, calibration,
// incident log and service scenarios.

// Risk is one register row: annual likelihood and impact as 90% intervals,
// residual (P, I) and inherent (IP, II).
type Risk struct {
	Id   string
	Name string
	P    [2]float64
	I    [2]float64
	IP   [2]float64
	II   [2]float64
}

var risks = []Risk{
	{Id: "R1", Name: "Ransomware encrypts core ERP", P: [2]float64{0.03, 0.15}, I: [2]float64{800e3, 12e6}, IP: [2]float64{0.10, 0.35}, II: [2]float64{1.5e6, 25e6}},
	{Id: "R2", Name: "Cloud region outage over 24 h", P: [2]float64{0.02, 0.08}, I: [2]float64{200e3, 3e6}, IP: [2]float64{0.02, 0.08}, II: [2]float64{600e3, 6e6}},
	{Id: "R3", Name: "Key supplier insolvency", P: [2]float64{0.05, 0.20}, I: [2]float64{300e3, 4e6}, IP: [2]float64{0.05, 0.20}, II: [2]float64{1e6, 10e6}},
	{Id: "R4", Name: "Payment fraud via fake invoice", P: [2]float64{0.20, 0.60}, I: [2]float64{20e3, 400e3}, IP: [2]float64{0.40, 0.90}, II: [2]float64{50e3, 900e3}},
	{Id: "R5", Name: "Customer data breach", P: [2]float64{0.02, 0.10}, I: [2]float64{500e3, 20e6}, IP: [2]float64{0.05, 0.20}, II: [2]float64{1e6, 30e6}},
	{Id: "R6", Name: "Late incident reporting fine", P: [2]float64{0.01, 0.05}, I: [2]float64{100e3, 5e6}, IP: [2]float64{0.04, 0.15}, II: [2]float64{100e3, 5e6}},
	{Id: "R7", Name: "Loss of platform lead", P: [2]float64{0.10, 0.30}, I: [2]float64{50e3, 600e3}, IP: [2]float64{0.15, 0.40}, II: [2]float64{80e3, 900e3}},
	{Id: "R8", Name: "Warehouse fire", P: [2]float64{0.002, 0.01}, I: [2]float64{2e6, 40e6}, IP: [2]float64{0.004, 0.02}, II: [2]float64{3e6, 60e6}},
}

const z90 = 1.6449

func gmean(a [2]float64) float64 { return math.Sqrt(a[0] * a[1]) }

// lnParams fits a lognormal to a 90% interval.
func lnParams(ci [2]float64) (mu, s float64) {
	a, b := math.Log(ci[0]), math.Log(ci[1])
	return (a + b) / 2, (b - a) / (2 * z90)
}

func meanImpact(ci [2]float64) float64 {
	mu, s := lnParams(ci)
	return math.Exp(mu + s*s/2)
}

// mulberry32 is the prototype's PRNG, bit for bit: JS's |0 and Math.imul are
// uint32 wrap-around arithmetic.
func mulberry32(seed uint32) func() float64 {
	return func() float64 {
		seed += 0x6D2B79F5
		t := (seed ^ (seed >> 15)) * (1 | seed)
		t = (t + (t^(t>>7))*(61|t)) ^ t
		return float64(t^(t>>14)) / 4294967296
	}
}

// simYears is the number of simulated years.
const simYears = 20000

// simulate draws simYears annual totals: each risk occurs at most once a year
// with p at its interval's geometric mean, impact lognormal. Sorted ascending.
func simulate(useInherent bool, seed uint32) []float64 {
	rnd := mulberry32(seed)
	type par struct{ p, mu, s float64 }
	pars := make([]par, len(risks))
	for k, r := range risks {
		pi, ii := r.P, r.I
		if useInherent {
			pi, ii = r.IP, r.II
		}
		mu, s := lnParams(ii)
		pars[k] = par{p: gmean(pi), mu: mu, s: s}
	}
	out := make([]float64, simYears)
	for n := range out {
		tot := 0.0
		for _, pr := range pars {
			if rnd() < pr.p {
				u1 := rnd()
				if u1 == 0 {
					u1 = 1e-12
				}
				u2 := rnd()
				z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
				tot += math.Exp(pr.mu + pr.s*z)
			}
		}
		out[n] = tot
	}
	sort.Float64s(out)
	return out
}

var (
	simRes = simulate(false, 42)
	simInh = simulate(true, 7)
)

// exceed is P(annual loss >= x) over a sorted sample.
func exceed(sorted []float64, x float64) float64 {
	lo := sort.SearchFloat64s(sorted, x) // first index with sorted[i] >= x
	return float64(len(sorted)-lo) / float64(len(sorted))
}

// tol is the board's tolerance curve: P(annual loss >= x) must stay below.
var tol = [][2]float64{{100e3, 0.60}, {1e6, 0.20}, {5e6, 0.05}, {20e6, 0.01}, {100e6, 0.002}}

func tolAt(x float64) float64 {
	if x <= tol[0][0] {
		return tol[0][1]
	}
	for i := 1; i < len(tol); i++ {
		if x <= tol[i][0] {
			t := (math.Log(x) - math.Log(tol[i-1][0])) / (math.Log(tol[i][0]) - math.Log(tol[i-1][0]))
			return math.Exp(math.Log(tol[i-1][1]) + t*(math.Log(tol[i][1])-math.Log(tol[i-1][1])))
		}
	}
	return tol[len(tol)-1][1]
}

func ealResidual() (s float64) {
	for _, r := range risks {
		s += gmean(r.P) * meanImpact(r.I)
	}
	return
}

func ealInherent() (s float64) {
	for _, r := range risks {
		s += gmean(r.IP) * meanImpact(r.II)
	}
	return
}

// logSpace returns n+1 points from a to b, geometric.
func logSpace(a, b float64, n int) []float64 {
	xs := make([]float64, n+1)
	for i := range xs {
		xs[i] = math.Exp(math.Log(a) + float64(i)/float64(n)*(math.Log(b)-math.Log(a)))
	}
	return xs
}

// lecBreach is the LEC callout: the first and last of 81 log-spaced amounts
// where the residual curve sits above tolerance.
func lecBreach() (lo, hi float64, ok bool) {
	for _, x := range logSpace(50e3, 80e6, 80) {
		if exceed(simRes, x) > tolAt(x) {
			if !ok {
				lo, ok = x, true
			}
			hi = x
		}
	}
	return
}

// ---------- formatting, as the prototype's eur / pct / oneIn ----------

func jsRound(v float64) float64 { return math.Floor(v + 0.5) }

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func eur(v float64) string {
	if v == 0 {
		return "€0"
	}
	if v >= 1e6 {
		m := v / 1e6
		if m >= 10 {
			return "€" + fmtNum(jsRound(m)) + "m"
		}
		s := strconv.FormatFloat(m, 'f', 1, 64)
		s = strings.TrimSuffix(s, ".0")
		return "€" + s + "m"
	}
	if v >= 1e3 {
		return "€" + fmtNum(jsRound(v/1e3)) + "k"
	}
	return "€" + fmtNum(jsRound(v))
}

func pct(v float64, d int) string { return strconv.FormatFloat(v*100, 'f', d, 64) + "%" }

func oneIn(p float64) string { return "1 in " + fmtNum(jsRound(1/p)) + " yr" }

// ---------- view 1: matrix bands ----------

var (
	likeBands       = []float64{0.001, 0.01, 0.03, 0.1, 0.3, 1}
	likeLabels      = []string{"< 1 in 100 yr", "1 in 30–100 yr", "1 in 10–30 yr", "1 in 3–10 yr", "> 1 in 3 yr"}
	impBands        = []float64{10e3, 100e3, 500e3, 2.5e6, 12.5e6, 100e6}
	impLabels       = []string{"< €100k", "€100k–500k", "€500k–2.5m", "€2.5m–12.5m", "> €12.5m"}
	cellShadeLabels = []string{"< €3k", "€3k–20k", "€20k–100k", "€100k–500k", "> €500k"}
)

func bandIdx(edges []float64, v float64) int {
	for i := 0; i < len(edges)-2; i++ {
		if v < edges[i+1] {
			return i
		}
	}
	return len(edges) - 2
}

// cellShade is the expected-loss band of a matrix cell's geometric centre.
func cellShade(li, ii int) int {
	pc := math.Sqrt(likeBands[li] * likeBands[li+1])
	ic := math.Sqrt(impBands[ii] * impBands[ii+1])
	e := pc * ic
	switch {
	case e < 3e3:
		return 0
	case e < 2e4:
		return 1
	case e < 1e5:
		return 2
	case e < 5e5:
		return 3
	default:
		return 4
	}
}

// ---------- view 3: dotplot ----------

const reserve = 1.5e6

func quantileDots(q int) []float64 {
	qs := make([]float64, q)
	for i := range qs {
		qs[i] = simRes[int(math.Floor((float64(i)+0.5)/float64(q)*simYears))]
	}
	return qs
}

// ---------- view 4: KRIs ----------

var months = []string{"Nov", "Dec", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct"}

type Kri struct {
	Name        string
	Unit        string
	LowerBetter bool
	Min, Max    float64
	Hist        []float64
	Appetite    []float64
	Tolerance   []float64
	Note        string
}

func fill12(v float64) []float64 {
	s := make([]float64, 12)
	for i := range s {
		s[i] = v
	}
	return s
}

var kris = []Kri{
	{Name: "Critical patches overdue more than 14 days", LowerBetter: true, Max: 16,
		Hist:      []float64{11, 9, 12, 10, 8, 9, 7, 6, 8, 5, 7, 6},
		Appetite:  []float64{10, 10, 10, 10, 10, 10, 5, 5, 5, 5, 5, 5},
		Tolerance: []float64{14, 14, 14, 14, 14, 14, 10, 10, 10, 10, 10, 10},
		Note:      "Appetite tightened from 10 to 5 in May"},
	{Name: "Phishing test click rate", Unit: "%", LowerBetter: true, Max: 12,
		Hist:     []float64{9.1, 8.4, 7.9, 7.2, 6.8, 6.1, 5.9, 5.2, 4.8, 4.9, 4.4, 4.2},
		Appetite: fill12(5), Tolerance: fill12(8), Note: "Thresholds unchanged"},
	{Name: "Backup restore tests passed, last 90 days", Unit: "%", LowerBetter: false, Min: 70, Max: 100,
		Hist:     []float64{97, 98, 96, 95, 97, 93, 91, 88, 90, 94, 93, 92},
		Appetite: fill12(95), Tolerance: fill12(90), Note: "Thresholds unchanged"},
	{Name: "Privileged accounts without MFA", LowerBetter: true, Max: 8,
		Hist:     []float64{5, 4, 4, 3, 2, 2, 1, 1, 0, 0, 1, 3},
		Appetite: fill12(0), Tolerance: fill12(2), Note: "Thresholds unchanged"},
}

// StatusE is a test or indicator status. The prototype's rule: never colour
// alone — every status carries an icon and a label.
type StatusE uint8

const (
	StatusGood StatusE = iota
	StatusWarn
	StatusCrit
	StatusNone
)

func kriStatus(k Kri, i int) StatusE {
	v, a, t := k.Hist[i], k.Appetite[i], k.Tolerance[i]
	var inA, inT bool
	if k.LowerBetter {
		inA, inT = v <= a, v <= t
	} else {
		inA, inT = v >= a, v >= t
	}
	switch {
	case inA:
		return StatusGood
	case inT:
		return StatusWarn
	default:
		return StatusCrit
	}
}

func fmtK(k Kri, v float64) string {
	if k.Unit == "%" {
		d := 0
		if math.Mod(v, 1) != 0 {
			d = 1
		}
		return strconv.FormatFloat(v, 'f', d, 64) + k.Unit
	}
	return fmtNum(v) + k.Unit
}

// ---------- view 5: bow-tie ----------

type Barrier struct {
	Name   string
	Status StatusE
	Detail string
}

type BowItem struct {
	Text     string
	Barriers []Barrier
}

var bowTop = "Ransomware encrypts core ERP"

var bowThreats = []BowItem{
	{Text: "Phishing steals credentials", Barriers: []Barrier{{"MFA on all remote access", StatusGood, "Tested 12 Sep · 0 bypasses in 40 attempts"}, {"Mail filtering and sandbox", StatusWarn, "Tested 3 Aug · 6 of 50 test payloads delivered"}}},
	{Text: "Unpatched VPN appliance", Barriers: []Barrier{{"Known-exploited patches within 14 days", StatusWarn, "6 critical patches overdue (view 4)"}}},
	{Text: "Supplier remote access abused", Barriers: []Barrier{{"Supplier access through PAM only", StatusNone, "No test recorded since Jul last year"}, {"Session recording and review", StatusCrit, "Tested 20 Sep · 3 of 10 sessions unrecorded"}}},
}

var bowCons = []BowItem{
	{Text: "Production halt over 48 h", Barriers: []Barrier{{"Offline backups, restore drill", StatusWarn, "Restore tests 92% passed, appetite 95% (view 4)"}, {"Manual order fallback", StatusGood, "Exercised 14 Jun · 6 h to switch"}}},
	{Text: "Notification deadline missed", Barriers: []Barrier{{"Incident playbook with 24 h report step", StatusGood, "Tabletop 2 Oct · report drafted in 9 h"}}},
	{Text: "Data leaked for extortion", Barriers: []Barrier{{"Egress monitoring and EDR containment", StatusCrit, "Red team 18 Sep · exfiltration undetected for 11 h"}}},
}

func barrierLabel(s StatusE) string {
	switch s {
	case StatusGood:
		return "Tested, effective"
	case StatusWarn:
		return "Tested, degraded"
	case StatusCrit:
		return "Tested, failed"
	default:
		return "Not tested in 12 months"
	}
}

func kriLabel(s StatusE) string {
	switch s {
	case StatusGood:
		return "Within appetite"
	case StatusWarn:
		return "Within tolerance"
	default:
		return "Beyond tolerance"
	}
}

// ---------- view 6: tornado ----------

type TornadoItem struct {
	Label, Range, Name string
	Lo, Hi, W          float64
}

func tornadoItems() (base float64, items []TornadoItem) {
	type over struct {
		id   string
		kind byte
		v    float64
	}
	eal := func(o *over) (s float64) {
		for _, r := range risks {
			p := gmean(r.P)
			if o != nil && o.id == r.Id && o.kind == 'p' {
				p = o.v
			}
			mi := meanImpact(r.I)
			if o != nil && o.id == r.Id && o.kind == 'i' {
				mi *= o.v
			}
			s += p * mi
		}
		return
	}
	base = eal(nil)
	for _, r := range risks {
		items = append(items, TornadoItem{Label: r.Id + " likelihood", Name: r.Name,
			Lo: eal(&over{r.Id, 'p', r.P[0]}), Hi: eal(&over{r.Id, 'p', r.P[1]}),
			Range: pct(r.P[0], 1) + "–" + pct(r.P[1], 0) + " / yr"})
		_, s := lnParams(r.I)
		f := math.Exp(z90 * s)
		items = append(items, TornadoItem{Label: r.Id + " impact", Name: r.Name,
			Lo: eal(&over{r.Id, 'i', 1 / f}), Hi: eal(&over{r.Id, 'i', f}),
			Range: "mean impact ÷/× " + strconv.FormatFloat(f, 'f', 1, 64)})
	}
	for i := range items {
		items[i].W = items[i].Hi - items[i].Lo
	}
	// JS Array.prototype.sort is stable; so is this.
	sort.SliceStable(items, func(a, b int) bool { return items[a].W > items[b].W })
	items = items[:8]
	return
}

// ---------- view 7: calibration ----------

type CalSeries struct {
	Name string
	Slot int          // qualitative palette slot
	Pts  [][3]float64 // estimated, observed share, count
}

var calib = []CalSeries{
	{Name: "2024 register", Slot: slotInherent, Pts: [][3]float64{{0.05, 0.14, 40}, {0.15, 0.25, 35}, {0.30, 0.38, 22}, {0.50, 0.46, 15}, {0.70, 0.57, 12}, {0.90, 0.70, 10}}},
	{Name: "2025 register, after calibration training", Slot: slotResidual, Pts: [][3]float64{{0.05, 0.07, 45}, {0.15, 0.17, 38}, {0.30, 0.31, 25}, {0.50, 0.52, 18}, {0.70, 0.66, 14}, {0.90, 0.85, 9}}},
}

func brier(s CalSeries) float64 {
	n, b := 0.0, 0.0
	for _, p := range s.Pts {
		n += p[2]
		b += p[2] * (p[1]*(1-p[0])*(1-p[0]) + (1-p[1])*p[0]*p[0])
	}
	return b / n
}

// ---------- view 8: incidents ----------

var lanes = []string{"IT operations", "Security", "Suppliers", "Finance"}

type Incident struct {
	Month    float64
	Lane     int
	Loss     float64
	NearMiss bool
	Text     string
}

var incidents = []Incident{
	{0.4, 0, 120e3, false, "Storage controller failure"}, {1.6, 3, 45e3, false, "Duplicate supplier payment"}, {2.2, 1, 0, true, "Phishing click, no access gained"},
	{3.1, 2, 260e3, false, "Carrier strike delays dispatch"}, {4.5, 1, 0, true, "VPN exploit attempt blocked"}, {5.0, 3, 180e3, false, "Fake invoice paid"},
	{6.3, 0, 60e3, false, "Failed release rollback"}, {7.8, 1, 410e3, false, "Account takeover, data copied"}, {8.6, 3, 0, true, "Fake invoice stopped at callback"},
	{9.9, 2, 0, true, "Supplier credit downgrade"}, {11.2, 0, 30e3, false, "DNS misconfiguration"}, {12.5, 1, 0, true, "Phishing click, MFA held"},
	{13.4, 3, 0, true, "Fake invoice stopped at callback"}, {14.8, 2, 520e3, false, "Sole supplier factory fire"}, {16.0, 1, 0, true, "Supplier session unrecorded"},
	{17.2, 0, 90e3, false, "Backup restore failed in drill"}, {18.9, 1, 0, true, "Ransomware beacon contained"}, {20.1, 3, 25e3, false, "Card fraud chargebacks"},
	{21.4, 1, 0, true, "Red team exfiltration undetected"}, {22.7, 0, 15e3, false, "Certificate expiry outage"}, {23.5, 1, 0, true, "Phishing click, MFA held"},
}

type ControlChange struct {
	Month float64
	Text  string
}

var controlChanges = []ControlChange{{6.8, "MFA on remote access"}, {10.5, "Invoice callback rule"}, {17.6, "Supplier PAM"}}

var monthLabels = []string{"Nov 24", "Feb 25", "May 25", "Aug 25", "Nov 25", "Feb 26", "May 26", "Aug 26"}

// ---------- view 9: impact tolerance ----------

type Scenario struct {
	Name string
	Slot int
}

var scenarios = []Scenario{{"Ransomware", slotInherent}, {"Cloud region outage", slotResidual}, {"Key supplier failure", slotThird}}

type Service struct {
	Name string
	Tol  float64   // hours
	Est  []float64 // hours, one per scenario
}

var services = []Service{
	{"Order intake", 24, []float64{72, 10, 4}},
	{"Warehouse dispatch", 12, []float64{48, 6, 30}},
	{"Payroll", 72, []float64{60, 8, 2}},
	{"Customer support", 48, []float64{30, 20, 1}},
}

func fmtHours(h float64) string {
	if h < 24 {
		return fmtNum(h) + " h"
	}
	return fmtNum(h/24) + " d"
}

func fmtHoursTip(h float64) string {
	if h < 24 {
		return fmtNum(h) + " h"
	}
	return strconv.FormatFloat(h/24, 'f', 1, 64) + " d"
}
