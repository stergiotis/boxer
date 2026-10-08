package play

import (
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The Map pane as an agent reads and moves it (ADR-0270, update of
// 2026-10-05). The read is the pane's settings, its raster and its errors,
// the camera, the selected area and the window the Timeline applies. Two
// commands: set_map_view moves the camera (view effect), and
// set_map_options sets the raster's source, render and colour expression,
// sampling, cache and readout, and the selected area. The source and the
// colour expression are SQL fragments the raster template splices in; the
// raster runs on the pane's lane, which runs under the task's mark after an
// agent's command (§SD2). The live switch, opacity, basemap and device
// resolution stay the person's.

const (
	opGetMap         = "get_map"
	opSetMapView     = "set_map_view"
	opSetMapOptions  = "set_map_options"
	mapPaneId        = "map"
	opsResMap        = mapPaneId
	mapReadSQLBytes  = 2 << 10
	mapReadNoteBytes = 512
)

// MapBox is a lat/lon box.
type MapBox struct {
	South float64 `desc:"the southern edge, degrees"`
	West  float64 `desc:"the western edge, degrees"`
	North float64 `desc:"the northern edge, degrees"`
	East  float64 `desc:"the eastern edge, degrees; may exceed 180 across the antimeridian"`
}

// MapPoint is a lat/lon point.
type MapPoint struct {
	Lat float64 `desc:"latitude, degrees"`
	Lon float64 `desc:"longitude, degrees"`
}

// MapCameraReading is where the map looks.
type MapCameraReading struct {
	Center MapPoint `desc:"the view's centre"`
	Zoom   float64  `desc:"the zoom level"`
	View   MapBox   `desc:"the box the view shows"`
}

// MapRenderReading is one render the pane offers.
type MapRenderReading struct {
	Name  string   `desc:"the render's name, as set_map_options takes it"`
	Needs []string `json:",omitzero" desc:"columns it reads beyond mercator_x and mercator_y"`
}

// MapReading is get_map's result.
type MapReading struct {
	Drawn        PaneDraw           `desc:"which draw this is of and its status line"`
	Table        string             `desc:"the raster's table source: a table, or a table function, as typed"`
	TimeColumn   string             `json:",omitzero" desc:"the source's time column, filtered by the Timeline's brushed window"`
	Render       string             `desc:"the render drawn"`
	Renders      []MapRenderReading `desc:"the renders set_map_options takes"`
	ColorSql     string             `json:",omitzero" desc:"the Custom render's colour expression"`
	Sampling     float64            `desc:"the manual sampling factor, used while refine is off"`
	Refine       bool               `json:",omitzero" desc:"the sampling ladder climbs from coarse to fine levels"`
	Level        string             `json:",omitzero" desc:"the ladder's level on screen"`
	Cache        bool               `json:",omitzero" desc:"the raster runs use the server's query cache"`
	Readout      bool               `json:",omitzero" desc:"the hover readout's columns are fetched"`
	ReadoutLabel string             `json:",omitzero" desc:"what the readout shows beside a pixel's count"`
	Live         bool               `json:",omitzero" desc:"the person's switch: the raster refetches whenever the camera rests; off, only on Refresh or a command"`
	RasterWidth  uint32             `json:",omitzero" desc:"the raster on screen's width in pixels"`
	RasterHeight uint32             `json:",omitzero" desc:"its height"`
	RasterBox    *MapBox            `json:",omitzero" desc:"the box the raster on screen covers"`
	FromMemory   bool               `json:",omitzero" desc:"the raster on screen came from memory rather than a fetch"`
	Loading      bool               `json:",omitzero" desc:"a raster fetch is in flight"`
	Cancelled    bool               `json:",omitzero" desc:"the last fetch was cancelled"`
	Error        string             `json:",omitzero" desc:"the configuration, query or raster error the status line gives"`
	Camera       *MapCameraReading  `json:",omitzero" desc:"the camera, once the map has laid out"`
	Area         *MapBox            `json:",omitzero" desc:"the selected area, published as the area_* signals"`
	Window       string             `json:",omitzero" desc:"the time window the raster is filtered to"`
}

// SetMapViewArgs is set_map_view's argument.
type SetMapViewArgs struct {
	Center *MapPoint `json:",omitzero" desc:"the point to centre on"`
	Zoom   *float64  `json:",omitzero" desc:"the zoom level, with or without a centre"`
	Bounds *MapBox   `json:",omitzero" desc:"a box to frame, instead of a centre and zoom"`
}

// SetMapOptionsArgs is set_map_options' argument.
type SetMapOptionsArgs struct {
	Table      *string  `json:",omitzero" desc:"the table source: a table with mercator_x and mercator_y, or a table function; no ';' or comments; refused for an agent"`
	TimeColumn *string  `json:",omitzero" desc:"the source's time column, a plain name, filtered by the Timeline's window; empty for none"`
	Render     *string  `json:",omitzero" desc:"one of the renders get_map lists"`
	ColorSql   *string  `json:",omitzero" desc:"the Custom render's colour expression: defines red, green, blue (0 to 255) and optionally alpha, over total, max_total, transparency and aggregates of the source's columns; no ';'; refused for an agent"`
	Sampling   *float64 `json:",omitzero" desc:"the manual sampling factor, 1 to 100"`
	Refine     *bool    `json:",omitzero" desc:"climb the sampling ladder from coarse to fine"`
	Cache      *bool    `json:",omitzero" desc:"use the server's query cache for the raster runs"`
	Readout    *bool    `json:",omitzero" desc:"fetch the hover readout's columns"`
	Area       *MapBox  `json:",omitzero" desc:"select this area: written as the area_* signals"`
	ClearArea  bool     `json:",omitzero" desc:"clear the selected area: the area_* signals go back to the whole world"`
}

// mapOpsView is what get_map reads, copied on the render goroutine.
type mapOpsView struct {
	r MapReading
}

func mapBoxOf(b portolan.LatLngBounds) MapBox {
	return MapBox{South: b.GetSouth(), West: b.GetWest(), North: b.GetNorth(), East: b.GetEast()}
}

// mapView copies the Map's settings and state.
func (inst *PlayApp) mapView() mapOpsView {
	d := inst.mapDriver
	r := MapReading{Table: truncateBytes(d.table, mapReadSQLBytes), TimeColumn: d.timeCol,
		Render: builtinRenders[d.renderIdx].name, ColorSql: truncateBytes(d.customColorSQL, mapReadSQLBytes),
		Sampling: d.sampling, Refine: d.refine, Cache: d.cache, Readout: d.readoutOn, Live: d.live,
		RasterWidth: d.packW, RasterHeight: d.packH, FromMemory: d.memoOnScreen, Loading: d.loading, Cancelled: d.cancelled,
		Window: d.windowStatus()}
	for _, rr := range builtinRenders {
		r.Renders = append(r.Renders, MapRenderReading{Name: rr.name, Needs: rr.needs})
	}
	if d.readoutOn {
		r.ReadoutLabel = d.readoutLabel
	}
	if d.packW > 0 {
		r.Level = d.ladder.status(d.packLevel, d.loading)
		r.RasterBox = &MapBox{South: d.packBounds[0], West: d.packBounds[1], North: d.packBounds[2], East: d.packBounds[3]}
	}
	switch {
	case d.controlErr != "":
		r.Error = "config: " + d.controlErr
	case d.laneErr != nil:
		r.Error = truncateBytes("query error: "+d.laneErr.Error(), mapReadNoteBytes)
	case d.packErr != nil:
		r.Error = truncateBytes("raster error: "+d.packErr.Error(), mapReadNoteBytes)
	}
	if d.pm != nil {
		if v := d.pm.View(); v.Loaded() {
			c := v.Center()
			r.Camera = &MapCameraReading{Center: MapPoint{Lat: c.Lat, Lon: c.Lng}, Zoom: v.Zoom(), View: mapBoxOf(v.Bounds())}
		}
	}
	if d.hasArea {
		_, south, west, north, east := areaSignals(d.area)
		r.Area = &MapBox{South: south, West: west, North: north, East: east}
	}
	return mapOpsView{r: r}
}

// mapReading is get_map.
func mapReading(sn *opsSnap, _ appops.None) (out MapReading, err error) {
	d, _, err := paneDrawOf(sn, mapPaneId)
	if err != nil {
		return
	}
	out = sn.paneViews.mapv.r
	out.Drawn = d
	return
}

// checkMapBox refuses a box the map cannot frame or select.
func checkMapBox(what string, b MapBox) error {
	for _, v := range []float64{b.South, b.West, b.North, b.East} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return app.RefuseOperation(what + " is four finite numbers")
		}
	}
	if !(b.South < b.North) || b.South < -90 || b.North > 90 || !(b.West < b.East) || b.East-b.West > 360 {
		return app.RefuseOperation(what + " is south < north within ±90, and west < east no more than 360 apart")
	}
	return nil
}

