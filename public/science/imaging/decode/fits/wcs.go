package fits

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// World coordinates (ADR-0398 §SD7, §SD4), read from the keywords of
// Greisen & Calabretta 2002 (Paper I) and Calabretta & Greisen 2002
// (Paper II) as the FITS Standard 4.0 §8 restates them. Only the primary
// description (no letter suffix) is read.

// ctypeParts is a CTYPE value split as Paper I §2.1.1 writes it: a
// coordinate type, and in the "4–3" form an algorithm code and anything
// after it (a distortion suffix such as "-SIP").
type ctypeParts struct {
	raw, typ, alg, suffix string
}

func splitCtype(s string) ctypeParts {
	s = strings.TrimRight(s, " ")
	p := ctypeParts{raw: s, typ: s}
	if len(s) >= 8 && s[4] == '-' {
		p.typ = strings.TrimRight(s[:4], "-")
		p.alg = strings.TrimRight(s[5:8], " -")
		p.suffix = s[8:]
	}
	return p
}

// celestial classifies a coordinate type (Standard §8.3): lon or lat, and
// the family the pair must share; ok false for a type that is not
// celestial.
func (inst ctypeParts) celestial() (isLon bool, family string, ok bool) {
	t := inst.typ
	switch {
	case t == "RA":
		return true, "RA", true
	case t == "DEC":
		return false, "RA", true
	case len(t) == 4 && t[1:] == "LON":
		return true, t[:1], true
	case len(t) == 4 && t[1:] == "LAT":
		return false, t[:1], true
	case len(t) == 4 && t[2:] == "LN":
		return true, t[:2], true
	case len(t) == 4 && t[2:] == "LT":
		return false, t[:2], true
	}
	return false, "", false
}

// wcsReading is what the header says about world coordinates: the
// placement of the image axes, coordinates for the axes beyond them, and
// notes for a person.
type wcsReading struct {
	placement imaging.Placement
	// coords and units are by FITS axis number; coords is nil for an axis
	// whose coordinates are not read.
	coords map[int][]float64
	units  map[int]string
	notes  []string
}

func (inst *wcsReading) note(s string) { inst.notes = append(inst.notes, s) }

func axisKw(prefix string, i int) string { return prefix + strconv.Itoa(i) }

func pairKw(prefix string, i, j int) string {
	return prefix + strconv.Itoa(i) + "_" + strconv.Itoa(j)
}

// indexPair parses PREFIXi_j with i and j in 1 … 99 and no leading zeros.
func indexPair(kw, prefix string) (i, j int, ok bool) {
	rest, found := strings.CutPrefix(kw, prefix)
	if !found {
		return
	}
	a, b, found := strings.Cut(rest, "_")
	if !found {
		return
	}
	i, okA := axisIndex(a)
	j, okB := axisIndex(b)
	return i, j, okA && okB
}

func axisIndex(s string) (int, bool) {
	if s == "" || len(s) > 2 || s[0] == '0' || digits(s) != len(s) {
		return 0, false
	}
	v, _ := strconv.Atoi(s)
	return v, true
}

// parameterIndex parses PVi_m, whose m may be 0.
func parameterIndex(kw, prefix string) (i, m int, ok bool) {
	rest, found := strings.CutPrefix(kw, prefix)
	if !found {
		return
	}
	a, b, found := strings.Cut(rest, "_")
	if !found {
		return
	}
	i, okA := axisIndex(a)
	if b == "0" {
		return i, 0, okA
	}
	m, okB := axisIndex(b)
	return i, m, okA && okB
}

var altIndexed = []string{"CTYPE", "CUNIT", "CRVAL", "CDELT", "CRPIX", "CNAME", "CRDER", "CSYER"}
var altPaired = []string{"CD", "PC", "PV", "PS"}
var altPlain = []string{"LONPOLE", "LATPOLE", "RADESYS", "EQUINOX", "WCSNAME", "WCSAXES", "RESTFRQ", "RESTWAV", "SPECSYS"}

