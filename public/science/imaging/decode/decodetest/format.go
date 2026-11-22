package decodetest

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"io"
	"math"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/decode"
)

// The kit's own format (ADR-0398 §SD9). It exists so that this tree's
// tests can give an app stacks, entries, placements, invalid values and
// refusals through a registry without a real format; it is not an
// interchange format, and nothing outside tests should write or read it.
//
// Layout, every number little-endian:
//
//	0        16 bytes  magic: 0x89 "DECODETEST" "\r\n" 0x1a "\n" 0x01
//	16       uint32    T, the table's length in bytes, at most 16 MiB
//	20       T bytes   the table: a JSON object {"members": [...]}, one
//	                   member per entry — its id, title, detail, whether it
//	                   is the default, the reason it is refused when it is,
//	                   its width, height, sample kind and colour model by
//	                   name, its axes (name, role, size, unit, coordinates,
//	                   labels), a value transform per plane, its invalid
//	                   value, its placement, the provenance fields a reader
//	                   fills (variant, bit depth, colour type, lossy,
//	                   derivations, colour declaration, notes), and where
//	                   its samples are
//	20 + T   ...       samples: each member's planes in the stack's order,
//	                   each row-major in its kind, at the offset the table
//	                   gives

// FormatName is the test format's name in a registry, and the
// Provenance.Format of what its reader decodes.
const FormatName = "decodetest"

// magic begins every file of the test format.
var magic = []byte("\x89DECODETEST\r\n\x1a\n\x01")

const (
	headLen  = 20
	maxTable = 16 << 20
)

// Member is one entry of a test-format file, as [Write] takes it.
type Member struct {
	// Id is the entry's id; empty takes its index in decimal.
	Id string
	// Title names it for a person; empty takes "member " and the id.
	Title string
	// Detail is listed as the entry's Detail.
	Detail string
	// Stack is what the entry decodes to: its planes, axes, colour model,
	// placement, invalid value, value transforms, and the provenance
	// fields a reader fills. The decode gives a stack of the same digest.
	Stack imaging.Stack
	// Default marks the entry a plain open reads; when no member says so,
	// the first readable one is, and in a file with none, the first.
	Default bool
	// Unsupported, when not empty, lists the entry as unreadable for this
	// reason, and a decode of it is refused as unsupported, naming it —
	// the way to exercise a reader's refusal. Its Stack may be zero; when
	// it is not, its shape is listed and its samples are not written. A
	// refused member is the default only in a file with no readable one.
	Unsupported string
}

type tableJSON struct {
	Members []memberJSON `json:"members"`
}

type axisJSON struct {
	Name   string    `json:"name"`
	Role   string    `json:"role"`
	Size   int       `json:"size"`
	Unit   string    `json:"unit,omitzero"`
	Coords []float64 `json:"coords,omitzero"`
	Labels []string  `json:"labels,omitzero"`
}

type transformJSON struct {
	Scale  float64 `json:"scale"`
	Offset float64 `json:"offset"`
}

type placementJSON struct {
	Affine    [6]float64 `json:"affine"`
	Frame     string     `json:"frame"`
	Code      string     `json:"code,omitzero"`
	AxisNames [2]string  `json:"axisNames"`
	Units     [2]string  `json:"units"`
	RefLon    float64    `json:"refLon,omitzero"`
	RefLat    float64    `json:"refLat,omitzero"`
	LonPole   float64    `json:"lonPole,omitzero"`
	Source    string     `json:"source,omitzero"`
	Unapplied []string   `json:"unapplied,omitzero"`
}

