package grib

import (
	"bufio"
	"encoding/json/v2"
	"io"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Entry is one field's line in an index ([IndexE]): where it is and what identifies
// it, enough for a consumer to choose fields and fetch their bytes by range
// without decoding anything (ADR-0292 §R9). Times are UTC; a zero
// ForecastSeconds with ForecastKnown false means the unit had no duration.
type Entry struct {
	Offset int64 `json:"offset"`
	Length int64 `json:"length"`
	// Field is the field's index within its message, from 0.
	Field   int    `json:"field"`
	Edition uint8  `json:"edition"`
	Centre  uint16 `json:"centre"`
	// Discipline, Category and Number are the parameter triplet (edition 2);
	// for edition 1 Number holds the parameter and Category the table version.
	Discipline      uint8     `json:"discipline"`
	Category        uint8     `json:"category"`
	Number          uint8     `json:"number"`
	GridTemplate    uint16    `json:"gdt"`
	ProductTemplate uint16    `json:"pdt"`
	PackingTemplate uint16    `json:"drt"`
	Bits            uint8     `json:"bits"`
	NumPoints       uint32    `json:"points"`
	RefTime         time.Time `json:"reftime"`
	ForecastSeconds int64     `json:"forecast_s"`
	ForecastKnown   bool      `json:"forecast_known"`
	IntervalEnd     time.Time `json:"interval_end,omitzero"`
	SurfaceType     uint8     `json:"surface_type"`
	SurfaceValue    float64   `json:"surface_value"`
	SurfaceMissing  bool      `json:"surface_missing"`
	SurfaceType2    uint8     `json:"surface_type2"`
	SurfaceValue2   float64   `json:"surface_value2"`
	Member          uint8     `json:"member"`
	Members         uint8     `json:"members"`
	// Refuses names the feature the reader would refuse this field for, or
	// is empty.
	Refuses string `json:"refuses,omitempty"`
}

// IndexE lists every field of r in file order. A malformed message ends
// the listing and is returned as the error beside the entries before it.
func IndexE(r io.ReaderAt, size int64) (entries []Entry, err error) {
	for m, scanErr := range Scan(r, size) {
		if scanErr != nil {
			err = scanErr
			return
		}
		for _, f := range m.Fields {
			entries = append(entries, entryOf(m, f))
		}
	}
	return
}

// IndexBytesE is [IndexE] over a buffer.
func IndexBytesE(buf []byte) (entries []Entry, err error) {
	for m, scanErr := range ScanBytes(buf) {
		if scanErr != nil {
			err = scanErr
			return
		}
		for _, f := range m.Fields {
			entries = append(entries, entryOf(m, f))
		}
	}
	return
}

func entryOf(m *Message, f *Field) (e Entry) {
	e = Entry{Offset: m.Offset, Length: m.Length, Field: f.Index, Edition: m.Edition, Centre: m.Ident.Centre, RefTime: m.Ident.RefTime,
		GridTemplate: f.Grid.Template, PackingTemplate: f.Packing.Template, Bits: f.Packing.Bits, NumPoints: f.Grid.NumPoints}
	if m.Edition == 1 {
		h := m.Grib1
		e.Category = h.TableVersion
		e.Number = h.Parameter
		e.ProductTemplate = uint16(h.TimeRange)
		e.SurfaceType = h.LevelType
		e.SurfaceValue = float64(h.Level)
		if h.ForecastOffsetOK {
			e.ForecastSeconds = int64(h.ForecastOffset / time.Second)
			e.ForecastKnown = true
			if h.HasInterval {
				e.IntervalEnd = m.Ident.RefTime.Add(h.IntervalEnd)
			}
		}
		if h.Local != nil {
			e.Member, e.Members = h.Local.Number, h.Local.Total
		}
	} else {
		p := &f.Product
		e.Discipline = m.Discipline
		e.ProductTemplate = p.Template
		if p.Prefixed {
			e.Category, e.Number = p.Category, p.Number
			e.SurfaceType, e.SurfaceValue, e.SurfaceMissing = p.Surface1.Type, p.Surface1.Value, p.Surface1.Missing
			e.SurfaceType2, e.SurfaceValue2 = p.Surface2.Type, p.Surface2.Value
			if p.ForecastOffsetOK {
				e.ForecastSeconds = int64(p.ForecastOffset / time.Second)
				e.ForecastKnown = true
			}
			if p.Statistics != nil {
				e.IntervalEnd = p.Statistics.IntervalEnd
			}
			if p.Ensemble != nil {
				e.Member, e.Members = p.Ensemble.Number, p.Ensemble.Size
			}
		}
	}
	if err := f.Packing.supportedE(); err != nil {
		e.Refuses, _ = UnsupportedFeature(err)
	} else if f.Bitmap.Indicator != 0 && f.Bitmap.Indicator != 255 {
		e.Refuses = "bitmap indicator " + itoa(int(f.Bitmap.Indicator))
	}
	return
}

func itoa(n int) (s string) {
	s = string(rune('0' + n%10))
	if n >= 10 {
		s = itoa(n/10) + s
	}
	return
}

// WriteIndexE writes entries as JSON lines, one field per line, in a
// deterministic key order.
func WriteIndexE(w io.Writer, entries []Entry) (err error) {
	bw := bufio.NewWriter(w)
	for _, e := range entries {
		err = json.MarshalWrite(bw, e, json.Deterministic(true))
		if err != nil {
			err = eb.Build().Int64("offset", e.Offset).Errorf("write index line: %w", err)
			return
		}
		err = bw.WriteByte('\n')
		if err != nil {
			return
		}
	}
	err = bw.Flush()
	return
}

// ReadIndexE reads what [WriteIndexE] wrote.
func ReadIndexE(r io.Reader) (entries []Entry, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		err = json.Unmarshal(line, &e)
		if err != nil {
			err = eb.Build().Int("line", len(entries)+1).Errorf("read index line: %w", err)
			return
		}
		entries = append(entries, e)
	}
	err = sc.Err()
	return
}

// ReadE reads the field an entry names from r: the message at the entry's
// offset, and the field at its index. size bounds r; a reader that holds
// only the message's own bytes passes its length and an offset of zero
// through [Entry.At].
func (inst Entry) ReadE(r io.ReaderAt, size int64) (f *Field, err error) {
	for m, scanErr := range ScanFrom(r, size, inst.Offset) {
		if scanErr != nil {
			err = scanErr
			return
		}
		if inst.Field < 0 || inst.Field >= len(m.Fields) {
			err = eb.Build().Int("field", inst.Field).Int("fields", len(m.Fields)).Errorf("index names a field the message does not have: %w", ErrInconsistent)
			return
		}
		f = m.Fields[inst.Field]
		return
	}
	err = eb.Build().Int64("offset", inst.Offset).Errorf("no message at the index offset: %w", ErrMalformed)
	return
}

// At returns the entry relocated to offset, for a buffer that holds the
// message's bytes alone (a range request's body).
func (inst Entry) At(offset int64) (e Entry) {
	e = inst
	e.Offset = offset
	return
}
