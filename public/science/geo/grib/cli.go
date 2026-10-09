package grib

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/science/geo/grib/tables"
)

// NewCliCommand is the `grib` command group (ADR-0292): `ls`
// inventories the fields of one or more files without decoding them,
// `dump` decodes one file's fields and prints their statistics, `index`
// writes the fields of a file as JSON lines for range-driven readers, and
// `tables` regenerates the embedded WMO tables. All run with no server and
// no external program.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "grib",
		Usage: "inventory, index and decode GRIB files with the clean-room reader (ADR-0292)",
		Commands: []*cli.Command{
			newLsCommand(),
			newDumpCommand(),
			newIndexCommand(),
			newTablesCommand(),
		},
	}
	return
}

func newIndexCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:      "index",
		Usage:     "list every field of a file as JSON lines: offset, length, identity, and what the reader would refuse",
		ArgsUsage: "<file>",
		Description: "One JSON object per field, in file order, with the message's byte offset and length so a\n" +
			"consumer can fetch it by HTTP range; the parameter triplet, templates, level, reference and\n" +
			"forecast times, ensemble member, and the feature the reader would refuse, if any. A malformed\n" +
			"message ends the listing with an error after the good entries.",
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			if cmd.NArg() != 1 {
				err = eb.Build().Errorf("exactly one file is required")
				return
			}
			f, err := os.Open(cmd.Args().First())
			if err != nil {
				return
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				return
			}
			entries, scanErr := Index(f, st.Size())
			err = WriteIndex(cmd.Root().Writer, entries)
			if err != nil {
				return
			}
			err = scanErr
			return
		},
	}
	return
}

func newTablesCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "tables",
		Usage: "regenerate the embedded WMO tables from checkouts of wmo-im/GRIB2 and wmo-im/CCT",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "grib2", Usage: "checkout of wmo-im/GRIB2 at the wanted tag", Required: true, TakesFile: true},
			&cli.StringFlag{Name: "cct", Usage: "checkout of wmo-im/CCT", Required: true, TakesFile: true},
			&cli.StringFlag{Name: "version", Usage: "the GRIB2 tag the checkout is at, recorded in the output", Required: true},
			&cli.StringFlag{Name: "out", Usage: "directory to write into", Value: "public/science/geo/grib/tables/wmo", TakesFile: true},
		},
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			g, err := tables.Generate(cmd.String("grib2"), cmd.String("cct"), cmd.String("version"))
			if err != nil {
				return
			}
			err = g.Write(cmd.String("out"))
			if err != nil {
				return
			}
			fmt.Fprintf(cmd.Root().Writer, "wrote %s (%d bytes of codes, %d of templates)\n", cmd.String("out"), len(g.Codes), len(g.Templates))
			return
		},
	}
	return
}

func newLsCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:      "ls",
		Usage:     "list every field of the given files: offsets, templates, parameter triplet, level, times",
		ArgsUsage: "<file>...",
		Description: "One line per field, in file order, in the shape of a wgrib2 inventory: message.field, byte\n" +
			"offset, edition, centre, discipline.category.number, grid/product/packing template numbers,\n" +
			"point count, first surface, reference time, forecast offset. A field the reader would\n" +
			"refuse to decode is listed with the feature it would refuse; a malformed message ends the\n" +
			"file's listing with the error.",
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			if cmd.NArg() == 0 {
				err = eb.Build().Errorf("at least one file is required")
				return
			}
			for _, path := range cmd.Args().Slice() {
				err = lsFile(ctx, cmd, path)
				if err != nil {
					return
				}
			}
			return
		},
	}
	return
}

