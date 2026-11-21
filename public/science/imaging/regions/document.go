package regions

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DocumentVersion is the version of the region document this package
// writes and the only one it reads (ADR-0399 §SD6).
const DocumentVersion = 1

// The region document is the interchange form of a region and of a set of
// them (ADR-0399 §SD6): everything a [Region] holds and nothing else. A
// region is
//
//	{"id": "…", "label": "…", "note": "…", "origin": "…",
//	 "frame": {"kind": "pixel" | "physical" | "reference-system" | "celestial",
//	           "code": "…", "units": ["…", "…"]},
//	 "anchor": [{"axis": "…", "position": 0}, …],
//	 "shape": {…}}
//
// where a frame carries "code" for a reference system or the sky and
// "units" for a physical frame, and a shape is one of
//
//	{"kind": "point", "at": [x, y]}
//	{"kind": "line", "from": [x, y], "to": [x, y]}
//	{"kind": "rectangle", "centre": [x, y], "size": [width, height], "angle": degrees}
//	{"kind": "ellipse", "centre": [x, y], "radii": [rx, ry], "angle": degrees}
//	{"kind": "polygon", "vertices": [[x, y], …]}
//
// One region is {"version": 1, "region": {…}}; a set is
// {"version": 1, "regions": [{…}, …]}. Reading is strict: an unknown or
// duplicate member, a member another shape kind has, a missing one, an
// unknown kind, a missing frame and another version are refused by name.
// Numbers are written as the shortest decimal that reads back to the same
// float64, so a region reads back as it was written.

type frameDoc struct {
	Kind  string   `json:"kind"`
	Code  *string  `json:"code,omitzero"`
	Units []string `json:"units,omitzero"`
}

type anchorDoc struct {
	Axis     string `json:"axis"`
	Position *int   `json:"position"`
}

type shapeDoc struct {
	Kind     string      `json:"kind"`
	At       []float64   `json:"at,omitzero"`
	From     []float64   `json:"from,omitzero"`
	To       []float64   `json:"to,omitzero"`
	Centre   []float64   `json:"centre,omitzero"`
	Size     []float64   `json:"size,omitzero"`
	Radii    []float64   `json:"radii,omitzero"`
	Angle    *float64    `json:"angle,omitzero"`
	Vertices [][]float64 `json:"vertices,omitzero"`
}

type regionDoc struct {
	Id     *string     `json:"id"`
	Label  string      `json:"label"`
	Note   string      `json:"note"`
	Origin string      `json:"origin"`
	Frame  *frameDoc   `json:"frame"`
	Anchor []anchorDoc `json:"anchor"`
	Shape  *shapeDoc   `json:"shape"`
}

type oneDoc struct {
	Version *int       `json:"version"`
	Region  *regionDoc `json:"region"`
}

type setDoc struct {
	Version *int        `json:"version"`
	Regions []regionDoc `json:"regions"`
}

// MarshalRegion is the document of one region, which must validate.
func MarshalRegion(r Region) (data []byte, err error) {
	d, err := toDoc(r)
	if err != nil {
		return nil, err
	}
	v := DocumentVersion
	return marshal(oneDoc{Version: &v, Region: &d})
}

// MarshalRegions is the document of a set of regions, each of which must
// validate, with ids unique among them.
func MarshalRegions(rs []Region) (data []byte, err error) {
	if err = checkUniqueIds(rs); err != nil {
		return nil, err
	}
	docs := make([]regionDoc, len(rs))
	for i, r := range rs {
		if docs[i], err = toDoc(r); err != nil {
			return nil, err
		}
	}
	v := DocumentVersion
	return marshal(setDoc{Version: &v, Regions: docs})
}

func marshal(v any) (data []byte, err error) {
	data, err = json.Marshal(v, jsontext.WithIndent("  "))
	if err != nil {
		return nil, eb.Build().Errorf("write region document: %w", err)
	}
	return data, nil
}

// UnmarshalRegion reads the document of one region.
func UnmarshalRegion(data []byte) (r Region, err error) {
	var d oneDoc
	if err = json.Unmarshal(data, &d, json.RejectUnknownMembers(true)); err != nil {
		return r, eb.Build().Errorf("read region document: %w", err)
	}
	if err = checkVersion(d.Version); err != nil {
		return
	}
	if d.Region == nil {
		return r, eb.Build().Str("member", "region").Errorf("region document has no region")
	}
	return fromDoc(*d.Region)
}