type memberJSON struct {
	Id               string          `json:"id"`
	Title            string          `json:"title"`
	Detail           string          `json:"detail,omitzero"`
	Default          bool            `json:"default,omitzero"`
	Unsupported      string          `json:"unsupported,omitzero"`
	Width            int             `json:"width,omitzero"`
	Height           int             `json:"height,omitzero"`
	Kind             string          `json:"kind,omitzero"`
	Model            string          `json:"model,omitzero"`
	Axes             []axisJSON      `json:"axes,omitzero"`
	Transforms       []transformJSON `json:"transforms,omitzero"`
	Invalid          *float64        `json:"invalid,omitzero"`
	Placement        *placementJSON  `json:"placement,omitzero"`
	Variant          string          `json:"variant,omitzero"`
	StoredBitDepth   uint8           `json:"storedBitDepth,omitzero"`
	StoredColorType  string          `json:"storedColorType,omitzero"`
	Lossy            bool            `json:"lossy,omitzero"`
	Derivations      uint8           `json:"derivations,omitzero"`
	ColorDeclaration string          `json:"colorDeclaration,omitzero"`
	Notes            []string        `json:"notes,omitzero"`
	Offset           int64           `json:"offset,omitzero"`
	Length           int64           `json:"length,omitzero"`
}

// Write makes a test-format file of the members, in order. It refuses ids
// that repeat, two defaults, a refused default in a file with a readable
// member, and a readable member without a stack.
func Write(members ...Member) (file []byte, err error) {
	table := tableJSON{Members: make([]memberJSON, len(members))}
	defaults := 0
	readable := slices.ContainsFunc(members, func(m Member) bool { return m.Unsupported == "" })
	for _, m := range members {
		if m.Default {
			defaults++
			if m.Unsupported != "" && readable {
				return nil, eb.Build().Str("id", m.Id).Errorf("a refused member is the default only in a file with no readable one")
			}
		}
	}
	if defaults > 1 {
		return nil, eb.Build().Int("defaults", defaults).Errorf("at most one member is the default")
	}
	var samples [][]byte
	for i, m := range members {
		j := &table.Members[i]
		j.Id, j.Title, j.Detail, j.Unsupported = m.Id, m.Title, m.Detail, m.Unsupported
		if j.Id == "" {
			j.Id = strconv.Itoa(i)
		}
		if j.Title == "" {
			j.Title = "member " + j.Id
		}
		for _, o := range table.Members[:i] {
			if o.Id == j.Id {
				return nil, eb.Build().Str("id", j.Id).Errorf("two members share an id")
			}
		}
		if m.Unsupported == "" && m.Stack.IsZero() {
			return nil, eb.Build().Str("id", j.Id).Errorf("a readable member needs a stack")
		}
		if defaults == 0 && (m.Unsupported == "" || !readable) {
			j.Default, defaults = true, 1
		} else {
			j.Default = m.Default
		}
		if m.Stack.IsZero() {
			continue
		}
		describe(j, m.Stack)
		if m.Unsupported == "" {
			b := planeBytes(m.Stack)
			j.Length = int64(len(b))
			samples = append(samples, b)
		}
	}
	// The table holds the samples' offsets, which follow it: lay it out
	// until its length no longer moves the offsets.
	var tbl []byte
	for laid := -1; laid != len(tbl); {
		laid = len(tbl)
		at, k := int64(headLen+laid), 0
		for i := range table.Members {
			if table.Members[i].Length > 0 {
				table.Members[i].Offset = at
				at += int64(len(samples[k]))
				k++
			}
		}
		if tbl, err = json.Marshal(table); err != nil {
			return nil, eb.Build().Errorf("write the table: %w", err)
		}
	}
	file = make([]byte, 0, headLen+len(tbl))
	file = append(file, magic...)
	file = binary.LittleEndian.AppendUint32(file, uint32(len(tbl)))
	file = append(file, tbl...)
	for _, b := range samples {
		file = append(file, b...)
	}
	return file, nil
}

