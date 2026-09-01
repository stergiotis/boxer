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

// projPt is a ring vertex in normalized projection space (see projectNorm).
type projPt struct{ X, Y float32 }

// Country is one admin-0 feature: identity fields as shipped upstream (the
// `_EH` ISO variants — empty when upstream has none, e.g. Northern Cyprus)
// plus the projected outline rings. Rings concatenate every ring of every
// member polygon; the rasterizer's even-odd rule makes outer/hole/member
// distinctions irrelevant (members are disjoint, holes alternate parity).
type Country struct {
	Admin string
	Name  string
	A2    string // ISO 3166-1 alpha-2 (upstream ISO_A2_EH); "" when absent
	A3    string // ISO 3166-1 alpha-3 (upstream ISO_A3_EH); "" when absent

	rings [][]projPt
	// ringHole marks, per ring, whether it is an interior ring (a hole) of its
	// member polygon rather than an outer boundary. The rasterizer ignores it
	// — even-odd needs no such distinction — but a painter overlay does: a
	// filled polygon has no hole support, so a hole must be outlined and never
	// filled. Exactly one ring in the vendored asset is a hole (South Africa's
	// Lesotho enclave).
	ringHole []bool
	// bbox in normalized projection space: minX, minY, maxX, maxY.
	bbox [4]float32
}

// Label is the human-facing form used in readouts: "Name (A3)" when a code
// exists, plain Name otherwise.
func (inst *Country) Label() string {
	if inst.A3 != "" {
		return inst.Name + " (" + inst.A3 + ")"
	}
	return inst.Name
}

// Atlas is the parsed country set plus the resolver's key table.
type Atlas struct {
	Countries []Country
	byKey     map[string]CountryIdx
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
			rings:    rings,
			ringHole: holes,
			bbox:     ringsBBox(rings),
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

// decodeRings flattens a Polygon or MultiPolygon into projected rings, plus
// the parallel outer/hole roles (see Country.ringHole).
func decodeRings(g neGeometry) ([][]projPt, []bool, error) {
	switch g.Type {
	case "Polygon":
		var poly [][][2]float64
		if err := json.Unmarshal(g.Coordinates, &poly); err != nil {
			return nil, nil, err
		}
		rings, holes := projectPoly(nil, nil, poly)
		return rings, holes, nil
	case "MultiPolygon":
		var mp [][][][2]float64
		if err := json.Unmarshal(g.Coordinates, &mp); err != nil {
			return nil, nil, err
		}
		var rings [][]projPt
		var holes []bool
		for _, poly := range mp {
			rings, holes = projectPoly(rings, holes, poly)
		}
		return rings, holes, nil
	default:
		return nil, nil, eb.Build().Str("type", g.Type).Errorf("unsupported geometry type")
	}
}

// projectPoly appends one GeoJSON polygon's rings. Ring 0 of a polygon is its
// outer boundary and the rest are holes — the only place that distinction is
// still visible, so it is recorded here rather than re-derived by winding.
func projectPoly(dstR [][]projPt, dstH []bool, poly [][][2]float64) ([][]projPt, []bool) {
	for i, ring := range poly {
		if len(ring) < 4 { // degenerate (GeoJSON rings repeat the first point)
			continue
		}
		pr := make([]projPt, len(ring))
		for j, ll := range ring {
			x, y := projectNorm(ll[0], ll[1])
			pr[j] = projPt{X: float32(x), Y: float32(y)}
		}
		dstR = append(dstR, pr)
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
