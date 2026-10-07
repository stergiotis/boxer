package adhocdata

// Column summaries (ADR-0288 (proposed) §SD5): the pass that seals and
// digests a stream also describes each column, so an agent learns what a
// dataset holds without opening it and the trail describes the data, not
// only its bytes.

import (
	"encoding/binary"
	"encoding/json/v2"
	"math"
	"math/bits"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/zeebo/xxh3"
)

const (
	// SummarySampleValues bounds a column's sample: its first non-null
	// values in stream order.
	SummarySampleValues = 5
	// SummaryValueRunes bounds one rendered value; a longer one is cut and
	// marked with an ellipsis.
	SummaryValueRunes = 64
)

// ColumnSummaries describes a dataset's columns, index for index. Min, Max
// and each element of Sample are JSON literals, so a string column's empty
// minimum ("") is told apart from a type that does not order (empty
// text); a value the type cannot spell in JSON — a timestamp, a NaN — is a
// JSON string of its Arrow rendering. Distinct is a HyperLogLog estimate,
// within about 2% (precision 12). Sample is a JSON array.
type ColumnSummaries struct {
	Names    []string
	Types    []string
	Nulls    []uint64
	Min      []string
	Max      []string
	Distinct []uint64
	Sample   []string
}

// Len is the number of columns described.
func (inst *ColumnSummaries) Len() (n int) { return len(inst.Names) }

func (inst *ColumnSummaries) appendAll(o ColumnSummaries) {
	inst.Names = append(inst.Names, o.Names...)
	inst.Types = append(inst.Types, o.Types...)
	inst.Nulls = append(inst.Nulls, o.Nulls...)
	inst.Min = append(inst.Min, o.Min...)
	inst.Max = append(inst.Max, o.Max...)
	inst.Distinct = append(inst.Distinct, o.Distinct...)
	inst.Sample = append(inst.Sample, o.Sample...)
}

// columnsOf is the summaries of the live dataset under handle.
func (inst *Service) columnsOf(handle string) (cols ColumnSummaries, ok bool) {
	p, found := inst.reg.Lookup(handle)
	if !found {
		return
	}
	rec, isRec := p.(*record)
	if !isRec {
		return
	}
	rec.mu.RLock()
	cols = rec.columns
	rec.mu.RUnlock()
	return cols, true
}

// summarizer accumulates a stream's columns batch by batch.
type summarizer struct {
	cols []columnSummarizer
}

func newSummarizer(schema *arrow.Schema) (inst *summarizer) {
	fields := schema.Fields()
	inst = &summarizer{cols: make([]columnSummarizer, len(fields))}
	for i, f := range fields {
		inst.cols[i].name, inst.cols[i].typ = f.Name, f.Type.String()
	}
	return
}

// observe takes one record batch of the stream in.
func (inst *summarizer) observe(rec arrow.RecordBatch) {
	for i := range inst.cols {
		inst.cols[i].observe(rec.Column(i))
	}
}

// result renders what was observed.
func (inst *summarizer) result() (s ColumnSummaries) {
	n := len(inst.cols)
	s = ColumnSummaries{Names: make([]string, 0, n), Types: make([]string, 0, n), Nulls: make([]uint64, 0, n),
		Min: make([]string, 0, n), Max: make([]string, 0, n), Distinct: make([]uint64, 0, n), Sample: make([]string, 0, n)}
	for i := range inst.cols {
		c := &inst.cols[i]
		s.Names = append(s.Names, c.name)
		s.Types = append(s.Types, c.typ)
		s.Nulls = append(s.Nulls, c.nulls)
		s.Min = append(s.Min, c.min)
		s.Max = append(s.Max, c.max)
		s.Distinct = append(s.Distinct, c.hll.estimate())
		s.Sample = append(s.Sample, "["+strings.Join(c.sample, ",")+"]")
	}
	return
}

