package play

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The Map's area selection (ADR-0096 2026-10-02 Update, SD10's hover→info
// deferral approached from the other side): with "select area" on, a drag on
// the map draws a box instead of panning (portolan's SetBoxSelect), and the
// released box is published as play-wide signals, in mercator units for a
// table with mercator_x/mercator_y and in degrees for any other. The editor's
// own query, reading them, is the report on what the box holds. Seeded to the
// whole world, so a query filtering on them runs before anything is selected.

const (
	signalAreaMinX   SignalID = "area_min_x"
	signalAreaMaxX   SignalID = "area_max_x"
	signalAreaMinY   SignalID = "area_min_y"
	signalAreaMaxY   SignalID = "area_max_y"
	signalAreaMinLat SignalID = "area_min_lat"
	signalAreaMaxLat SignalID = "area_max_lat"
	signalAreaMinLon SignalID = "area_min_lon"
	signalAreaMaxLon SignalID = "area_max_lon"
)

// mapArea is a selected box as drawn (lon may lie on another world copy).
type mapArea struct {
	south, west, north, east float64
}

// areaSignals are the values a selected box publishes: the longitudes folded
// onto the world the mercator columns cover, the latitudes kept, and the
// mercator box from the same projection the raster bins with (SD4).
func areaSignals(a mapArea) (merc mercBox, south, west, north, east float64) {
	west, east, _ = foldViewLon(a.west, a.east)
	south, north = max(a.south, -90), min(a.north, 90)
	merc = mercBox{
		minX: lonToMercX(west), maxX: lonToMercX(east),
		minY: latToMercY(north), maxY: latToMercY(south),
	}
	return
}

// setArea publishes a selected box, or the whole world when has is false.
func (inst *MapDriver) setArea(a mapArea, has bool, emit SignalEmitterI) {
	inst.area, inst.hasArea = a, has
	if emit == nil {
		return
	}
	if !has {
		emit.Emit(signalAreaMinX, uint64(0))
		emit.Emit(signalAreaMaxX, uint64(mercUnitMax))
		emit.Emit(signalAreaMinY, uint64(0))
		emit.Emit(signalAreaMaxY, uint64(mercUnitMax))
		emit.Emit(signalAreaMinLat, float64(-90))
		emit.Emit(signalAreaMaxLat, float64(90))
		emit.Emit(signalAreaMinLon, float64(-180))
		emit.Emit(signalAreaMaxLon, float64(180))
		return
	}
	m, south, west, north, east := areaSignals(a)
	emit.Emit(signalAreaMinX, uint64(m.minX))
	emit.Emit(signalAreaMaxX, uint64(m.maxX))
	emit.Emit(signalAreaMinY, uint64(m.minY))
	emit.Emit(signalAreaMaxY, uint64(m.maxY))
	emit.Emit(signalAreaMinLat, south)
	emit.Emit(signalAreaMaxLat, north)
	emit.Emit(signalAreaMinLon, west)
	emit.Emit(signalAreaMaxLon, east)
}

// onSelected takes a box the map reported this frame.
func (inst *MapDriver) onSelected(ev portolan.Events, emit SignalEmitterI) {
	if !ev.SelectedOk {
		return
	}
	b := ev.Selected
	inst.setArea(mapArea{south: b.GetSouth(), west: b.GetWest(), north: b.GetNorth(), east: b.GetEast()}, true, emit)
}

// paintArea outlines the selected box where it was drawn.
func (inst *MapDriver) paintArea(p portolan.Projector) {
	if !inst.hasArea {
		return
	}
	a := inst.area
	lats := []float64{a.south, a.north, a.north, a.south, a.south}
	lngs := []float64{a.west, a.west, a.east, a.east, a.west}
	p.Polyline(lats, lngs, color.Hex(styletokens.InfoDefault.AsHex()), styletokens.StrokeStrong)
}

// areaStatus names the selected area for the status line; empty without one.
func (inst *MapDriver) areaStatus() string {
	if !inst.hasArea {
		return ""
	}
	_, south, west, north, east := areaSignals(inst.area)
	return fmt.Sprintf("area %.3f…%.3f N, %.3f…%.3f E", south, north, west, east)
}
