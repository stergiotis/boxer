package trailviewer

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
)

// The day × hour view of ADR-0350 §SD11 (proposed), built by M5: one GROUP
// BY of the weekday and the hour over the timeline for the case, window and
// filter the table last read, in the time range picker's zone, drawn as a
// 7 × 24 grid with the count as text and a tint by count behind it, under a
// line that states the rows, the busiest cell, the share outside working
// hours and the zone. The view's question is off-hours activity.

// DayHourCell is one (weekday, hour) with its row count; the weekday is
// ClickHouse's toDayOfWeek mode 0, Monday 1 to Sunday 7.
type DayHourCell struct {
	Weekday uint8  `ch:"weekday"`
	Hour    uint8  `ch:"hour"`
	N       uint64 `ch:"n"`
}

const (
	// Working hours, for the share the line states: Monday to Friday, the
	// hour buckets from workStartHour up to workEndHour.
	workStartHour = 8
	workEndHour   = 18
	// dayHourTintAlpha is the tint's alpha behind the count, so the text
	// still reads against it.
	dayHourTintAlpha = 0x70
)

var weekdayNames = [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// dayHourSQL is the view's read: rows per (weekday, hour) in zone, an IANA
// name, over the case, window and filter. At most 168 groups by
// construction.
func (inst *marts) dayHourSQL(q listQuery, zone string) string {
	tl := trailviews.ViewTimeline
	ts, z := inst.col(tl, "ts"), sqlString(zone)
	return "SELECT toDayOfWeek(" + ts + ", 0, " + z + ") AS weekday, toHour(" + ts + ", " + z + ") AS hour, count() AS n FROM " +
		inst.table(tl) + " WHERE " + strings.Join(inst.rowsWhere(q, nil), " AND ") + " GROUP BY weekday, hour ORDER BY weekday, hour"
}

// dayHourData is the grid: the count per cell, the busiest cell, the rows
// outside working hours, and the zone the hours are in.
type dayHourData struct {
	zone     string
	fallback bool
	counts   [7][24]uint64
	rows     uint64
	cells    int
	maxN     uint64
	busiestD int
	busiestH int
	off      uint64
}

// buildDayHour folds the cells into the grid; a cell the read did not
// return is zero. fallback says the picker's zone had no IANA name, so the
// hours are UTC.
func buildDayHour(cells []DayHourCell, zone string, fallback bool) (d dayHourData) {
	d.zone, d.fallback = zone, fallback
	for _, x := range cells {
		if x.Weekday < 1 || x.Weekday > 7 || x.Hour > 23 || x.N == 0 {
			continue
		}
		di, hi := int(x.Weekday)-1, int(x.Hour)
		d.counts[di][hi] += x.N
		d.rows += x.N
		d.cells++
		if di >= 5 || hi < workStartHour || hi >= workEndHour {
			d.off += x.N
		}
		if d.counts[di][hi] > d.maxN {
			d.maxN, d.busiestD, d.busiestH = d.counts[di][hi], di, hi
		}
	}
	return
}

// offShare is the share of rows outside working hours, in whole percent.
func (inst *dayHourData) offShare() int {
	if inst.rows == 0 {
		return 0
	}
	return int(math.Round(100 * float64(inst.off) / float64(inst.rows)))
}

// line is the grid in words: rows, the busiest cell, the share outside
// Mon–Fri working hours, and the zone.
func (inst *dayHourData) line() string {
	if inst.rows == 0 {
		return fmt.Sprintf("0 rows in the case and window; hours in %s", inst.zoneNote())
	}
	return fmt.Sprintf("%d rows; busiest %s %02d:00 with %d; %d%% outside Mon–Fri %02d:00–%02d:00 %s",
		inst.rows, weekdayNames[inst.busiestD], inst.busiestH, inst.maxN, inst.offShare(), workStartHour, workEndHour, inst.zoneNote())
}

func (inst *dayHourData) zoneNote() string {
	if inst.fallback {
		return inst.zone + " (the picker's zone has no IANA name)"
	}
	return inst.zone
}

// aggZone is the zone the day × hour view groups in: the picker's, as the
// IANA name the picker itself injects into SQL, or UTC when it has none.
func (inst *App) aggZone() (zone string, fallback bool) {
	if id, err := timerangepicker.LookupTz(inst.picker.tzName); err == nil {
		if name, nerr := timerangepicker.IanaName(id); nerr == nil && name != "" {
			return name, false
		}
	}
	return "UTC", true
}

func (inst *App) dayHourKey() aggKey {
	zone, _ := inst.aggZone()
	return aggKey{gen: inst.cache.gen, zone: zone}
}

// readDayHour is the view's read for tickAnalytics.
func (inst *App) readDayHour(q listQuery) func(ctx context.Context, p *probe) (dayHourData, error) {
	zone, fallback := inst.aggZone()
	return func(ctx context.Context, p *probe) (d dayHourData, err error) {
		cells, err := queryRows[DayHourCell](ctx, p, p.marts.dayHourSQL(q, zone))
		if err != nil {
			return
		}
		return buildDayHour(cells, zone, fallback), nil
	}
}

// dayHourTint is the cell's tint for a count against the largest: the
// design system's sequential palette, translucent, with the count as text
// over it, so colour is never the only channel.
func dayHourTint(n uint64, maxN uint64) color.Color {
	t := float32(0)
	if maxN > 0 {
		t = float32(math.Sqrt(float64(n) / float64(maxN)))
	}
	return color.Hex((styletokens.Sequential(styletokens.SequentialDefault(), t).AsHex() & 0xffffff00) | dayHourTintAlpha)
}

// renderDayHour is the day × hour tab: the line and the 7 × 24 grid, days
// as rows and hours as columns.
func (inst *App) renderDayHour() {
	an := &inst.an
	d, ok := aggStatus(&an.dayhour, inst.dayHourKey())
	if !ok {
		return
	}
	c.Label(d.line()).Wrap().Send()
	g := &an.dayHourGrid
	hours := make([]string, 24)
	for h := range hours {
		hours[h] = fmt.Sprintf("%02d", h)
	}
	g.build(an.dayhour.ver, "day", weekdayNames[:], hours, func(i, j int) int64 { return int64(d.counts[i][j]) })
	cols := make([]recordtable.Column, 0, 25)
	cols = append(cols, recordtable.Column{Arrow: 0, Header: "day", Width: 60, WidthType: "text", NoSort: true, Cell: func(row int64) recordtable.Cell {
		return recordtable.Cell{Text: weekdayNames[row], LeftAlign: true}
	}})
	for h := range 24 {
		cols = append(cols, recordtable.Column{Arrow: h + 1, Header: hours[h], Hover: hours[h] + ":00 " + d.zone, Width: 40, WidthType: "number", DescFirst: true,
			Cell: func(row int64) recordtable.Cell {
				n := d.counts[row][h]
				if n == 0 {
					return recordtable.Cell{}
				}
				return recordtable.Cell{Text: strconv.FormatUint(n, 10), Fill: dayHourTint(n, d.maxN), Filled: true,
					Hover: weekdayNames[row] + " " + hours[h] + ":00 " + d.zone + ": " + strconv.FormatUint(n, 10) + " rows"}
			}})
	}
	g.order = g.sort.OrderFor(g.rec, g.rec.NumRows())
	res := recordtable.RenderGrid(recordtable.Input{Ids: inst.ids, ScopeKey: "trail-dayhour", Record: g.rec, Columns: cols, Sort: &g.sort,
		Order: g.order, Selected: -1, State: &g.state, RowHeight: 22})
	g.sort.Apply(res)
}