// describe records a stack's shape and metadata in its table entry.
func describe(j *memberJSON, st imaging.Stack) {
	j.Width, j.Height, j.Kind = st.Width(), st.Height(), st.Kind().String()
	if st.Model() != 0 {
		j.Model = st.Model().String()
	}
	for _, a := range st.Axes() {
		j.Axes = append(j.Axes, axisJSON{Name: a.Name, Role: a.Role.String(), Size: a.Size, Unit: a.Unit, Coords: a.Coords, Labels: a.Labels})
	}
	for flat := range st.PlaneCount() {
		p, _ := st.Plane(st.Index(flat))
		t := p.Transform()
		j.Transforms = append(j.Transforms, transformJSON{Scale: t.Scale, Offset: t.Offset})
		if v, ok := p.Invalid(); ok && flat == 0 {
			j.Invalid = &v
		}
	}
	if pl := st.Placement(); !pl.IsZero() {
		f := pl.Frame
		j.Placement = &placementJSON{Affine: pl.Affine, Frame: f.Kind.String(), Code: f.Code, AxisNames: f.AxisNames, Units: f.Units,
			RefLon: f.RefLon, RefLat: f.RefLat, LonPole: f.LonPole, Source: pl.Source, Unapplied: pl.Unapplied}
	}
	p := st.Provenance()
	j.Variant = p.Variant
	j.StoredBitDepth, j.StoredColorType, j.Lossy = p.StoredBitDepth, p.StoredColorType, p.Lossy
	j.Derivations, j.ColorDeclaration, j.Notes = uint8(p.Derivations), p.ColorDeclaration, p.Notes
}

// planeBytes is a stack's samples, plane after plane, little-endian.
func planeBytes(st imaging.Stack) []byte {
	out := make([]byte, 0, st.Bytes())
	for flat := range st.PlaneCount() {
		p, _ := st.Plane(st.Index(flat))
		switch p.Kind() {
		case imaging.SampleKindU8:
			out = append(out, p.U8()...)
		case imaging.SampleKindU16:
			for _, v := range p.U16() {
				out = binary.LittleEndian.AppendUint16(out, v)
			}
		case imaging.SampleKindI16:
			for _, v := range p.I16() {
				out = binary.LittleEndian.AppendUint16(out, uint16(v))
			}
		case imaging.SampleKindI32:
			for _, v := range p.I32() {
				out = binary.LittleEndian.AppendUint32(out, uint32(v))
			}
		case imaging.SampleKindF32:
			for _, v := range p.F32() {
				out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
			}
		case imaging.SampleKindF64:
			for _, v := range p.F64() {
				out = binary.LittleEndian.AppendUint64(out, math.Float64bits(v))
			}
		}
	}
	return out
}

// Reader is the test format's reader. It keeps the seam's contract and
// passes [Check]; its Format is [FormatName], version 1.
func Reader() decode.ReaderI { return testReader{} }

type testReader struct{}

var _ decode.ReaderI = testReader{}

func (testReader) Format() decode.Format {
	return decode.Format{Name: FormatName, Title: "the conformance kit's test format", Extensions: []string{".decodetest"}, Version: 1,
		Doc: "A container of the conformance kit, for tests only: stacks with their axes, placements and invalid values, and entries refused on purpose. Not an interchange format."}
}

func (testReader) Sniff(head []byte) bool { return bytes.HasPrefix(head, magic) }

// readTable reads and parses the head and the table.
func readTable(r io.ReaderAt, size int64) (t tableJSON, err error) {
	var head [headLen]byte
	if size < headLen {
		return t, decode.Malformed("a file shorter than the test format's head of " + strconv.Itoa(headLen) + " bytes")
	}
	if err = decode.ReadFullAt(r, head[:], 0, "the head"); err != nil {
		return
	}
	if !bytes.Equal(head[:len(magic)], magic) {
		return t, decode.Malformed("a file without the test format's magic")
	}
	n := int64(binary.LittleEndian.Uint32(head[len(magic):]))
	if n > maxTable {
		return t, decode.OverLimit("a table of " + strconv.FormatInt(n, 10) + " bytes (the reader reads at most " + strconv.Itoa(maxTable) + ")")
	}
	if headLen+n > size {
		return t, decode.Malformed("a table of " + strconv.FormatInt(n, 10) + " bytes in a file of " + strconv.FormatInt(size, 10))
	}
	tbl := make([]byte, n)
	if err = decode.ReadFullAt(r, tbl, headLen, "the table"); err != nil {
		return
	}
	if err = json.Unmarshal(tbl, &t); err != nil {
		return t, decode.MalformedWith("the table", err)
	}
	return t, nil
}