// altLetter reports the letter of a keyword of an alternate description
// (Standard §8.2.1): a WCS keyword with a suffix A … Z.
func altLetter(kw string) (byte, bool) {
	if len(kw) < 2 {
		return 0, false
	}
	a := kw[len(kw)-1]
	if a < 'A' || a > 'Z' {
		return 0, false
	}
	base := kw[:len(kw)-1]
	if slices.Contains(altPlain, base) {
		return a, true
	}
	for _, p := range altIndexed {
		if rest, ok := strings.CutPrefix(base, p); ok {
			if _, ok = axisIndex(rest); ok {
				return a, true
			}
		}
	}
	for _, p := range altPaired {
		if _, _, ok := parameterIndex(base, p); ok {
			return a, true
		}
	}
	return 0, false
}

var distortionKeywords = []string{"A_ORDER", "B_ORDER", "AP_ORDER", "BP_ORDER", "CPDIS1", "CPDIS2", "CQDIS1", "CQDIS2", "D2IMDIS1", "D2IMDIS2"}

// celestialUnit is the factor from a celestial axis's CUNIT to degrees.
func celestialUnit(u string) (f float64, ok bool) {
	switch u {
	case "", "deg":
		return 1, true
	case "arcmin":
		return 1.0 / 60, true
	case "arcsec":
		return 1.0 / 3600, true
	case "mas":
		return 1.0 / 3_600_000, true
	case "rad":
		return 180 / math.Pi, true
	}
	return 0, false
}