// setMapView is set_map_view. A move asks for one fetch of the new view,
// whether or not the person's live switch is on, and the viewport signals
// that fetch writes carry writer.
func (inst *MapDriver) setMapView(in SetMapViewArgs, writer string, follow bool) (err error) {
	if in.Center == nil && in.Zoom == nil && in.Bounds == nil {
		return app.RefuseOperation("name where to look: center and zoom, or bounds")
	}
	if in.Bounds != nil && (in.Center != nil || in.Zoom != nil) {
		return app.RefuseOperation("bounds frames a box; give it, or a center and zoom, not both")
	}
	if inst.pm == nil || !inst.pm.View().Loaded() {
		return app.RefuseOperation("the map has not laid out yet: show_pane map, and call again once it has drawn")
	}
	v := inst.pm.View()
	if in.Bounds != nil {
		if err = checkMapBox("bounds", *in.Bounds); err != nil {
			return
		}
	}
	if in.Center != nil {
		c := *in.Center
		if math.IsNaN(c.Lat) || math.IsNaN(c.Lon) || c.Lat < -90 || c.Lat > 90 || math.IsInf(c.Lon, 0) {
			return app.RefuseOperation("center is a latitude within ±90 and a finite longitude")
		}
	}
	if in.Zoom != nil {
		if z := *in.Zoom; math.IsNaN(z) || z < v.MinZoom() || z > v.MaxZoom() {
			return app.RefuseOperation("zoom is " + strconv.FormatFloat(v.MinZoom(), 'g', -1, 64) + " to " +
				strconv.FormatFloat(v.MaxZoom(), 'g', -1, 64))
		}
	}
	switch {
	case in.Bounds != nil:
		b := *in.Bounds
		_ = v.FitBounds(portolan.LatLngBoundsOf(portolan.LL(b.South, b.West), portolan.LL(b.North, b.East)), portolan.FitOptions{})
	default:
		center, zoom := v.Center(), v.Zoom()
		if in.Center != nil {
			center = portolan.LL(in.Center.Lat, in.Center.Lon)
		}
		if in.Zoom != nil {
			zoom = *in.Zoom
		}
		v.SetView(center, zoom)
	}
	inst.forceRefresh = true
	if follow {
		inst.followWriter = writer
	}
	return
}