// UnmarshalRegions reads the document of a set of regions, refusing ids
// that are not unique among them.
func UnmarshalRegions(data []byte) (rs []Region, err error) {
	var d setDoc
	if err = json.Unmarshal(data, &d, json.RejectUnknownMembers(true)); err != nil {
		return nil, eb.Build().Errorf("read region document: %w", err)
	}
	if err = checkVersion(d.Version); err != nil {
		return nil, err
	}
	if d.Regions == nil {
		return nil, eb.Build().Str("member", "regions").Errorf("region set document has no regions")
	}
	rs = make([]Region, len(d.Regions))
	for i, rd := range d.Regions {
		if rs[i], err = fromDoc(rd); err != nil {
			return nil, eb.Build().Int("index", i).Errorf("region of the set: %w", err)
		}
	}
	if err = checkUniqueIds(rs); err != nil {
		return nil, err
	}
	return rs, nil
}

func checkVersion(v *int) error {
	if v == nil {
		return eb.Build().Str("member", "version").Errorf("region document has no version")
	}
	if *v != DocumentVersion {
		return eb.Build().Int("version", *v).Int("supported", DocumentVersion).Errorf("region document version is not supported")
	}
	return nil
}

func checkUniqueIds(rs []Region) error {
	seen := make(map[string]struct{}, len(rs))
	for _, r := range rs {
		if _, dup := seen[r.Id]; dup {
			return eb.Build().Str("id", r.Id).Errorf("region id is not unique in the set")
		}
		seen[r.Id] = struct{}{}
	}
	return nil
}

func pair(p Point) []float64 { return []float64{p.X, p.Y} }

func toDoc(r Region) (d regionDoc, err error) {
	if err = r.Validate(); err != nil {
		return
	}
	id := r.Id
	d = regionDoc{Id: &id, Label: r.Label, Note: r.Note, Origin: r.Origin,
		Frame: &frameDoc{Kind: r.Frame.Kind.String()}, Anchor: make([]anchorDoc, 0, len(r.Anchor.positions))}
	switch r.Frame.Kind {
	case FrameKindPhysical:
		d.Frame.Units = []string{r.Frame.Units[0], r.Frame.Units[1]}
	case FrameKindReferenceSystem, FrameKindCelestial:
		code := r.Frame.Code
		d.Frame.Code = &code
	}
	for _, p := range r.Anchor.positions {
		pos := p.Position
		d.Anchor = append(d.Anchor, anchorDoc{Axis: p.Axis, Position: &pos})
	}
	s := r.Shape
	sd := &shapeDoc{Kind: s.kind.String()}
	switch s.kind {
	case ShapeKindPoint:
		sd.At = pair(s.a)
	case ShapeKindLine:
		sd.From, sd.To = pair(s.a), pair(s.b)
	case ShapeKindRectangle, ShapeKindEllipse:
		angle := s.angle
		sd.Centre, sd.Angle = pair(s.a), &angle
		if s.kind == ShapeKindRectangle {
			sd.Size = pair(s.b)
		} else {
			sd.Radii = pair(s.b)
		}
	case ShapeKindPolygon:
		sd.Vertices = make([][]float64, len(s.vertices))
		for i, v := range s.vertices {
			sd.Vertices[i] = pair(v)
		}
	}
	d.Shape = sd
	return d, nil
}

func fromDoc(d regionDoc) (r Region, err error) {
	if d.Id == nil {
		return r, eb.Build().Str("member", "id").Errorf("region has no id")
	}
	r.Id, r.Label, r.Note, r.Origin = *d.Id, d.Label, d.Note, d.Origin
	if d.Frame == nil {
		return r, eb.Build().Str("region", r.Id).Str("member", "frame").Errorf("region has no frame")
	}
	if r.Frame, err = frameFromDoc(*d.Frame); err != nil {
		return r, eb.Build().Str("region", r.Id).Errorf("region frame: %w", err)
	}
	positions := make([]AxisPosition, 0, len(d.Anchor))
	for i, a := range d.Anchor {
		if a.Position == nil {
			return r, eb.Build().Str("region", r.Id).Int("entry", i).Str("member", "position").Errorf("anchor entry has no position")
		}
		positions = append(positions, AxisPosition{Axis: a.Axis, Position: *a.Position})
	}
	if r.Anchor, err = NewAnchor(positions...); err != nil {
		return r, eb.Build().Str("region", r.Id).Errorf("region anchor: %w", err)
	}
	if d.Shape == nil {
		return r, eb.Build().Str("region", r.Id).Str("member", "shape").Errorf("region has no shape")
	}
	if r.Shape, err = shapeFromDoc(*d.Shape); err != nil {
		return r, eb.Build().Str("region", r.Id).Errorf("region shape: %w", err)
	}
	if err = r.Validate(); err != nil {
		return Region{}, err
	}
	return r, nil
}