func lsFile(ctx context.Context, cmd *cli.Command, path string) (err error) {
	f, err := os.Open(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("open: %w", err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	w := cmd.Root().Writer
	if cmd.NArg() > 1 {
		fmt.Fprintf(w, "== %s\n", path)
	}
	n := 0
	for m, scanErr := range Scan(f, st.Size()) {
		if scanErr != nil {
			fmt.Fprintf(w, "!! %v\n", scanErr)
			break
		}
		n++
		for _, fld := range m.Fields {
			fmt.Fprintln(w, lsLine(n, m, fld))
		}
	}
	return
}

func lsLine(n int, m *Message, f *Field) (s string) {
	var b strings.Builder
	if m.Edition == 1 {
		h := m.Grib1
		fmt.Fprintf(&b, "%d.1\t%d\ted1\tcentre=%d\ttable=%d param=%d\tgrid=%d bits=%d\tnpts=%d\tlevel=%d:%d", n, m.Offset, m.Ident.Centre, h.TableVersion, h.Parameter, f.Grid.Grib1Type, f.Packing.Bits, f.Grid.NumPoints, h.LevelType, h.Level)
		if h.Local != nil {
			fmt.Fprintf(&b, "\tmember=%d/%d", h.Local.Number, h.Local.Total)
		}
		fmt.Fprintf(&b, "\tref=%s", m.Ident.RefTime.Format("2006-01-02T15:04:05Z"))
		if h.ForecastOffsetOK {
			fmt.Fprintf(&b, "\tfcst=%s", h.ForecastOffset)
			if h.HasInterval {
				fmt.Fprintf(&b, "..%s", h.IntervalEnd)
			}
		} else {
			fmt.Fprintf(&b, "\tP1=%d P2=%d[unit %d]", h.P1, h.P2, h.TimeUnit)
		}
		fmt.Fprintf(&b, " range=%d", h.TimeRange)
		if f.Bitmap.Present {
			b.WriteString("\tbitmap")
		}
		if err := f.Packing.supported(); err != nil {
			feature, _ := UnsupportedFeature(err)
			fmt.Fprintf(&b, "\trefuse: %s", feature)
		}
		s = b.String()
		return
	}
	p := &f.Product
	fmt.Fprintf(&b, "%d.%d\t%d\ted2\tcentre=%d\t", n, f.Index+1, m.Offset, m.Ident.Centre)
	if p.Prefixed {
		fmt.Fprintf(&b, "%d.%d.%d", m.Discipline, p.Category, p.Number)
		if e, ok := tables.Parameter(m.Discipline, p.Category, p.Number); ok {
			fmt.Fprintf(&b, " %q", e.Meaning)
		}
	} else {
		fmt.Fprintf(&b, "%d.?.?", m.Discipline)
	}
	fmt.Fprintf(&b, "\tgdt=%d pdt=%d drt=%d bits=%d\tnpts=%d", f.Grid.Template, p.Template, f.Packing.Template, f.Packing.Bits, f.Grid.NumPoints)
	if p.Prefixed {
		if p.Surface1.Missing {
			fmt.Fprintf(&b, "\tsfc=%d", p.Surface1.Type)
		} else {
			fmt.Fprintf(&b, "\tsfc=%d:%g", p.Surface1.Type, p.Surface1.Value)
		}
		if p.Ensemble != nil {
			fmt.Fprintf(&b, "\tmember=%d/%d", p.Ensemble.Number, p.Ensemble.Size)
		}
	}
	fmt.Fprintf(&b, "\tref=%s", m.Ident.RefTime.Format("2006-01-02T15:04:05Z"))
	if p.Prefixed {
		if p.ForecastOffsetOK {
			fmt.Fprintf(&b, "\tfcst=%s", p.ForecastOffset)
		} else {
			fmt.Fprintf(&b, "\tfcst=%d[unit %d]", p.ForecastTime, p.ForecastTimeUnit)
		}
		if p.Statistics != nil && !p.Statistics.IntervalEnd.IsZero() {
			fmt.Fprintf(&b, "\tuntil=%s", p.Statistics.IntervalEnd.Format("2006-01-02T15:04:05Z"))
		}
	}
	if f.Bitmap.Present {
		b.WriteString("\tbitmap")
	}
	if err := f.Packing.supported(); err != nil {
		feature, _ := UnsupportedFeature(err)
		fmt.Fprintf(&b, "\trefuse: %s", feature)
	} else if f.Bitmap.Indicator != 0 && f.Bitmap.Indicator != 255 {
		fmt.Fprintf(&b, "\trefuse: bitmap indicator %d", f.Bitmap.Indicator)
	}
	s = b.String()
	return
}

func newDumpCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:      "dump",
		Usage:     "decode every field of one file and print its sections and value statistics",
		ArgsUsage: "<file>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "field", Usage: "only this field, counted from 1 across the file", Value: 0},
			&cli.BoolFlag{Name: "values", Usage: "print every value in stored order, one per line, NaN for missing"},
			&cli.BoolFlag{Name: "points", Usage: "with --values, prefix each value with its latitude and longitude"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			if cmd.NArg() != 1 {
				err = eb.Build().Errorf("exactly one file is required")
				return
			}
			err = dumpFile(ctx, cmd, cmd.Args().First(), cmd.Int("field"), cmd.Bool("values"), cmd.Bool("points"))
			return
		},
	}
	return
}

func dumpFile(ctx context.Context, cmd *cli.Command, path string, only int, printValues bool, printPoints bool) (err error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("read: %w", err)
		return
	}
	w := cmd.Root().Writer
	n, k := 0, 0
	for m, scanErr := range ScanBytes(buf) {
		if scanErr != nil {
			fmt.Fprintf(w, "!! %v\n", scanErr)
			break
		}
		n++
		for _, f := range m.Fields {
			k++
			if only != 0 && only != k {
				continue
			}
			dumpField(ctx, cmd, n, k, m, f, printValues, printPoints)
		}
	}
	return
}

