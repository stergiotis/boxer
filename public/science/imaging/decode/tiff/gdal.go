package tiff

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// nodataOf is the stored value GDAL_NODATA (tag 42113) declares invalid,
// when the planes can carry it: their kind holds it exactly, and the
// colour model has no alpha. Otherwise it is a note and is not applied.
// "nan" on a float file needs nothing, NaN being invalid already.
func nodataOf(d *dir, model imaging.ColorModelE) (stored float64, ok bool, note string) {
	if !d.hasNodata {
		return 0, false, ""
	}
	quoted := strconv.Quote(d.nodata)
	v, err := strconv.ParseFloat(d.nodata, 64)
	switch {
	case err != nil:
		return 0, false, "GDAL_NODATA " + quoted + " is not a number and was not applied."
	case d.Photometric == photoPalette:
		return 0, false, "GDAL_NODATA " + quoted + " names a palette index, which the expanded colours do not keep; it was not applied."
	case math.IsNaN(v) && d.Kind.IsFloat():
		return 0, false, ""
	case model.HasAlpha():
		return 0, false, "GDAL_NODATA " + quoted + " was not applied: the samples are read with alpha, which marks what is not there."
	}
	probe, perr := probePlane(d.Kind)
	if perr == nil {
		_, perr = probe.WithInvalid(v)
	}
	if perr != nil {
		return 0, false, "GDAL_NODATA " + quoted + " is not a value " + d.Kind.String() + " samples can hold exactly; it was not applied."
	}
	return v, true, ""
}

// probePlane is a one-sample plane of a kind, to ask [imaging.Plane.WithInvalid]
// whether the kind holds a value.
func probePlane(kind imaging.SampleKindE) (imaging.Plane, error) {
	switch kind {
	case imaging.SampleKindU8:
		return imaging.NewPlaneU8(1, 1, []uint8{0})
	case imaging.SampleKindU16:
		return imaging.NewPlaneU16(1, 1, []uint16{0})
	case imaging.SampleKindI16:
		return imaging.NewPlaneI16(1, 1, []int16{0})
	case imaging.SampleKindI32:
		return imaging.NewPlaneI32(1, 1, []int32{0})
	case imaging.SampleKindF32:
		return imaging.NewPlaneF32(1, 1, []float32{0})
	}
	return imaging.NewPlaneF64(1, 1, []float64{0})
}

// gdalMetadata is what GDAL_METADATA (tag 42112) says per band; bands are
// GDAL's 0-based sample index.
type gdalMetadata struct {
	descriptions map[int]string
	scales       map[int]float64
	offsets      map[int]float64
	// unread counts items not applied: dataset metadata, other roles,
	// values that are not numbers, bands the file does not have.
	unread int
}

type gdalItem struct {
	Name   string `xml:"name,attr"`
	Sample string `xml:"sample,attr"`
	Role   string `xml:"role,attr"`
	Value  string `xml:",chardata"`
}

type gdalDocument struct {
	Items []gdalItem `xml:"Item"`
}

// maxGDALMetadata bounds the XML read from tag 42112.
const maxGDALMetadata = 1 << 20

func readGDALMetadata(f *file, e entry) (meta gdalMetadata, err error) {
	text, err := f.ascii(e, maxGDALMetadata)
	if err != nil {
		return
	}
	var doc gdalDocument
	if err = xml.Unmarshal([]byte(text), &doc); err != nil {
		return meta, malformedErr("GDAL_METADATA is not XML", err)
	}
	meta.descriptions = make(map[int]string, 4)
	meta.scales = make(map[int]float64, 4)
	meta.offsets = make(map[int]float64, 4)
	for _, it := range doc.Items {
		band, berr := strconv.Atoi(strings.TrimSpace(it.Sample))
		if it.Sample == "" || berr != nil || band < 0 {
			meta.unread++
			continue
		}
		role := strings.ToLower(it.Role)
		if role == "" {
			role = strings.ToLower(it.Name)
		}
		value := strings.TrimSpace(it.Value)
		switch role {
		case "description":
			meta.descriptions[band] = value
		case "scale", "offset":
			v, perr := strconv.ParseFloat(value, 64)
			if perr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				meta.unread++
				continue
			}
			if role == "scale" {
				meta.scales[band] = v
			} else {
				meta.offsets[band] = v
			}
		default:
			meta.unread++
		}
	}
	return meta, nil
}

// apply sets the band labels and value transforms the metadata states and
// returns notes for what it did not apply.
func (inst gdalMetadata) apply(labels []string, transforms []imaging.ValueTransform) (notes []string) {
	unread := inst.unread
	for band, text := range inst.descriptions {
		if band >= len(labels) {
			unread++
			continue
		}
		if text != "" {
			labels[band] = text
		}
	}
	for band := range len(transforms) {
		scale, hasScale := inst.scales[band]
		offset, hasOffset := inst.offsets[band]
		if !hasScale && !hasOffset {
			continue
		}
		t := imaging.IdentityTransform
		if hasScale {
			t.Scale = scale
		}
		t.Offset = offset
		if t.Scale == 0 {
			notes = append(notes, "GDAL_METADATA gives band "+strconv.Itoa(band+1)+" a scale of 0; its value transform was not applied.")
			continue
		}
		transforms[band] = t
	}
	for band := range inst.scales {
		if band >= len(transforms) {
			unread++
		}
	}
	for band := range inst.offsets {
		if band >= len(transforms) {
			unread++
		}
	}
	if unread > 0 {
		notes = append(notes, "GDAL_METADATA: "+strconv.Itoa(unread)+
			" items were not applied (dataset metadata, roles other than description, scale and offset, or bands the file does not have).")
	}
	return
}