type columnSummarizer struct {
	name, typ string
	nulls     uint64
	hll       hyperLogLog
	sample    []string
	word      [8]byte

	seen       bool
	minI, maxI int64
	minU, maxU uint64
	minF, maxF float64
	minS, maxS string
	// min and max are the extremes rendered when they were found: the
	// array that held them is gone by the end of the stream.
	min, max string
}

func (inst *columnSummarizer) observe(arr arrow.Array) {
	inst.nulls += uint64(arr.NullN())
	switch a := arr.(type) {
	case *array.Int8:
		observeSigned(inst, a, a.Int8Values())
	case *array.Int16:
		observeSigned(inst, a, a.Int16Values())
	case *array.Int32:
		observeSigned(inst, a, a.Int32Values())
	case *array.Int64:
		observeSigned(inst, a, a.Int64Values())
	case *array.Date32:
		observeSigned(inst, a, a.Date32Values())
	case *array.Date64:
		observeSigned(inst, a, a.Date64Values())
	case *array.Timestamp:
		observeSigned(inst, a, a.TimestampValues())
	case *array.Time32:
		observeSigned(inst, a, a.Time32Values())
	case *array.Time64:
		observeSigned(inst, a, a.Time64Values())
	case *array.Duration:
		observeSigned(inst, a, a.DurationValues())
	case *array.Uint8:
		observeUnsigned(inst, a, a.Uint8Values())
	case *array.Uint16:
		observeUnsigned(inst, a, a.Uint16Values())
	case *array.Uint32:
		observeUnsigned(inst, a, a.Uint32Values())
	case *array.Uint64:
		observeUnsigned(inst, a, a.Uint64Values())
	case *array.Float32:
		observeFloat(inst, a, a.Float32Values())
	case *array.Float64:
		observeFloat(inst, a, a.Float64Values())
	case *array.Boolean:
		for i := range a.Len() {
			if a.IsNull(i) {
				continue
			}
			lit := strconv.FormatBool(a.Value(i))
			inst.hll.add(xxh3.HashString(lit))
			inst.addSample(lit)
		}
	case *array.String:
		observeText(inst, a, a.Value)
	case *array.LargeString:
		observeText(inst, a, a.Value)
	default:
		for i := range arr.Len() {
			if arr.IsNull(i) {
				continue
			}
			v := arr.ValueStr(i)
			inst.hll.add(xxh3.HashString(v))
			inst.addSample(jsonText(v))
		}
	}
}

func (inst *columnSummarizer) hashWord(v uint64) (h uint64) {
	binary.LittleEndian.PutUint64(inst.word[:], v)
	return xxh3.Hash(inst.word[:])
}

func (inst *columnSummarizer) addSample(literal string) {
	if len(inst.sample) < SummarySampleValues {
		inst.sample = append(inst.sample, literal)
	}
}

func observeSigned[T ~int8 | ~int16 | ~int32 | ~int64](inst *columnSummarizer, arr arrow.Array, vals []T) {
	// A plain integer is a JSON number; a date, time or duration is spelt
	// as Arrow renders it.
	numeric := isPlainInteger(arr.DataType())
	for i, tv := range vals {
		if arr.IsNull(i) {
			continue
		}
		v := int64(tv)
		inst.hll.add(inst.hashWord(uint64(v)))
		if len(inst.sample) < SummarySampleValues {
			inst.addSample(renderSigned(arr, i, v, numeric))
		}
		if !inst.seen || v < inst.minI {
			inst.minI, inst.min = v, renderSigned(arr, i, v, numeric)
		}
		if !inst.seen || v > inst.maxI {
			inst.maxI, inst.max = v, renderSigned(arr, i, v, numeric)
		}
		inst.seen = true
	}
}