func (inst testReader) Entries(ctx context.Context, r io.ReaderAt, size int64) (entries []decode.Entry, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	t, err := readTable(r, size)
	if err != nil {
		return
	}
	return entriesOf(t)
}

// entriesOf lists a table's members as entries, refusing a table that
// breaks the format's own rules.
func entriesOf(t tableJSON) (entries []decode.Entry, err error) {
	entries = make([]decode.Entry, len(t.Members))
	defaults, readable := 0, 0
	for i, m := range t.Members {
		e := decode.Entry{Id: m.Id, Title: m.Title, Detail: m.Detail, Width: m.Width, Height: m.Height, Default: m.Default,
			Readable: true}
		if m.Unsupported != "" {
			e = e.Refused(decode.Unsupported(m.Unsupported))
		}
		if e.Id == "" || slices.ContainsFunc(entries[:i], func(o decode.Entry) bool { return o.Id == e.Id }) {
			return nil, decode.Malformed("member " + strconv.Itoa(i) + " has an empty or repeated id")
		}
		if m.Kind != "" {
			if e.Kind = kindNamed(m.Kind); e.Kind == 0 && e.Readable {
				e = e.Refused(decode.Unsupported("the sample kind " + strconv.Quote(m.Kind)))
			}
		}
		e.Planes = 1
		for _, a := range m.Axes {
			e.Planes = int(min(int64(e.Planes)*int64(min(max(a.Size, 0), math.MaxInt32)), math.MaxInt32))
		}
		if e.Readable {
			readable++
		}
		if e.Default {
			defaults++
		}
		entries[i] = e
	}
	if len(entries) > 0 && defaults != 1 {
		return nil, decode.Malformed("a table that marks " + strconv.Itoa(defaults) + " defaults")
	}
	if i := slices.IndexFunc(entries, func(e decode.Entry) bool { return e.Default }); readable > 0 && i >= 0 && !entries[i].Readable {
		return nil, decode.Malformed("the default member " + strconv.Quote(entries[i].Id) + " is not readable, and another is")
	}
	return entries, nil
}

func kindNamed(s string) imaging.SampleKindE {
	for _, k := range imaging.AllSampleKinds {
		if k.String() == s {
			return k
		}
	}
	return 0
}

func (inst testReader) Decode(ctx context.Context, r io.ReaderAt, size int64, req decode.Request) (stack imaging.Stack, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	t, err := readTable(r, size)
	if err != nil {
		return
	}
	entries, err := entriesOf(t)
	if err != nil {
		return
	}
	e, err := decode.SelectEntry(entries, req.Entry)
	if err != nil {
		return
	}
	m := t.Members[slices.IndexFunc(entries, func(x decode.Entry) bool { return x.Id == e.Id })]
	if err = req.Limits.Check(int64(m.Width), int64(m.Height), int64(e.Planes), e.Kind); err != nil {
		return
	}
	return decodeMember(ctx, r, size, m, e, req.SourceName)
}