func frameFromDoc(d frameDoc) (f Frame, err error) {
	for _, k := range AllFrameKinds {
		if k.String() == d.Kind {
			f.Kind = k
		}
	}
	if f.Kind == 0 {
		return f, eb.Build().Str("kind", d.Kind).Errorf("unknown frame kind")
	}
	if d.Code != nil {
		if f.Kind != FrameKindReferenceSystem && f.Kind != FrameKindCelestial {
			return f, eb.Build().Stringer("kind", f.Kind).Str("member", "code").Errorf("frame has a member its kind does not have")
		}
		f.Code = *d.Code
	}
	if d.Units != nil {
		if f.Kind != FrameKindPhysical {
			return f, eb.Build().Stringer("kind", f.Kind).Str("member", "units").Errorf("frame has a member its kind does not have")
		}
		if len(d.Units) != 2 {
			return f, eb.Build().Int("units", len(d.Units)).Errorf("a frame's units are two strings")
		}
		f.Units = [2]string{d.Units[0], d.Units[1]}
	}
	return f, f.Validate()
}

func pointFromDoc(name string, v []float64) (p Point, err error) {
	if len(v) != 2 {
		return p, eb.Build().Str("member", name).Int("numbers", len(v)).Errorf("shape member is not a pair of numbers")
	}
	return Point{X: v[0], Y: v[1]}, nil
}

func shapeFromDoc(d shapeDoc) (s Shape, err error) {
	var kind ShapeKindE
	for _, k := range AllShapeKinds {
		if k.String() == d.Kind {
			kind = k
		}
	}
	if kind == 0 {
		return s, eb.Build().Str("kind", d.Kind).Errorf("unknown shape kind")
	}
	present := map[string]bool{
		"at": d.At != nil, "from": d.From != nil, "to": d.To != nil, "centre": d.Centre != nil,
		"size": d.Size != nil, "radii": d.Radii != nil, "angle": d.Angle != nil, "vertices": d.Vertices != nil,
	}
	want := map[ShapeKindE][]string{
		ShapeKindPoint:     {"at"},
		ShapeKindLine:      {"from", "to"},
		ShapeKindRectangle: {"centre", "size", "angle"},
		ShapeKindEllipse:   {"centre", "radii", "angle"},
		ShapeKindPolygon:   {"vertices"},
	}[kind]
	for _, name := range []string{"at", "from", "to", "centre", "size", "radii", "angle", "vertices"} {
		wanted := false
		for _, w := range want {
			wanted = wanted || w == name
		}
		switch {
		case wanted && !present[name]:
			return s, eb.Build().Stringer("kind", kind).Str("member", name).Errorf("shape lacks a member its kind has")
		case !wanted && present[name]:
			return s, eb.Build().Stringer("kind", kind).Str("member", name).Errorf("shape has a member its kind does not have")
		}
	}
	switch kind {
	case ShapeKindPoint:
		var at Point
		if at, err = pointFromDoc("at", d.At); err == nil {
			s, err = NewPoint(at)
		}
	case ShapeKindLine:
		var from, to Point
		if from, err = pointFromDoc("from", d.From); err != nil {
			return
		}
		if to, err = pointFromDoc("to", d.To); err != nil {
			return
		}
		s, err = NewLine(from, to)
	case ShapeKindRectangle, ShapeKindEllipse:
		var centre, size Point
		if centre, err = pointFromDoc("centre", d.Centre); err != nil {
			return
		}
		if kind == ShapeKindRectangle {
			if size, err = pointFromDoc("size", d.Size); err != nil {
				return
			}
			s, err = NewRectangle(centre, size.X, size.Y, *d.Angle)
		} else {
			if size, err = pointFromDoc("radii", d.Radii); err != nil {
				return
			}
			s, err = NewEllipse(centre, size.X, size.Y, *d.Angle)
		}
	case ShapeKindPolygon:
		vs := make([]Point, len(d.Vertices))
		for i, v := range d.Vertices {
			if vs[i], err = pointFromDoc("vertices", v); err != nil {
				return
			}
		}
		s, err = NewPolygon(vs)
	}
	return s, err
}
