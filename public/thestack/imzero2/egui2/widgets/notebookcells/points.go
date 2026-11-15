package notebookcells

import (
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/db/clickhouse/chrows"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/notebookview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/landoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

const (
	// RendererPoints draws a relation's rows as places on a map: [ColLat]
	// and [ColLon] in decimal degrees, and [ColPlace] to name one. The map
	// frames the places it is handed and is as tall as the cell's ceiling.
	RendererPoints = "points"
	ColLat         = "lat"
	ColLon         = "lon"
	ColPlace       = "label"
)

const (
	// maxPoints bounds the places drawn: past it a relation is a density,
	// which is another drawing.
	maxPoints = 512
	// fitMaxZoom is as close as the map frames what it is handed: one place
	// is shown with the countries around it, and the outlines say little
	// beyond it.
	fitMaxZoom = 4
	// fitFrames is how many frames a new result is framed for: the map
	// learns its pane's size a frame after it is first drawn.
	fitFrames   = 2
	pointRadius = 5
	labelSize   = 12
)

func init() {
	notebookview.RegisterRenderer(RendererPoints, func(ids *c.WidgetIdStack) (notebookview.CellRenderer, func()) {
		p := &points{ids: ids}
		return p.render, p.close
	})
}

type points struct {
	ids   *c.WidgetIdStack
	m     *portolan.Map
	land  landoverlay.Layer
	atlas *worldmap.Atlas
	// seen is the relation the places were last read from.
	seen   arrow.RecordBatch
	at     []portolan.LatLng
	names  []string
	bounds portolan.LatLngBounds
	more   int
	fit    int
	reason string
}

func (inst *points) close() {
	if inst.m != nil {
		inst.m.Close()
	}
}

func (inst *points) render(res *mdnotebook.Result) (reason string) {
	if res.Main != inst.seen {
		inst.fill(res.Main)
	}
	if inst.reason != "" {
		return inst.reason
	}
	if inst.m == nil {
		// No tiles: a report is read where no tile server need be, and the
		// outlines place a point well enough to say where it is.
		inst.m = portolan.New(inst.ids, "points", portolan.Options{
			NoTiles: true, Center: inst.bounds.GetCenter(), Zoom: 1, NoScrollWheelZoom: true, NoKeyboard: true,
		})
		inst.atlas, _ = worldmap.LoadAtlas()
	}
	if inst.fit > 0 {
		inst.fit--
		if size := inst.m.View().Size(); size.X > 0 && size.Y > 0 {
			_ = inst.m.View().FitBounds(inst.bounds, portolan.FitOptions{
				Padding: portolan.Point{X: 32, Y: 32}, MaxZoom: fitMaxZoom, HasMaxZoom: true,
			})
		}
		c.RequestRepaint()
	}
	accent := color.Hex(styletokens.AccentDefault.AsHex())
	ink := color.Hex(styletokens.NeutralTextPrimary.AsHex())
	inst.m.RenderFill(480, 240, func(p portolan.Projector) {
		if inst.atlas != nil {
			// Outlines alone: the fill is the outline triangulated, and at
			// some views it draws a sliver across open sea, which on a map
			// of places reads as a line between two of them.
			land := landoverlay.DefaultStyle()
			land.NoFill = true
			inst.land.Paint(p, inst.atlas, land)
		}
		for range c.IdScope(inst.ids.PrepareStr("places")) {
			for i, ll := range inst.at {
				p.Marker(ll, pointRadius, accent)
				name := inst.names[i]
				if name == "" {
					continue
				}
				p.Label(ll, pointRadius+3, 0, 0, 1, name, labelSize, ink)
				at := p.ToCanvas(ll)
				c.PaintHitCircle(inst.ids.PrepareSeq(uint64(i)), float32(at.X), float32(at.Y), pointRadius+2).
					Label(name).
					Tooltip(strconv.FormatFloat(ll.Lat, 'f', 5, 64) + ", " + strconv.FormatFloat(ll.Lng, 'f', 5, 64)).
					Send()
			}
		}
	})
	if inst.more > 0 {
		for rt := range c.RichTextLabel("+" + strconv.Itoa(inst.more) + " more not shown") {
			rt.Small().Weak()
		}
	}
	return ""
}

// fill reads the relation's places and asks for them to be framed.
func (inst *points) fill(rec arrow.RecordBatch) {
	inst.seen = rec
	inst.at, inst.names, inst.more, inst.reason = inst.at[:0], inst.names[:0], 0, ""
	inst.bounds = portolan.LatLngBounds{}
	latCol, lonCol, nameCol := -1, -1, -1
	for i, f := range rec.Schema().Fields() {
		switch f.Name {
		case ColLat:
			latCol = i
		case ColLon:
			lonCol = i
		case ColPlace:
			nameCol = i
		}
	}
	if latCol < 0 || lonCol < 0 {
		inst.reason = "points reads the columns " + ColLat + " and " + ColLon
		return
	}
	for row := 0; row < int(rec.NumRows()); row++ {
		lat, okLat := chrows.Float64(rec.Column(latCol), row)
		lon, okLon := chrows.Float64(rec.Column(lonCol), row)
		if !okLat || !okLon || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			continue
		}
		if len(inst.at) == maxPoints {
			inst.more++
			continue
		}
		ll := portolan.LL(lat, lon)
		name := ""
		if nameCol >= 0 {
			name, _ = chrows.String(rec.Column(nameCol), row)
		}
		inst.at = append(inst.at, ll)
		inst.names = append(inst.names, name)
		inst.bounds = inst.bounds.Extend(ll)
	}
	if len(inst.at) == 0 {
		inst.reason = "no row has a place in " + ColLat + " and " + ColLon
		return
	}
	inst.fit = fitFrames
}