// mapRenderIndex is the render named, -1 for none.
func mapRenderIndex(name string) int {
	for i, r := range builtinRenders {
		if strings.EqualFold(r.name, strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

// setMapOptions is set_map_options: everything named is checked before
// anything is applied. The area is written at once with writer; the raster
// settings take effect at the fetch they ask for, whose viewport signals
// carry writer when follow is set.
func (inst *PlayApp) setMapOptions(in SetMapOptionsArgs, writer string, follow bool) (err error) {
	d := inst.mapDriver
	raster := in.Table != nil || in.TimeColumn != nil || in.Render != nil || in.ColorSql != nil || in.Sampling != nil ||
		in.Refine != nil || in.Cache != nil || in.Readout != nil
	if !raster && in.Area == nil && !in.ClearArea {
		return noOptionsRefusal(mapPaneId, "table", "time_column", "render", "color_sql", "sampling", "refine", "cache", "readout", "area", "clear_area")
	}
	if in.Area != nil && in.ClearArea {
		return app.RefuseOperation("give an area or clear_area, not both")
	}
	if in.Table != nil && sanitizeTable(*in.Table) == "" {
		return app.RefuseOperation("table is a table name or a table function, with no ';' and no comments")
	}
	if in.TimeColumn != nil {
		if t := strings.TrimSpace(*in.TimeColumn); t != "" && !mapTimeColRe.MatchString(t) {
			return app.RefuseOperation("time_column is a plain column name, or empty for none")
		}
	}
	render := -1
	if in.Render != nil {
		if render = mapRenderIndex(*in.Render); render < 0 {
			names := make([]string, 0, len(builtinRenders))
			for _, r := range builtinRenders {
				names = append(names, r.name)
			}
			return app.RefuseOperation("render is one of: " + strings.Join(names, ", "))
		}
	}
	if in.ColorSql != nil {
		if s := *in.ColorSql; strings.TrimSpace(s) == "" || strings.Contains(s, ";") {
			return app.RefuseOperation("color_sql is a non-empty expression block with no ';'")
		}
	}
	if in.Sampling != nil && !(*in.Sampling >= 1 && *in.Sampling <= 100) {
		return app.RefuseOperation("sampling is 1 to 100")
	}
	if in.Area != nil {
		if err = checkMapBox("area", *in.Area); err != nil {
			return
		}
	}

	if in.Table != nil {
		d.table = *in.Table
	}
	if in.TimeColumn != nil {
		d.timeCol = strings.TrimSpace(*in.TimeColumn)
	}
	if in.ColorSql != nil {
		d.customColorSQL = *in.ColorSql
	}
	if in.Sampling != nil {
		d.sampling = *in.Sampling
	}
	if in.Refine != nil {
		d.refine = *in.Refine
	}
	if in.Cache != nil {
		d.cache = *in.Cache
	}
	if in.Readout != nil {
		d.readoutOn = *in.Readout
	}
	emit := inst.sigEmit.as(writer)
	switch {
	case in.Area != nil:
		a := *in.Area
		d.setArea(mapArea{south: a.South, west: a.West, north: a.North, east: a.East}, true, emit)
	case in.ClearArea:
		d.setArea(mapArea{}, false, emit)
	}
	if render >= 0 && render != d.renderIdx {
		d.renderIdx = render
		d.requestRefresh()
	} else if raster {
		d.forceRefresh = true
	}
	if raster && follow {
		d.followWriter = writer
	}
	return
}

// requestOptions is the render combo, Clear area or a box drawn on the map:
// through set_map_options as the person's gesture where play's launcher
// routes it, directly otherwise.
func (inst *MapDriver) requestOptions(in SetMapOptionsArgs, emit SignalEmitterI) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	switch {
	case in.Area != nil:
		a := *in.Area
		inst.setArea(mapArea{south: a.South, west: a.West, north: a.North, east: a.East}, true, emit)
	case in.ClearArea:
		inst.setArea(mapArea{}, false, emit)
	}
	if in.Render != nil {
		if i := mapRenderIndex(*in.Render); i >= 0 {
			inst.renderIdx = i
			inst.requestRefresh()
		}
	}
}

// emitWriter is the writer of the Map's next signal writes: the task whose
// command asked for the fetch, until it has published, else the pane's own.
func (inst *MapDriver) emitWriter() string {
	if inst.followWriter != "" {
		return inst.followWriter
	}
	return signalWriterMap
}

// mapOptionsDigest is the map resource: the raster's settings, the selected
// area and the viewport last published. The camera between rests is left
// out, so a pan in progress invalidates nothing.
func mapOptionsDigest(p *PlayApp) string {
	d := p.mapDriver
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(d.table)
	b.WriteString("|" + d.timeCol + "|" + strconv.Itoa(d.renderIdx) + "|" + d.customColorSQL)
	b.WriteString("|" + strconv.FormatFloat(d.sampling, 'g', -1, 64) + "|" + strconv.FormatBool(d.refine) +
		"|" + strconv.FormatBool(d.cache) + "|" + strconv.FormatBool(d.readoutOn))
	if d.hasArea {
		a := d.area
		b.WriteString("|area=" + strconv.FormatFloat(a.south, 'g', -1, 64) + "," + strconv.FormatFloat(a.west, 'g', -1, 64) +
			"," + strconv.FormatFloat(a.north, 'g', -1, 64) + "," + strconv.FormatFloat(a.east, 'g', -1, 64))
	}
	if d.reqValid {
		b.WriteString("|vp=" + strconv.FormatUint(uint64(d.req.minX), 10) + "," + strconv.FormatUint(uint64(d.req.maxX), 10) +
			"," + strconv.FormatUint(uint64(d.req.minY), 10) + "," + strconv.FormatUint(uint64(d.req.maxY), 10))
	}
	return b.String()
}

// mapFollows reports whether a command's later writes carry its writer: a
// task's do, the person's and the app's are the pane's own.
func mapFollows(writer string) bool { return writer != mapPaneId && writer != signalWriterApp }

func addMapOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	s.Resource(opsResMap, "the Map pane's raster settings, its selected area and the viewport it last published",
		func(inst *PlayLauncher) any {
			if inst.inner == nil {
				return ""
			}
			return mapOptionsDigest(inst.inner)
		})
	appops.Query(s, app.OperationSpec{Name: opGetMap, Version: 1,
		Summary: "read the Map pane: its table source, render and colour expression, sampling, the raster on screen and its errors, " +
			"the camera, the selected area and the Timeline window it filters by",
		Reads: []string{opsResMap, opsResPanes}, Agents: true, Untrusted: true},
		func(sn opsSnap, in appops.None) (MapReading, error) { return mapReading(&sn, in) })
	appops.Command(s, app.OperationSpec{Name: opSetMapView, Version: 1,
		Summary: "move the Map pane's camera: centre and zoom, or frame a lat/lon box",
		Effect:  app.OperationEffectView, Writes: []string{opsResMap}, Agents: true,
		Follows: []string{"the raster is fetched once for the new view, whether or not the person's live switch is on",
			"the fetch writes the vp_* signals with the task as writer; Live reruns a query that reads them"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetMapViewArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			writer := paneSignalWriter(call, mapPaneId)
			if err := p.mapDriver.setMapView(in, writer, mapFollows(writer)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
	appops.Command(s, app.OperationSpec{Name: opSetMapOptions, Version: 1,
		Summary: "set the Map pane's time column, render, sampling, ladder, cache or readout, or select or clear its area; the table source and colour expression are the person's",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResMap, opsResSignals}, Agents: true,
		Gesture: "the render combo, Clear area, and drawing a box with select area on",
		Follows: []string{"the raster is fetched again with the new settings, on the pane's lane, under the agent limits",
			"an area is written at once as the area_* signals; Live reruns a query that reads them",
			"get_map gives a source or expression the server rejected as the query error"}},
		func(inst *PlayLauncher, call app.OperationCall, in SetMapOptionsArgs) (appops.None, error) {
			p := inst.inner
			if p == nil {
				return appops.None{}, app.RefuseOperation("the window has not mounted")
			}
			// The table source and the colour expression are SQL the raster
			// lane runs on every draw whose viewport moves, long after the
			// task's mark is gone; they stay the person's, as the live switch
			// does (ADR-0270 update of 2026-10-06).
			if call.OnBehalfOf != nil && (in.Table != nil || in.ColorSql != nil) {
				return appops.None{}, app.RefuseOperation("the Map's table source and colour expression are the person's: they are SQL the raster runs on every pan, outside the task's checks; ask the person to set them")
			}
			writer := paneSignalWriter(call, mapPaneId)
			if err := p.setMapOptions(in, writer, mapFollows(writer)); err != nil {
				return appops.None{}, err
			}
			p.markAgent(call.OnBehalfOf)
			return appops.None{}, nil
		})
}