func observeUnsigned[T ~uint8 | ~uint16 | ~uint32 | ~uint64](inst *columnSummarizer, arr arrow.Array, vals []T) {
	for i, tv := range vals {
		if arr.IsNull(i) {
			continue
		}
		v := uint64(tv)
		inst.hll.add(inst.hashWord(v))
		if len(inst.sample) < SummarySampleValues {
			inst.addSample(strconv.FormatUint(v, 10))
		}
		if !inst.seen || v < inst.minU {
			inst.minU, inst.min = v, strconv.FormatUint(v, 10)
		}
		if !inst.seen || v > inst.maxU {
			inst.maxU, inst.max = v, strconv.FormatUint(v, 10)
		}
		inst.seen = true
	}
}

func observeFloat[T ~float32 | ~float64](inst *columnSummarizer, arr arrow.Array, vals []T) {
	for i, tv := range vals {
		if arr.IsNull(i) {
			continue
		}
		v := float64(tv)
		inst.hll.add(inst.hashWord(math.Float64bits(v)))
		if len(inst.sample) < SummarySampleValues {
			inst.addSample(renderFloat(v))
		}
		// NaN orders against nothing; it is counted and sampled only.
		if math.IsNaN(v) {
			continue
		}
		if !inst.seen || v < inst.minF {
			inst.minF, inst.min = v, renderFloat(v)
		}
		if !inst.seen || v > inst.maxF {
			inst.maxF, inst.max = v, renderFloat(v)
		}
		inst.seen = true
	}
}

func observeText(inst *columnSummarizer, arr arrow.Array, value func(i int) string) {
	for i := range arr.Len() {
		if arr.IsNull(i) {
			continue
		}
		v := value(i)
		inst.hll.add(xxh3.HashString(v))
		if len(inst.sample) < SummarySampleValues {
			inst.addSample(jsonText(v))
		}
		if !inst.seen || v < inst.minS {
			// The comparison keeps the whole value; only its rendering is cut.
			inst.minS, inst.min = strings.Clone(v), jsonText(v)
		}
		if !inst.seen || v > inst.maxS {
			inst.maxS, inst.max = strings.Clone(v), jsonText(v)
		}
		inst.seen = true
	}
}

func isPlainInteger(dt arrow.DataType) (plain bool) {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64:
		return true
	default:
		return false
	}
}

func renderSigned(arr arrow.Array, i int, v int64, numeric bool) (literal string) {
	if numeric {
		return strconv.FormatInt(v, 10)
	}
	return jsonText(arr.ValueStr(i))
}

// renderFloat spells a finite float as a JSON number and the rest as a
// JSON string.
func renderFloat(v float64) (literal string) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return jsonText(strconv.FormatFloat(v, 'g', -1, 64))
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// jsonText is s as a JSON string, cut to SummaryValueRunes.
func jsonText(s string) (literal string) {
	if utf8.RuneCountInString(s) > SummaryValueRunes {
		cut := 0
		for range SummaryValueRunes {
			_, size := utf8.DecodeRuneInString(s[cut:])
			cut += size
		}
		s = s[:cut] + "…"
	}
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// hyperLogLog estimates a count of distinct values in 4 KiB (Flajolet et
// al. 2007, with linear counting for small cardinalities).
type hyperLogLog struct {
	registers [hllRegisters]uint8
}

const (
	hllPrecision = 12
	hllRegisters = 1 << hllPrecision
)

func (inst *hyperLogLog) add(h uint64) {
	idx := h >> (64 - hllPrecision)
	w := h<<hllPrecision | 1<<(hllPrecision-1)
	rank := uint8(bits.LeadingZeros64(w) + 1)
	if rank > inst.registers[idx] {
		inst.registers[idx] = rank
	}
}

func (inst *hyperLogLog) estimate() (n uint64) {
	const m = float64(hllRegisters)
	alpha := 0.7213 / (1 + 1.079/m)
	var sum float64
	var zeros int
	for _, r := range inst.registers {
		sum += math.Ldexp(1, -int(r))
		if r == 0 {
			zeros++
		}
	}
	if zeros == hllRegisters {
		return 0
	}
	est := alpha * m * m / sum
	if est <= 2.5*m && zeros > 0 {
		est = m * math.Log(m/float64(zeros))
	}
	return uint64(math.Round(est))
}