// decodeMember reads a member's samples, a plane at a time, honouring ctx
// between planes, and assembles its stack.
func decodeMember(ctx context.Context, r io.ReaderAt, size int64, m memberJSON, e decode.Entry, sourceName string) (stack imaging.Stack, err error) {
	name := "member " + strconv.Quote(m.Id)
	n := int64(m.Width) * int64(m.Height)
	planeLen := n * int64(e.Kind.Bytes())
	if m.Width <= 0 || m.Height <= 0 || m.Length != planeLen*int64(e.Planes) {
		return stack, decode.Malformed(name + ": a size and a length of samples that do not agree")
	}
	if m.Offset < headLen || m.Offset > size-m.Length {
		return stack, decode.Malformed(name + ": samples at " + strconv.FormatInt(m.Offset, 10) + " of " +
			strconv.FormatInt(m.Length, 10) + " bytes in a file of " + strconv.FormatInt(size, 10))
	}
	if m.Transforms != nil && len(m.Transforms) != e.Planes {
		return stack, decode.Malformed(name + ": a value transform per plane, or none")
	}
	planes := make([]imaging.Plane, e.Planes)
	buf := make([]byte, planeLen)
	for i := range planes {
		if err = ctx.Err(); err != nil {
			return
		}
		if err = decode.ReadFullAt(r, buf, m.Offset+int64(i)*planeLen, name+": plane "+strconv.Itoa(i)); err != nil {
			return
		}
		if planes[i], err = planeOf(m.Width, m.Height, e.Kind, buf); err == nil && m.Transforms != nil {
			planes[i], err = planes[i].WithTransform(imaging.ValueTransform{Scale: m.Transforms[i].Scale, Offset: m.Transforms[i].Offset})
		}
		if err == nil && m.Invalid != nil {
			planes[i], err = planes[i].WithInvalid(*m.Invalid)
		}
		if err != nil {
			return stack, decode.MalformedWith(name+": plane "+strconv.Itoa(i), err)
		}
	}
	axes := make([]imaging.Axis, len(m.Axes))
	for i, a := range m.Axes {
		axes[i] = imaging.Axis{Name: a.Name, Size: a.Size, Unit: a.Unit, Coords: a.Coords, Labels: a.Labels}
		for _, role := range imaging.AllAxisRoles {
			if role.String() == a.Role {
				axes[i].Role = role
			}
		}
	}
	var model imaging.ColorModelE
	for _, cm := range imaging.AllColorModels {
		if cm.String() == m.Model {
			model = cm
		}
	}
	var placement imaging.Placement
	if p := m.Placement; p != nil {
		placement = imaging.Placement{Affine: p.Affine, Source: p.Source, Unapplied: p.Unapplied, Frame: imaging.Frame{Code: p.Code,
			AxisNames: p.AxisNames, Units: p.Units, RefLon: p.RefLon, RefLat: p.RefLat, LonPole: p.LonPole}}
		for _, k := range imaging.AllFrameKinds {
			if k.String() == p.Frame {
				placement.Frame.Kind = k
			}
		}
	}
	prov := imaging.Provenance{SourceName: sourceName, Format: FormatName, Variant: m.Variant, Decoder: "decodetest reader", StoredBitDepth: m.StoredBitDepth,
		StoredColorType: m.StoredColorType, Lossy: m.Lossy, Derivations: imaging.DerivationE(m.Derivations),
		ColorDeclaration: m.ColorDeclaration, Notes: m.Notes}
	if stack, err = imaging.NewStack(axes, planes, model, placement, prov); err != nil {
		return stack, decode.MalformedWith(name, err)
	}
	return stack, nil
}

// planeOf is a plane of kind from its little-endian samples.
func planeOf(w, h int, kind imaging.SampleKindE, b []byte) (imaging.Plane, error) {
	n := w * h
	switch kind {
	case imaging.SampleKindU8:
		return imaging.NewPlaneU8(w, h, slices.Clone(b[:n]))
	case imaging.SampleKindU16:
		s := make([]uint16, n)
		for i := range s {
			s[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
		return imaging.NewPlaneU16(w, h, s)
	case imaging.SampleKindI16:
		s := make([]int16, n)
		for i := range s {
			s[i] = int16(binary.LittleEndian.Uint16(b[2*i:]))
		}
		return imaging.NewPlaneI16(w, h, s)
	case imaging.SampleKindI32:
		s := make([]int32, n)
		for i := range s {
			s[i] = int32(binary.LittleEndian.Uint32(b[4*i:]))
		}
		return imaging.NewPlaneI32(w, h, s)
	case imaging.SampleKindF32:
		s := make([]float32, n)
		for i := range s {
			s[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
		}
		return imaging.NewPlaneF32(w, h, s)
	case imaging.SampleKindF64:
		s := make([]float64, n)
		for i := range s {
			s[i] = math.Float64frombits(binary.LittleEndian.Uint64(b[8*i:]))
		}
		return imaging.NewPlaneF64(w, h, s)
	}
	return imaging.Plane{}, eb.Build().Stringer("kind", kind).Errorf("no such sample kind")
}
