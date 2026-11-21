package tiff

import (
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// GeoKey ids read here (GeoTIFF 1.1 §7).
const (
	keyModelType        = 1024
	keyRasterType       = 1025
	keyGeodeticCRS      = 2048
	keyGeogAngularUnits = 2054
	keyProjectedCRS     = 3072
	keyProjLinearUnits  = 3076
	keyVerticalCRS      = 4096

	modelTypeProjected  = 1
	modelTypeGeographic = 2
	modelTypeGeocentric = 3

	rasterPixelIsArea  = 1
	rasterPixelIsPoint = 2

	userDefined     = 32767
	unitMetre       = 9001
	unitDegree      = 9102
	geoKeyHeaderLen = 4
)

// geoKeys are the GeoKeyDirectory's keys whose value is one SHORT; keys
// stored elsewhere are recorded as present.
type geoKeys struct {
	shorts  map[uint16]uint16
	present map[uint16]bool
}

func readGeoKeys(f *file, d *dir) (keys geoKeys, err error) {
	keys.shorts = make(map[uint16]uint16, 8)
	keys.present = make(map[uint16]bool, 8)
	e, ok := d.entries[tagGeoKeyDirectory]
	if !ok {
		return keys, nil
	}
	v, err := f.uints(e, maxSmallValue)
	if err != nil {
		return
	}
	if len(v) < geoKeyHeaderLen {
		return keys, malformed("the GeoKeyDirectory is shorter than its header")
	}
	if v[0] != 1 {
		return keys, unsupported("GeoKeyDirectory version " + itoa(v[0]))
	}
	n := v[3]
	if n > uint64(len(v)-geoKeyHeaderLen)/4 {
		return keys, malformed("the GeoKeyDirectory counts more keys than it holds")
	}
	for i := range n {
		k := v[geoKeyHeaderLen+4*i:]
		id, location, value := uint16(k[0]), k[1], k[3]
		keys.present[id] = true
		if location == 0 {
			keys.shorts[id] = uint16(value)
		}
	}
	return keys, nil
}

// placementOf is the placement the first directory states (ADR-0398 §SD4,
// §SD6): from GeoTIFF's tags when it has them, otherwise from its
// resolution in a physical unit. What cannot be an affine is left
// unplaced, with a note saying why.
func placementOf(f *file, d *dir) (p imaging.Placement, notes []string) {
	_, hasScale := d.entries[tagModelPixelScale]
	_, hasTie := d.entries[tagModelTiepoint]
	_, hasTransform := d.entries[tagModelTransform]
	if !hasScale && !hasTie && !hasTransform {
		return resolutionPlacement(f, d)
	}
	unplaced := func(why string) (imaging.Placement, []string) {
		return imaging.Placement{}, append(notes, "Not placed: "+why+".")
	}
	keys, err := readGeoKeys(f, d)
	if err != nil {
		return unplaced("the GeoKeyDirectory cannot be read (" + err.Error() + ")")
	}
	var a [6]float64
	var source []string
	if hasTransform {
		m, terr := f.floats(d.entries[tagModelTransform], maxSmallValue)
		switch {
		case terr != nil:
			return unplaced("ModelTransformation cannot be read (" + terr.Error() + ")")
		case len(m) != 16:
			return unplaced("ModelTransformation does not hold 16 values")
		case m[12] != 0 || m[13] != 0 || m[14] != 0 || m[15] != 1:
			return unplaced("ModelTransformation is projective, not affine")
		case m[8] != 0 || m[9] != 0:
			return unplaced("ModelTransformation tilts the raster out of the plane")
		}
		// x = m0·I + m1·J + m3, y = m4·I + m5·J + m7 at raster (I, J); the
		// K column multiplies a raster height of 0.
		a = [6]float64{m[3], m[0], m[1], m[7], m[4], m[5]}
		source = append(source, "ModelTransformation")
		if hasScale || hasTie {
			notes = append(notes, "ModelPixelScale or ModelTiepoint are present beside ModelTransformation and were not used.")
		}
	} else {
		var t, s []float64
		var terr, serr error
		if hasTie {
			t, terr = f.floats(d.entries[tagModelTiepoint], maxSmallValue)
		}
		if hasScale {
			s, serr = f.floats(d.entries[tagModelPixelScale], maxSmallValue)
		}
		switch {
		case terr != nil || serr != nil:
			return unplaced("ModelTiepoint or ModelPixelScale cannot be read")
		case len(t) > 6 && len(t)%6 == 0:
			return unplaced(strconv.Itoa(len(t)/6) + " tiepoints without ModelTransformation describe a warp, not an affine")
		case !hasTie || !hasScale:
			return unplaced("a GeoTIFF needs ModelTiepoint and ModelPixelScale together, or ModelTransformation")
		case len(t) != 6 || len(s) < 2:
			return unplaced("ModelTiepoint or ModelPixelScale holds the wrong number of values")
		}
		// The tiepoint ties raster (I, J) to model (X, Y); the scale is
		// model units per pixel, y growing upwards as rows grow downwards.
		ti, tj, tx, ty, sx, sy := t[0], t[1], t[3], t[4], s[0], s[1]
		a = [6]float64{tx - ti*sx, sx, 0, ty + tj*sy, 0, -sy}
		source = append(source, "ModelPixelScale", "ModelTiepoint")
	}

	raster, stated := keys.shorts[keyRasterType]
	switch {
	case !stated:
		source = append(source, "GTRasterType absent (read as PixelIsArea)")
	case raster == rasterPixelIsArea:
		source = append(source, "PixelIsArea")
	case raster == rasterPixelIsPoint:
		// PixelIsPoint ties raster coordinate (I, J) to the centre of pixel
		// (I, J), which in this engine's coordinates — pixel (col, row)
		// covers [col, col+1) × [row, row+1) — is (I + ½, J + ½). So the
		// engine's (col, row) is the file's (col − ½, row − ½), and the
		// origin moves by minus half a column and half a row: for a north-up
		// raster half a pixel west and half a pixel north.
		a[0] -= (a[1] + a[2]) / 2
		a[3] -= (a[4] + a[5]) / 2
		source = append(source, "PixelIsPoint (origin moved half a pixel up and left)")
	default:
		return unplaced("GTRasterType " + strconv.Itoa(int(raster)) + " is neither PixelIsArea nor PixelIsPoint")
	}

	p.Affine = a
	p.Frame.Kind = imaging.FrameKindReferenceSystem
	modelType, hasModelType := keys.shorts[keyModelType]
	if !hasModelType {
		switch {
		case keys.present[keyProjectedCRS]:
			modelType = modelTypeProjected
		case keys.present[keyGeodeticCRS]:
			modelType = modelTypeGeographic
		}
	}
	codeKey := uint16(0)
	switch modelType {
	case modelTypeProjected:
		codeKey = keyProjectedCRS
		p.Frame.AxisNames = [2]string{"E", "N"}
		if keys.shorts[keyProjLinearUnits] == unitMetre {
			p.Frame.Units = [2]string{"m", "m"}
		}
	case modelTypeGeographic:
		codeKey = keyGeodeticCRS
		p.Frame.AxisNames = [2]string{"lon", "lat"}
		if keys.shorts[keyGeogAngularUnits] == unitDegree {
			p.Frame.Units = [2]string{"deg", "deg"}
		}
	case modelTypeGeocentric:
		return unplaced("the model is geocentric, whose coordinates are not a plane")
	default:
		p.Frame.AxisNames = [2]string{"x", "y"}
		if _, ok := d.entries[tagGeoKeyDirectory]; ok {
			p.Unapplied = append(p.Unapplied, "GTModelType "+strconv.Itoa(int(modelType))+": the kind of reference system is not stated")
		} else {
			p.Unapplied = append(p.Unapplied, "no GeoKeyDirectory: the reference system is not stated")
		}
	}
	if codeKey != 0 {
		code, ok := keys.shorts[codeKey]
		name := "ProjectedCRS"
		if codeKey == keyGeodeticCRS {
			name = "GeodeticCRS"
		}
		switch {
		case ok && code == userDefined:
			p.Frame.Code = "user-defined"
			p.Unapplied = append(p.Unapplied, name+" is user-defined (32767): the GeoKeys that define it are not evaluated")
		case ok && code != 0:
			p.Frame.Code = "EPSG:" + strconv.Itoa(int(code))
			source = append(source, name)
		default:
			p.Unapplied = append(p.Unapplied, name+" is not stated as an EPSG code")
		}
	}
	if v, ok := keys.shorts[keyVerticalCRS]; ok {
		p.Unapplied = append(p.Unapplied, "VerticalCRS "+strconv.Itoa(int(v))+": heights are not part of the placement")
	} else if keys.present[keyVerticalCRS] {
		p.Unapplied = append(p.Unapplied, "VerticalCRS: heights are not part of the placement")
	}
	p.Source = "GeoTIFF " + strings.Join(source, ", ")
	if err = p.Validate(); err != nil {
		return unplaced("the GeoTIFF affine cannot be evaluated (" + err.Error() + ")")
	}
	return p, notes
}

// resolutionPlacement is a physical scale from XResolution and YResolution
// in inches or centimetres (TIFF 6.0 §8: pixels per ResolutionUnit, which
// defaults to inch). ResolutionUnit 1, "no absolute unit", places nothing.
func resolutionPlacement(f *file, d *dir) (p imaging.Placement, notes []string) {
	ex, okX := d.entries[tagXResolution]
	ey, okY := d.entries[tagYResolution]
	if !okX || !okY {
		return
	}
	unit := uint64(2)
	source := "XResolution, YResolution, ResolutionUnit absent (inch by TIFF 6.0)"
	if e, ok := d.entries[tagResolutionUnit]; ok {
		v, err := f.firstUint(e)
		if err != nil {
			return p, []string{"ResolutionUnit cannot be read; the resolution was not applied."}
		}
		unit = v
		source = "XResolution, YResolution, ResolutionUnit"
	}
	var u string
	switch unit {
	case 1:
		return
	case 2:
		u = "in"
	case 3:
		u = "cm"
	default:
		return p, []string{"ResolutionUnit " + itoa(unit) + " is not a unit TIFF defines; the resolution was not applied."}
	}
	xs, errX := f.floats(ex, 64)
	ys, errY := f.floats(ey, 64)
	if errX != nil || errY != nil || len(xs) == 0 || len(ys) == 0 ||
		!(xs[0] > 0) || !(ys[0] > 0) || math.IsInf(xs[0], 0) || math.IsInf(ys[0], 0) {
		return p, []string{"XResolution or YResolution is not a positive number; the resolution was not applied."}
	}
	p = imaging.Placement{
		Affine: [6]float64{0, 1 / xs[0], 0, 0, 0, 1 / ys[0]},
		Frame:  imaging.Frame{Kind: imaging.FrameKindPhysical, AxisNames: [2]string{"x", "y"}, Units: [2]string{u, u}},
		Source: source,
	}
	if p.Validate() != nil {
		return imaging.Placement{}, []string{"XResolution or YResolution gives no invertible scale; the resolution was not applied."}
	}
	return p, nil
}