func formatNum(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// readWCS reads the world coordinates of an HDU whose axis lengths are
// axes. The placement is the zero one, with a note for each reason, when
// the header declares anything that would make the affine alone misplace
// pixels.
func readWCS(h *header, axes []int64) (w wcsReading) {
	w.coords = make(map[int][]float64)
	w.units = make(map[int]string)
	naxis := len(axes)
	n := naxis
	if v, present, valid := h.integer("WCSAXES"); present && valid && v > int64(n) && v <= 99 {
		n = int(v)
	}
	// What the header declares, by kind, for the axes up to 99.
	hasCD, hasPC, matrixOnImage := false, false, false
	pvs := make([][3]float64, 0, 4) // i, m, value
	alts := make([]byte, 0, 2)
	distortions := make([]string, 0, 2)
	for _, c := range h.cards {
		kw := c.Keyword
		if i, j, ok := indexPair(kw, "CD"); ok {
			hasCD = true
			n = max(n, i, j)
			matrixOnImage = matrixOnImage || i <= 2 || j <= 2
		} else if i, j, ok := indexPair(kw, "PC"); ok {
			hasPC = true
			n = max(n, i, j)
			matrixOnImage = matrixOnImage || i <= 2 || j <= 2
		} else if i, m, ok := parameterIndex(kw, "PV"); ok {
			v, _, _ := h.real(kw)
			pvs = append(pvs, [3]float64{float64(i), float64(m), v})
		} else if a, ok := altLetter(kw); ok {
			if !slices.Contains(alts, a) {
				alts = append(alts, a)
			}
		} else if slices.Contains(distortionKeywords, kw) {
			distortions = append(distortions, kw)
		}
	}
	slices.Sort(alts)
	if len(alts) > 0 {
		letters := make([]string, len(alts))
		for i, a := range alts {
			letters[i] = string(a)
		}
		w.note("alternate world coordinate descriptions " + strings.Join(letters, ", ") + " are declared and not read")
	}
	if naxis < 2 {
		return
	}
	n = min(n, 99)

	ctype := make([]ctypeParts, n+1)
	cunit := make([]string, n+1)
	crval, cdelt, crpix := make([]float64, n+1), make([]float64, n+1), make([]float64, n+1)
	hasCrval, hasCdelt, hasCrpix := make([]bool, n+1), make([]bool, n+1), make([]bool, n+1)
	anyOnImage := matrixOnImage
	for i := 1; i <= n; i++ {
		s, present := h.str(axisKw("CTYPE", i))
		ctype[i] = splitCtype(s)
		u, uPresent := h.str(axisKw("CUNIT", i))
		cunit[i] = strings.TrimSpace(u)
		cdelt[i] = 1
		v, p, ok := h.real(axisKw("CRVAL", i))
		if p && ok {
			crval[i], hasCrval[i] = v, true
		}
		if v, p, ok := h.real(axisKw("CDELT", i)); p && ok {
			cdelt[i], hasCdelt[i] = v, true
		}
		if v, p, ok := h.real(axisKw("CRPIX", i)); p && ok {
			crpix[i], hasCrpix[i] = v, true
		}
		if i <= 2 && (present || uPresent || p || hasCdelt[i] || hasCrpix[i] || h.has(axisKw("CROTA", i))) {
			anyOnImage = true
		}
	}

	// The linear part of Paper I eqs. (1)–(3): m[i][j] is the matrix with
	// the scale folded in, so that x_i = Σ_j m[i][j]·(p_j − r_j).
	m := make([][]float64, n+1)
	for i := range m {
		m[i] = make([]float64, n+1)
	}
	source := ""
	lonAxis, latAxis := 0, 0
	isLon1, fam1, cel1 := ctype[1].celestial()
	isLon2, fam2, cel2 := ctype[2].celestial()
	celestialPair := cel1 && cel2 && fam1 == fam2 && isLon1 != isLon2
	if celestialPair {
		if isLon1 {
			lonAxis, latAxis = 1, 2
		} else {
			lonAxis, latAxis = 2, 1
		}
	}
	unplaced := make([]string, 0, 4)
	cr1, has1, _ := h.real("CROTA1")
	cr2, has2, _ := h.real("CROTA2")
	switch {
	case hasCD:
		for i := 1; i <= n; i++ {
			for j := 1; j <= n; j++ {
				if v, p, ok := h.real(pairKw("CD", i, j)); p && ok {
					m[i][j] = v
				}
			}
		}
		source = "CDi_j"
		if hasPC {
			w.note("the header gives both CDi_j and PCi_j, which the standard forbids; CDi_j was used")
		}
	case hasPC:
		for i := 1; i <= n; i++ {
			for j := 1; j <= n; j++ {
				pc := 0.0
				if i == j {
					pc = 1
				}
				if v, p, ok := h.real(pairKw("PC", i, j)); p && ok {
					pc = v
				}
				m[i][j] = cdelt[i] * pc
			}
		}
		source = "PCi_j with CDELTi"
		if has1 || has2 {
			w.note("CROTAi was ignored: the header gives PCi_j, with which it must not appear")
		}
	default:
		for i := 1; i <= n; i++ {
			m[i][i] = cdelt[i]
		}
		source = "CDELTi"
		if has1 || has2 {
			source = "CROTA2 with CDELTi"
			switch {
			case celestialPair && latAxis == 2 && (cr1 == 0 || cr1 == cr2):
				// Calabretta & Greisen 2002 §6.1: the rotation is CROTA of
				// the latitude axis, folded into the matrix as
				// CD1_1 = CDELT1 cos ρ, CD1_2 = −CDELT2 sin ρ,
				// CD2_1 = CDELT1 sin ρ, CD2_2 = CDELT2 cos ρ.
				s, c := math.Sincos(cr2 * math.Pi / 180)
				m[1][1], m[1][2] = cdelt[1]*c, -cdelt[2]*s
				m[2][1], m[2][2] = cdelt[1]*s, cdelt[2]*c
			case cr1 == 0 && cr2 == 0:
			default:
				unplaced = append(unplaced, "CROTA1 = "+formatNum(cr1)+", CROTA2 = "+formatNum(cr2)+" rotate axes other than a longitude–latitude pair with latitude second")
			}
		}
	}

	// Axes beyond the image's: coordinates linear in the 1-based pixel
	// index, where the axis is neither coupled to another nor non-linear.
	for k := 3; k <= naxis && k <= n; k++ {
		coupled := false
		for j := 1; j <= n; j++ {
			if j != k && (m[k][j] != 0 || m[j][k] != 0) {
				coupled = true
			}
		}
		scaled := hasCdelt[k]
		if hasCD {
			_, p, ok := h.real(pairKw("CD", k, k))
			scaled = p && ok
		}
		given := hasCrval[k] && hasCrpix[k] && scaled
		if !given {
			continue
		}
		label := "axis " + strconv.Itoa(k)
		if ctype[k].raw != "" {
			label += " (" + ctype[k].raw + ")"
		}
		switch {
		case coupled:
			w.note(label + " is coupled to another axis by the transformation matrix; its coordinates are not read")
		case ctype[k].alg != "":
			w.note(label + " declares the non-linear algorithm " + ctype[k].alg + "; its coordinates are not read")
		default:
			cs := make([]float64, axes[k-1])
			for q := range cs {
				cs[q] = crval[k] + m[k][k]*(float64(q+1)-crpix[k])
			}
			w.coords[k] = cs
			w.units[k] = cunit[k]
		}
	}

	if !anyOnImage && len(pvs) == 0 && len(distortions) == 0 {
		return
	}
	// Coupling between the image's axes and any other.
	for i := 1; i <= 2; i++ {
		for j := 3; j <= n; j++ {
			if m[i][j] != 0 || m[j][i] != 0 {
				unplaced = append(unplaced, "the transformation matrix couples axis "+strconv.Itoa(i)+" with axis "+strconv.Itoa(j))
			}
		}
	}
	for _, kw := range distortions {
		unplaced = append(unplaced, kw+" declares a distortion")
	}
	for k := 3; k <= n; k++ {
		if _, _, ok := ctype[k].celestial(); ok {
			unplaced = append(unplaced, "CTYPE"+strconv.Itoa(k)+" = '"+ctype[k].raw+"' puts a celestial axis outside the image's two axes")
		}
	}
	kind := imaging.FrameKindPhysical
	switch {
	case celestialPair:
		kind = imaging.FrameKindCelestialGnomonic
		for _, i := range [...]int{lonAxis, latAxis} {
			c := ctype[i]
			switch {
			case c.alg != "TAN":
				what := "no projection code"
				if c.alg != "" {
					what = "the " + c.alg + " projection"
				}
				if c.alg == "TPV" || c.alg == "TNX" || c.alg == "ZPX" {
					what = "the " + c.alg + " distortion convention"
				}
				unplaced = append(unplaced, "CTYPE"+strconv.Itoa(i)+" = '"+c.raw+"' declares "+what)
			case c.suffix != "":
				what := "the suffix " + c.suffix
				if c.suffix == "-SIP" {
					what = "SIP distortion"
				}
				unplaced = append(unplaced, "CTYPE"+strconv.Itoa(i)+" = '"+c.raw+"' declares "+what)
			}
		}
		for _, pv := range pvs {
			i, mm, v := int(pv[0]), int(pv[1]), pv[2]
			kw := pairKw("PV", i, mm)
			switch {
			case i == lonAxis && mm == 1 && v == 0, i == lonAxis && mm == 2 && v == 90:
			case i == lonAxis && (mm == 3 || mm == 4):
				// LONPOLE and LATPOLE by another name (Standard Table 21).
			case i == lonAxis || i == latAxis:
				unplaced = append(unplaced, kw+" = "+formatNum(v)+" alters the TAN projection")
			}
		}
	case cel1 || cel2:
		unplaced = append(unplaced, "CTYPE1 = '"+ctype[1].raw+"' and CTYPE2 = '"+ctype[2].raw+"' are not a longitude–latitude pair")
	default:
		if ctype[1].raw == "" && ctype[2].raw == "" && !hasCD && !hasPC && !hasCdelt[1] && !hasCdelt[2] {
			unplaced = append(unplaced, "the header gives no scale for axes 1 and 2 (no CDELTi, CDi_j or PCi_j)")
		}
		for _, i := range [...]int{1, 2} {
			if ctype[i].alg != "" {
				unplaced = append(unplaced, "CTYPE"+strconv.Itoa(i)+" = '"+ctype[i].raw+"' declares the non-linear algorithm "+ctype[i].alg)
			}
		}
		for _, pv := range pvs {
			if i := int(pv[0]); i == 1 || i == 2 {
				unplaced = append(unplaced, pairKw("PV", i, int(pv[1]))+" gives a parameter to a linear axis")
			}
		}
	}

	rows := [2]int{1, 2}
	if kind == imaging.FrameKindCelestialGnomonic {
		rows = [2]int{lonAxis, latAxis}
	}
	factor := [2]float64{1, 1}
	if kind == imaging.FrameKindCelestialGnomonic {
		for r, i := range rows {
			f, ok := celestialUnit(cunit[i])
			if !ok {
				unplaced = append(unplaced, "CUNIT"+strconv.Itoa(i)+" = '"+cunit[i]+"' is not an angle unit this reader converts")
				continue
			}
			factor[r] = f
			if f != 1 {
				w.note("CUNIT" + strconv.Itoa(i) + " '" + cunit[i] + "' was converted to degrees")
			}
		}
	}
	if len(unplaced) > 0 {
		for _, u := range unplaced {
			w.note(u + ", which is not evaluated: the pixels are not placed")
		}
		return
	}

	// The engine's pixel (col, row) is FITS pixel (col + 0.5, NAXIS2 + 0.5 −
	// row): the centre of the first FITS pixel is (1, 1), and the rows are
	// flipped.
	n2 := float64(axes[1])
	var p imaging.Placement
	for r, i := range rows {
		f := factor[r]
		a0 := f * (m[i][1]*(0.5-crpix[1]) + m[i][2]*(n2+0.5-crpix[2]))
		a1, a2 := f*m[i][1], -f*m[i][2]
		if kind == imaging.FrameKindPhysical {
			a0 += crval[i]
		}
		p.Affine[3*r], p.Affine[3*r+1], p.Affine[3*r+2] = a0, a1, a2
	}
	p.Source = source + " about CRPIXj"
	if kind == imaging.FrameKindCelestialGnomonic {
		lon, lat := crval[lonAxis]*factor[0], crval[latAxis]*factor[1]
		pole := imaging.DefaultLonPole(lat)
		if v, present, ok := h.real("LONPOLE"); present && ok {
			pole = v
		} else {
			for _, pv := range pvs {
				if int(pv[0]) == lonAxis && int(pv[1]) == 3 {
					pole = pv[2]
				}
			}
		}
		p.Frame = imaging.Frame{
			Kind:      kind,
			Code:      celestialCode(h, fam1),
			AxisNames: celestialNames(ctype[lonAxis], ctype[latAxis]),
			Units:     [2]string{"deg", "deg"},
			RefLon:    lon,
			RefLat:    lat,
			LonPole:   pole,
		}
		p.Source += ", CRVALi, " + ctype[lonAxis].raw + "/" + ctype[latAxis].raw
	} else {
		p.Frame = imaging.Frame{Kind: kind, Units: [2]string{cunit[1], cunit[2]}}
		for r, i := range rows {
			name := ctype[i].raw
			if name == "" {
				name = "axis" + strconv.Itoa(i)
			}
			p.Frame.AxisNames[r] = name
		}
		p.Source += ", CRVALi"
	}
	if len(alts) > 0 {
		for _, a := range alts {
			p.Unapplied = append(p.Unapplied, "alternate description "+string(a))
		}
	}
	if err := p.Validate(); err != nil {
		w.note("the world coordinate keywords give a transformation that cannot be evaluated (" + err.Error() + "): the pixels are not placed")
		return
	}
	w.placement = p
	return
}

func celestialNames(lon, lat ctypeParts) [2]string {
	if lon.typ == "RA" {
		return [2]string{"RA", "Dec"}
	}
	return [2]string{lon.typ, lat.typ}
}

// celestialCode names the celestial system (Standard §8.3): RADESYS, or
// its default from EQUINOX, with the equinox where the system has one.
func celestialCode(h *header, family string) string {
	switch family {
	case "G":
		return "GALACTIC"
	case "S":
		return "SUPERGALACTIC"
	case "RA", "E", "H":
	default:
		return family
	}
	sys, _ := h.str("RADESYS")
	sys = strings.ToUpper(strings.TrimSpace(sys))
	eq, hasEq := 0.0, false
	if v, p, ok := h.real("EQUINOX"); p && ok {
		eq, hasEq = v, true
	} else if v, p, ok := h.real("EPOCH"); p && ok {
		eq, hasEq = v, true
	}
	if sys == "" {
		switch {
		case !hasEq:
			sys = "ICRS"
		case eq < 1984:
			sys = "FK4"
		default:
			sys = "FK5"
		}
	}
	switch sys {
	case "FK4", "FK4-NO-E":
		if !hasEq {
			eq = 1950
		}
		sys += " B" + formatNum(eq)
	case "FK5":
		if !hasEq {
			eq = 2000
		}
		sys += " J" + formatNum(eq)
	}
	switch family {
	case "E":
		return "ECLIPTIC " + sys
	case "H":
		return "HELIOECLIPTIC " + sys
	}
	return sys
}
