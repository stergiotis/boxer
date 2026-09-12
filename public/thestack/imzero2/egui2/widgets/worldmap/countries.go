package worldmap

import (
	"bytes"
	_ "embed"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Country geometry + identity, parsed once from the vendored Natural Earth
// 110m admin-0 asset (see assets/README.md for provenance; ADR-0114 §SD1).

//go:embed assets/ne_110m_admin_0_countries.geojson.zst
var neCountriesZst []byte

// CountryIdx indexes Atlas.Countries. NoCountry marks "no country" (sea in
// the raster index buffer, resolver miss).
type CountryIdx int32

const NoCountry CountryIdx = -1

// geoPt is a ring vertex as the asset carries it: degrees, unprojected. The
// atlas keeps the source coordinates because the projection is switchable —
// projected geometry is derived per projection (see Atlas.Projected).
type geoPt struct{ Lon, Lat float64 }

// projPt is a ring vertex in normalized projection space (see projectNorm).
type projPt struct{ X, Y float32 }

// Country is one admin-0 feature: identity fields as shipped upstream (the
// `_EH` ISO variants — empty when upstream has none, e.g. Northern Cyprus)
// plus the unprojected outline rings. Rings concatenate every ring of every
// member polygon; the rasterizer's even-odd rule makes outer/hole/member
// distinctions irrelevant (members are disjoint, holes alternate parity).
type Country struct {
	Admin string
	Name  string
	A2    string // ISO 3166-1 alpha-2 (upstream ISO_A2_EH); "" when absent
	A3    string // ISO 3166-1 alpha-3 (upstream ISO_A3_EH); "" when absent

	geo [][]geoPt
	// ringHole marks, per ring, whether it is an interior ring (a hole) of its
	// member polygon rather than an outer boundary. The rasterizer ignores it
	// — even-odd needs no such distinction — but a painter overlay does: a
	// filled polygon has no hole support, so a hole must be outlined and never
	// filled. Exactly one ring in the vendored asset is a hole (South Africa's
	// Lesotho enclave). Projection preserves ring count and order, so this
	// indexes Projected.Rings in parallel.
	ringHole []bool
}

// Label is the human-facing form used in readouts: "Name (A3)" when a code
// exists, plain Name otherwise.
func (inst *Country) Label() string {
	if inst.A3 != "" {
		return inst.Name + " (" + inst.A3 + ")"
	}
	return inst.Name
}

// RingCount is how many outline rings the country has: every ring of every
// member polygon, in the atlas's order.
func (inst *Country) RingCount() int { return len(inst.geo) }

// Ring appends ring i's vertices to lats and lngs in degrees and returns the
// grown slices, so a caller drawing every frame reuses its buffers instead of
// allocating per ring. hole reports an interior ring — a filled overlay must
// not fill one (exactly one ring in the vendored asset is a hole: South
// Africa's Lesotho enclave). An index outside the country returns the inputs
// untouched.
//
// These are the source coordinates the atlas keeps because its projection is
// switchable, which is what makes them useful to something projecting for
// itself — a slippy map drawing an offline basemap, say.
func (inst *Country) Ring(i int, lats, lngs []float64) (outLats, outLngs []float64, hole bool) {
	if i < 0 || i >= len(inst.geo) {
		return lats, lngs, false
	}
	for _, p := range inst.geo[i] {
		lats = append(lats, p.Lat)
		lngs = append(lngs, p.Lon)
	}
	return lats, lngs, inst.ringHole[i]
}

// GeoBounds is the country's extent in degrees, for a caller culling against
// a viewport before it projects anything. ok is false for a country with no
// geometry. The box is the naive one: a country crossing the antimeridian
// (Russia, Fiji) spans nearly the whole longitude range rather than wrapping,
// which over-includes and never under-includes.
func (inst *Country) GeoBounds() (minLat, minLng, maxLat, maxLng float64, ok bool) {
	for _, ring := range inst.geo {
		for _, p := range ring {
			if !ok {
				minLat, maxLat, minLng, maxLng, ok = p.Lat, p.Lat, p.Lon, p.Lon, true
				continue
			}
			minLat, maxLat = min(minLat, p.Lat), max(maxLat, p.Lat)
			minLng, maxLng = min(minLng, p.Lon), max(maxLng, p.Lon)
		}
	}
	return
}

// Atlas is the parsed country set plus the resolver's key table. Geometry is
// held unprojected; Projected derives (and caches) the outlines under one
// projection.
type Atlas struct {
	Countries []Country
	byKey     map[string]CountryIdx
	// projected memoizes one Projected per projection. Every widget shares the
	// process-wide atlas, so the builders must be safe to call concurrently.
	projected [projectionCount]func() *Projected
}

// projCountry is one country's outline under one projection.
type projCountry struct {
	rings [][]projPt
	// bbox in normalized projection space: minX, minY, maxX, maxY.
	bbox [4]float32
}

// Projected is the atlas geometry under one projection: the same countries in
// the same order, so a CountryIdx indexes Atlas.Countries and this alike.
type Projected struct {
	Projection Projection
	countries  []projCountry
}

// Projected returns the atlas outlines under p, projecting on first use and
// caching per projection (~85 KB each for the vendored asset). An unknown
// projection falls back to the default rather than failing.
func (inst *Atlas) Projected(p Projection) *Projected {
	if !p.Valid() {
		p = ProjectionNaturalEarth
	}
	return inst.projected[p]()
}

// Rings returns one country's outline rings in normalized projection space,
// in the atlas's ring order (parallel to Country.ringHole). A CountryIdx
// outside the atlas — NoCountry included — yields no rings.
func (inst *Projected) Rings(idx CountryIdx) [][]projPt {
	if idx < 0 || int(idx) >= len(inst.countries) {
		return nil
	}
	return inst.countries[idx].rings
}

// project builds the projected geometry for one projection.
func (inst *Atlas) project(p Projection) *Projected {
	out := &Projected{Projection: p, countries: make([]projCountry, len(inst.Countries))}
	for ci := range inst.Countries {
		rings := make([][]projPt, len(inst.Countries[ci].geo))
		for ri, gr := range inst.Countries[ci].geo {
			pr := make([]projPt, len(gr))
			for j, g := range gr {
				x, y := projectNorm(p, g.Lon, g.Lat)
				pr[j] = projPt{X: float32(x), Y: float32(y)}
			}
			rings[ri] = pr
		}
		out.countries[ci] = projCountry{rings: rings, bbox: ringsBBox(rings)}
	}
	return out
}

// aliases maps additional uppercase spellings to the upstream alpha-3 code.
// Deliberately small: ADMIN + NAME already cover both long and short forms
// (e.g. "Democratic Republic of the Congo" and "Dem. Rep. Congo"); this table
// only adds common external forms neither field carries. Fuzzy matching is a
// deferred non-goal (ADR-0114 §SD7).
var aliases = map[string]string{
	"UNITED STATES":     "USA",
	"UK":                "GBR",
	"GREAT BRITAIN":     "GBR",
	"CZECH REPUBLIC":    "CZE",
	"REPUBLIC OF KOREA": "KOR",
	"KOREA":             "KOR",
	"SWAZILAND":         "SWZ",
	"MACEDONIA":         "MKD",
	"BURMA":             "MMR",
	"DRC":               "COD",
	"CAPE VERDE":        "CPV", // absent at 110m scale; kept for a clean miss
}

// geojson decode targets — only the consumed subset (assets/README.md).
type neFeatureCollection struct {
	Features []neFeature `json:"features"`
}
type neFeature struct {
	Properties neProps    `json:"properties"`
	Geometry   neGeometry `json:"geometry"`
}
type neProps struct {
	Admin  string `json:"ADMIN"`
	Name   string `json:"NAME"`
	IsoA2E string `json:"ISO_A2_EH"`
	IsoA3E string `json:"ISO_A3_EH"`
}
type neGeometry struct {
	Type        string         `json:"type"`
	Coordinates jsontext.Value `json:"coordinates"`
}

var loadAtlasOnce = sync.OnceValues(loadAtlas)

// LoadAtlas parses the embedded asset once (process-wide) and returns the
// shared Atlas. Concurrency-safe; every caller sees the same instance.
func LoadAtlas() (*Atlas, error) { return loadAtlasOnce() }

func loadAtlas() (*Atlas, error) {
	// One-shot decode of a small embedded asset: a single-goroutine zstd
	// decoder streamed straight into the json/v2 reader — no intermediate
	// uncompressed buffer.
	zr, err := zstd.NewReader(bytes.NewReader(neCountriesZst), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, eh.Errorf("asset zstd: %w", err)
	}
	defer zr.Close()
	var fc neFeatureCollection
	if err = json.UnmarshalRead(zr, &fc); err != nil {
		return nil, eh.Errorf("asset parse: %w", err)
	}
	a := &Atlas{
		Countries: make([]Country, 0, len(fc.Features)),
		byKey:     make(map[string]CountryIdx, len(fc.Features)*4),
	}
	for _, f := range fc.Features {
		rings, holes, rerr := decodeRings(f.Geometry)
		if rerr != nil {
			return nil, eb.Build().Str("admin", f.Properties.Admin).Errorf("unable to decode country rings: %w", rerr)
		}
		if len(rings) == 0 {
			continue
		}
		ct := Country{
			Admin:    f.Properties.Admin,
			Name:     f.Properties.Name,
			A2:       cleanIso(f.Properties.IsoA2E),
			A3:       cleanIso(f.Properties.IsoA3E),
			geo:      rings,
			ringHole: holes,
		}
		idx := CountryIdx(len(a.Countries))
		a.Countries = append(a.Countries, ct)
		a.addKey(ct.A2, idx)
		a.addKey(ct.A3, idx)
		a.addKey(ct.Admin, idx)
		a.addKey(ct.Name, idx)
	}
	for alias, a3 := range aliases {
		if idx, ok := a.byKey[a3]; ok {
			a.addKey(alias, idx)
		}
	}
	for i := range a.projected {
		p := Projection(i)
		a.projected[i] = sync.OnceValue(func() *Projected { return a.project(p) })
	}
	return a, nil
}

// cleanIso maps the upstream "-99" no-code sentinel (and empties) to "".
func cleanIso(s string) string {
	if s == "" || s == "-99" {
		return ""
	}
	return s
}

// addKey registers one uppercase-normalized resolver key. First writer wins:
// upstream identity fields are inserted in feature order before aliases, and
// no colliding pair is known in the vendored asset (guarded by a test).
func (inst *Atlas) addKey(key string, idx CountryIdx) {
	key = normalizeKey(key)
	if key == "" {
		return
	}
	if _, exists := inst.byKey[key]; !exists {
		inst.byKey[key] = idx
	}
}

func normalizeKey(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// Resolve maps a free-form value — ISO alpha-2/alpha-3 code, upstream ADMIN /
// NAME spelling, or an alias — to a country (ADR-0114 §SD4). Exact matches
// only, case-insensitive, surrounding whitespace ignored.
func (inst *Atlas) Resolve(s string) (idx CountryIdx, ok bool) {
	idx, ok = inst.byKey[normalizeKey(s)]
	if !ok {
		idx = NoCountry
	}
	return
}

// decodeRings flattens a Polygon or MultiPolygon into lon/lat rings, plus the
// parallel outer/hole roles (see Country.ringHole).
func decodeRings(g neGeometry) ([][]geoPt, []bool, error) {
	switch g.Type {
	case "Polygon":
		var poly [][][2]float64
		if err := json.Unmarshal(g.Coordinates, &poly); err != nil {
			return nil, nil, err
		}
		rings, holes := appendPoly(nil, nil, poly)
		return rings, holes, nil
	case "MultiPolygon":
		var mp [][][][2]float64
		if err := json.Unmarshal(g.Coordinates, &mp); err != nil {
			return nil, nil, err
		}
		var rings [][]geoPt
		var holes []bool
		for _, poly := range mp {
			rings, holes = appendPoly(rings, holes, poly)
		}
		return rings, holes, nil
	default:
		return nil, nil, eb.Build().Str("type", g.Type).Errorf("unsupported geometry type")
	}
}

// appendPoly appends one GeoJSON polygon's rings. Ring 0 of a polygon is its
// outer boundary and the rest are holes — the only place that distinction is
// still visible, so it is recorded here rather than re-derived by winding.
func appendPoly(dstR [][]geoPt, dstH []bool, poly [][][2]float64) ([][]geoPt, []bool) {
	for i, ring := range poly {
		if len(ring) < 4 { // degenerate (GeoJSON rings repeat the first point)
			continue
		}
		gr := make([]geoPt, len(ring))
		for j, ll := range ring {
			gr[j] = geoPt{Lon: ll[0], Lat: ll[1]}
		}
		dstR = append(dstR, gr)
		dstH = append(dstH, i > 0)
	}
	return dstR, dstH
}

func ringsBBox(rings [][]projPt) (bb [4]float32) {
	bb = [4]float32{1, 1, 0, 0}
	for _, r := range rings {
		for _, p := range r {
			if p.X < bb[0] {
				bb[0] = p.X
			}
			if p.Y < bb[1] {
				bb[1] = p.Y
			}
			if p.X > bb[2] {
				bb[2] = p.X
			}
			if p.Y > bb[3] {
				bb[3] = p.Y
			}
		}
	}
	return
}