func dumpField(ctx context.Context, cmd *cli.Command, n, k int, m *Message, f *Field, printValues bool, printPoints bool) {
	w := cmd.Root().Writer
	fmt.Fprintf(w, "message %d field %d (#%d) at %d, %d bytes, %d skipped before\n", n, f.Index+1, k, m.Offset, m.Length, m.Skipped)
	centre := ""
	if name, ok := tables.Centre(m.Ident.Centre); ok {
		centre = " (" + name + ")"
	}
	fmt.Fprintf(w, "  identification: centre %d%s sub-centre %d tables %d local %d ref %s significance %d status %d type %d\n",
		m.Ident.Centre, centre, m.Ident.SubCentre, m.Ident.TablesVersion, m.Ident.LocalTables, m.Ident.RefTime.Format("2006-01-02T15:04:05Z"), m.Ident.RefSignificance, m.Ident.ProductionStatus, m.Ident.TypeOfData)
	if f.Local != nil {
		fmt.Fprintf(w, "  local section: %d bytes\n", len(f.Local))
	}
	g := &f.Grid
	fmt.Fprintf(w, "  grid: template 3.%d, %d points, earth shape %d, scan %#02x (i-negative=%t j-positive=%t j-consecutive=%t alternating=%t)\n",
		g.Template, g.NumPoints, g.Shape.Code, g.Scan.Raw, g.Scan.INegative, g.Scan.JPositive, g.Scan.JConsecutive, g.Scan.Alternating)
	switch {
	case g.LatLon != nil:
		ll := g.LatLon
		fmt.Fprintf(w, "    lat/lon: Ni %d Nj %d first (%g, %g) last (%g, %g) Di %g Dj %g", ll.Ni, ll.Nj, ll.Lat1, ll.Lon1, ll.Lat2, ll.Lon2, ll.Di, ll.Dj)
		if ll.N != 0 {
			fmt.Fprintf(w, " N %d", ll.N)
		}
		if ll.PL != nil {
			fmt.Fprintf(w, " reduced (%d rows)", len(ll.PL))
		}
		if ll.Rotated != nil {
			fmt.Fprintf(w, " rotated: south pole (%g, %g) angle %g", ll.Rotated.SouthPoleLat, ll.Rotated.SouthPoleLon, ll.Rotated.Angle)
		}
		if ll.UVRelativeToGrid {
			fmt.Fprint(w, " uv-relative-to-grid")
		}
		fmt.Fprintln(w)
	case g.Lambert != nil:
		l := g.Lambert
		fmt.Fprintf(w, "    lambert: Nx %d Ny %d first (%g, %g) LaD %g LoV %g Dx %g Dy %g Latin1 %g Latin2 %g\n", l.Nx, l.Ny, l.Lat1, l.Lon1, l.LaD, l.LoV, l.Dx, l.Dy, l.Latin1, l.Latin2)
	case g.PolarStereographic != nil:
		l := g.PolarStereographic
		fmt.Fprintf(w, "    polar stereographic: Nx %d Ny %d first (%g, %g) LaD %g LoV %g Dx %g Dy %g\n", l.Nx, l.Ny, l.Lat1, l.Lon1, l.LaD, l.LoV, l.Dx, l.Dy)
	case g.Mercator != nil:
		l := g.Mercator
		fmt.Fprintf(w, "    mercator: Ni %d Nj %d first (%g, %g) last (%g, %g) LaD %g Dx %g Dy %g\n", l.Ni, l.Nj, l.Lat1, l.Lon1, l.Lat2, l.Lon2, l.LaD, l.Dx, l.Dy)
	case g.Unstructured != nil:
		u := g.Unstructured
		fmt.Fprintf(w, "    unstructured: grid %d of reference %d uuid %x\n", u.NumberOfGridUsed, u.NumberOfGridInReference, u.UUID)
	}
	p := &f.Product
	if m.Edition == 1 {
		h := m.Grib1
		fmt.Fprintf(w, "  product (edition 1): table %d parameter %d process %d, level type %d value %d (%d/%d), unit %d P1 %d P2 %d range %d", h.TableVersion, h.Parameter, h.Process, h.LevelType, h.Level, h.Level1, h.Level2, h.TimeUnit, h.P1, h.P2, h.TimeRange)
		if h.ForecastOffsetOK {
			fmt.Fprintf(w, " (%s", h.ForecastOffset)
			if h.HasInterval {
				fmt.Fprintf(w, " to %s", h.IntervalEnd)
			}
			fmt.Fprint(w, ")")
		}
		fmt.Fprintln(w)
		if h.Local != nil {
			fmt.Fprintf(w, "    ecmwf local definition %d: class %d type %d stream %d expver %q member %d of %d\n", h.Local.Definition, h.Local.Class, h.Local.Type, h.Local.Stream, h.Local.ExpVer, h.Local.Number, h.Local.Total)
		}
		if len(h.VerticalCoordinates) > 0 {
			fmt.Fprintf(w, "    vertical coordinates: %d values\n", len(h.VerticalCoordinates))
		}
	} else {
		fmt.Fprintf(w, "  product: template 4.%d", p.Template)
	}
	if m.Edition == 1 {
	} else if p.Prefixed {
		fmt.Fprintf(w, ", parameter %d.%d.%d, process %d/%d/%d, unit %d forecast %d", m.Discipline, p.Category, p.Number, p.GeneratingProcessType, p.BackgroundProcess, p.GeneratingProcessID, p.ForecastTimeUnit, p.ForecastTime)
		if p.ForecastOffsetOK {
			fmt.Fprintf(w, " (%s)", p.ForecastOffset)
		}
		fmt.Fprintf(w, "\n    surfaces: %s / %s\n", surfaceString(p.Surface1), surfaceString(p.Surface2))
		if p.Ensemble != nil {
			fmt.Fprintf(w, "    ensemble: type %d member %d of %d\n", p.Ensemble.Type, p.Ensemble.Number, p.Ensemble.Size)
		}
		if p.Statistics != nil {
			fmt.Fprintf(w, "    statistics: until %s, %d missing, ranges", p.Statistics.IntervalEnd.Format("2006-01-02T15:04:05Z"), p.Statistics.NumMissing)
			for _, r := range p.Statistics.Ranges {
				fmt.Fprintf(w, " [stat %d incr-type %d length %d[unit %d] increment %d[unit %d]]", r.Statistic, r.IncrementType, r.Length, r.LengthUnit, r.Increment, r.IncrementUnit)
			}
			fmt.Fprintln(w)
		}
		if len(p.VerticalCoordinates) > 0 {
			fmt.Fprintf(w, "    vertical coordinates: %d values\n", len(p.VerticalCoordinates))
		}
	} else {
		fmt.Fprintf(w, " (%d template octets, not laid out)\n", len(p.Raw))
	}
	pk := &f.Packing
	fmt.Fprintf(w, "  packing: template 5.%d, %d coded values, reference %g, binary scale %d, decimal scale %d, %d bits", pk.Template, pk.NumValues, pk.Reference, pk.BinaryScale, pk.DecimalScale, pk.Bits)
	if pk.Complex != nil {
		fmt.Fprintf(w, ", %d groups, missing management %d, spatial order %d", pk.Complex.NumGroups, pk.Complex.MissingManagement, pk.Complex.SpatialOrder)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  bitmap: indicator %d\n", f.Bitmap.Indicator)
	values, err := f.Values(nil)
	if err != nil {
		if feature, ok := UnsupportedFeature(err); ok {
			fmt.Fprintf(w, "  values: refused — %s\n", feature)
		} else {
			fmt.Fprintf(w, "  values: error — %v\n", err)
		}
		return
	}
	missing := 0
	minV, maxV, sum := math.Inf(1), math.Inf(-1), 0.0
	for _, v := range values {
		if math.IsNaN(v) {
			missing++
			continue
		}
		minV, maxV, sum = math.Min(minV, v), math.Max(maxV, v), sum+v
	}
	present := len(values) - missing
	if present > 0 {
		fmt.Fprintf(w, "  values: %d, %d missing, min %g max %g mean %g\n", len(values), missing, minV, maxV, sum/float64(present))
	} else {
		fmt.Fprintf(w, "  values: %d, all missing\n", len(values))
	}
	if !printValues {
		return
	}
	var pointsErr error
	var next func() (float64, float64, bool)
	if printPoints {
		var points func(func(float64, float64) bool)
		points, pointsErr = f.Grid.Points()
		if pointsErr == nil {
			next, _ = iterPull(points)
		} else {
			fmt.Fprintf(w, "  points: %v\n", pointsErr)
		}
	}
	for _, v := range values {
		if next != nil {
			lat, lon, _ := next()
			fmt.Fprintf(w, "%.6f %.6f %s\n", lat, lon, strconv.FormatFloat(v, 'g', -1, 64))
			continue
		}
		fmt.Fprintln(w, strconv.FormatFloat(v, 'g', -1, 64))
	}
}

func surfaceString(s Surface) (str string) {
	name := ""
	if e, ok := tables.Code("4.5", uint32(s.Type)); ok && s.Type < 192 {
		name = " " + strconv.Quote(e.Meaning)
	}
	if s.Missing {
		str = fmt.Sprintf("type %d%s", s.Type, name)
		return
	}
	str = fmt.Sprintf("type %d%s value %g", s.Type, name, s.Value)
	return
}
