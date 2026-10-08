package grib

import (
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Message is one GRIB message: the indicator and identification sections,
// the optional local-use section, and one or more [Field]. A GRIB2 message
// may repeat sections 2–7, 3–7 or 4–7 and so carry several fields sharing
// the sections before the repetition (ADR-0292 §R4); each field keeps its
// own byte offsets so an index can point at it.
type Message struct {
	// Offset and Length locate the message in the file: Offset is where the
	// indicator starts, Length what the indicator section declares (after
	// the edition 1 large-message convention). Skipped is how many non-GRIB
	// bytes preceded the message — GTS headers, blocking records, padding.
	Offset  int64
	Length  int64
	Skipped int64
	Edition uint8
	// Discipline is Section 0's discipline (edition 2 only).
	Discipline uint8
	Ident      Identification
	// Local is Section 2's payload after its 5-octet header, nil when the
	// section is absent. Its meaning is the producer's; the reader does not
	// interpret it (ADR-0292 §R8). In a multi-field message it is the
	// most recent Section 2 before each field, kept per [Field].
	Local []byte
	// Fields are the data fields in message order; an edition 1 message has
	// exactly one, whose product is described by [Message.Grib1] rather than
	// by [Field.Product].
	Fields []*Field
	// Grib1 is set for edition 1 messages.
	Grib1 *Grib1Header
	raw   []byte
	// grib1Large says the edition 1 length came from the large-message
	// convention, so the data section's coded length is not its length.
	grib1Large bool
}

// Identification is Section 1 (edition 2) or the corresponding octets of
// the product definition section (edition 1). RefTime is the reference time
// exactly as coded, seconds included: producers do code non-zero seconds
// (MRMS writes the composite's clock time).
type Identification struct {
	Centre           uint16
	SubCentre        uint16
	TablesVersion    uint8
	LocalTables      uint8
	RefSignificance  uint8
	RefTime          time.Time
	ProductionStatus uint8
	TypeOfData       uint8
}

// Raw is the message's bytes, indicator to end marker. The slice is the
// message's own; callers never write through it.
func (inst *Message) Raw() (b []byte) {
	return inst.raw
}

// Field is one data field of a message: its grid, product, packing, bitmap
// and data sections, with each section's offset in the file. Values are
// decoded by [Field.ValuesE]; nothing is decoded until then.
type Field struct {
	Message *Message
	// Index is the field's position in its message, from 0.
	Index int
	// Local is the Section 2 payload in force for this field (see
	// [Message.Local]).
	Local   []byte
	Grid    Grid
	Product Product
	Packing Packing
	Bitmap  Bitmap
	// Offsets of sections 3 to 7 in the file, for an index (ADR-0292 §R9);
	// zero when the section was inherited from an earlier field and is
	// located there.
	Section3Offset, Section4Offset, Section5Offset, Section6Offset, Section7Offset int64
	// data is Section 7's payload after the 5-octet header.
	data []byte
}

// NumPoints is the grid's point count: the length of [Field.ValuesE]'s
// result, missing points included.
func (inst *Field) NumPoints() (n int) {
	n = int(inst.Grid.NumPoints)
	return
}

// sectionHeader is the 5-octet header every GRIB2 section but 0 and 8
// carries.
func sectionHeader(b []byte) (length uint32, number uint8, ok bool) {
	if len(b) < 5 {
		return
	}
	length = uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	number = b[4]
	ok = true
	return
}

func (inst *Message) parseGrib2E() (err error) {
	raw := inst.raw
	inst.Discipline = raw[6]
	pos := 16
	end := len(raw) - 4 // the end marker
	var (
		local            []byte
		sec3, sec4       []byte
		sec5             []byte
		sec7             []byte
		off3, off4       int64
		off5, off6, off7 int64
		grid             Grid
		product          Product
		packing          Packing
		bitmap           Bitmap
		lastSection      uint8
		bitmapSeen       bool
		havePendingField bool
		haveIdent        bool
	)
	inst.Fields = make([]*Field, 0, 1)
	for pos < end {
		length, number, ok := sectionHeader(raw[pos:end])
		if !ok {
			err = eb.Build().Int64("offset", inst.Offset+int64(pos)).Errorf("section header truncated: %w", ErrMalformed)
			return
		}
		if length < 5 || int64(length) > int64(end-pos) {
			err = eb.Build().Int64("offset", inst.Offset+int64(pos)).Uint8("section", number).Uint32("length", length).Int("available", end-pos).Errorf("section length outside message: %w", ErrMalformed)
			return
		}
		body := raw[pos+5 : pos+int(length)]
		fileOff := inst.Offset + int64(pos)
		if number != 1 && !haveIdent {
			err = eb.Build().Int64("offset", fileOff).Uint8("section", number).Errorf("section before identification: %w", ErrMalformed)
			return
		}
		// A section number that does not increase starts a new field; only
		// 2, 3 and 4 may do so, and only once a field is complete.
		if number <= lastSection {
			if !havePendingField || number < 2 || number > 4 || lastSection != 7 {
				err = eb.Build().Int64("offset", fileOff).Uint8("section", number).Uint8("previous", lastSection).Errorf("section out of order: %w", ErrMalformed)
				return
			}
		}
		switch number {
		case 1:
			if haveIdent {
				err = eb.Build().Int64("offset", fileOff).Errorf("second identification section: %w", ErrMalformed)
				return
			}
			err = inst.Ident.parseE(body)
			if err != nil {
				return
			}
			haveIdent = true
		case 2:
			local = body
			if inst.Local == nil {
				inst.Local = body
			}
		case 3:
			sec3 = body
			off3 = fileOff
			grid, err = parseGridE(body)
			if err != nil {
				return
			}
		case 4:
			sec4 = body
			off4 = fileOff
			if sec3 == nil {
				err = eb.Build().Int64("offset", fileOff).Errorf("product definition before any grid definition: %w", ErrMalformed)
				return
			}
			product, err = parseProductE(body)
			if err != nil {
				return
			}
		case 5:
			sec5 = body
			off5 = fileOff
			if sec4 == nil {
				err = eb.Build().Int64("offset", fileOff).Errorf("data representation before any product definition: %w", ErrMalformed)
				return
			}
			packing, err = parsePackingE(body)
			if err != nil {
				return
			}
			bitmapSeen = false
		case 6:
			off6 = fileOff
			if sec5 == nil {
				err = eb.Build().Int64("offset", fileOff).Errorf("bitmap before any data representation: %w", ErrMalformed)
				return
			}
			bitmap, err = parseBitmapE(body, grid.NumPoints)
			if err != nil {
				return
			}
			bitmapSeen = true
		case 7:
			sec7 = body
			off7 = fileOff
			if sec5 == nil || !bitmapSeen {
				err = eb.Build().Int64("offset", fileOff).Errorf("data section without representation and bitmap sections: %w", ErrMalformed)
				return
			}
			f := &Field{
				Message: inst, Index: len(inst.Fields), Local: local,
				Grid: grid, Product: product, Packing: packing, Bitmap: bitmap,
				Section3Offset: off3, Section4Offset: off4, Section5Offset: off5, Section6Offset: off6, Section7Offset: off7,
				data: sec7,
			}
			inst.Fields = append(inst.Fields, f)
			havePendingField = true
		default:
			err = eb.Build().Int64("offset", fileOff).Uint8("section", number).Errorf("unknown section number: %w", ErrMalformed)
			return
		}
		lastSection = number
		pos += int(length)
	}
	if pos != end {
		err = eb.Build().Int64("offset", inst.Offset).Int("position", pos).Int("end", end).Errorf("sections do not meet the end marker: %w", ErrMalformed)
		return
	}
	if len(inst.Fields) == 0 {
		err = eb.Build().Int64("offset", inst.Offset).Errorf("message carries no data field: %w", ErrMalformed)
		return
	}
	return
}

// parseE reads Section 1 of edition 2.
func (inst *Identification) parseE(body []byte) (err error) {
	r := rd{b: body}
	inst.Centre = r.u16()
	inst.SubCentre = r.u16()
	inst.TablesVersion = r.u8()
	inst.LocalTables = r.u8()
	inst.RefSignificance = r.u8()
	year := int(r.u16())
	month, day, hour, minute, second := int(r.u8()), int(r.u8()), int(r.u8()), int(r.u8()), int(r.u8())
	inst.ProductionStatus = r.u8()
	inst.TypeOfData = r.u8()
	err = r.errE("identification section")
	if err != nil {
		return
	}
	inst.RefTime, err = makeTimeE(year, month, day, hour, minute, second)
	return
}

// makeTimeE builds a UTC instant from coded calendar fields, refusing what
// no calendar holds. GRIB has no leap seconds; a second of 60 is malformed.
func makeTimeE(year, month, day, hour, minute, second int) (t time.Time, err error) {
	if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59 {
		err = eb.Build().Int("year", year).Int("month", month).Int("day", day).Int("hour", hour).Int("minute", minute).Int("second", second).Errorf("reference time outside the calendar: %w", ErrMalformed)
		return
	}
	t = time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if t.Day() != day {
		err = eb.Build().Int("year", year).Int("month", month).Int("day", day).Errorf("day outside its month: %w", ErrMalformed)
	}
	return
}
