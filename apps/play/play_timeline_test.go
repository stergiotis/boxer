package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
)

// schemaFromFields is a tiny helper that builds a *arrow.Schema from a
// flat list of (name, type) pairs. Keeps the table-driven cases below
// readable without dragging in arrow.Field literals.
func schemaFromFields(pairs ...any) (s *arrow.Schema) {
	if len(pairs)%2 != 0 {
		panic("schemaFromFields: pairs must be (name, type)*")
	}
	fields := make([]arrow.Field, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		name, ok := pairs[i].(string)
		if !ok {
			panic("schemaFromFields: odd entries must be string field names")
		}
		dt, ok := pairs[i+1].(arrow.DataType)
		if !ok {
			panic("schemaFromFields: even entries must be arrow.DataType")
		}
		fields = append(fields, arrow.Field{Name: name, Type: dt, Nullable: true})
	}
	s = arrow.NewSchema(fields, nil)
	return
}

func tsMS() arrow.DataType { return &arrow.TimestampType{Unit: arrow.Millisecond, TimeZone: "UTC"} }
func tsNS() arrow.DataType { return &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"} }

func TestResolveContract(t *testing.T) {
	tests := []struct {
		name           string
		schema         *arrow.Schema
		wantMode       timelineMode
		wantRejectSub  string // substring match on Reject (empty = expect no reject)
		wantColTime    int32  // -1 if don't-care
		wantColTimeEnd int32  // -1 if don't-care
		wantColLabel   int32  // -1 if don't-care
		wantColLane    int32
		wantColInt     int32
	}{
		{
			name:           "Points: _tl_time only",
			schema:         schemaFromFields(timelineSlotTime, tsMS()),
			wantMode:       timelineModePoints,
			wantColTime:    0,
			wantColTimeEnd: -1,
			wantColLabel:   -1,
			wantColLane:    -1,
			wantColInt:     -1,
		},
		{
			name: "Intervals: _tl_time + _tl_time_end",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotTimeEnd, tsMS(),
			),
			wantMode:       timelineModeIntervals,
			wantColTime:    0,
			wantColTimeEnd: 1,
			wantColLabel:   -1,
			wantColLane:    -1,
			wantColInt:     -1,
		},
		{
			name: "Annotations: _tl_time + _tl_label",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotLabel, arrow.BinaryTypes.String,
			),
			wantMode:       timelineModeAnnotations,
			wantColTime:    0,
			wantColTimeEnd: -1,
			wantColLabel:   1,
			wantColLane:    -1,
			wantColInt:     -1,
		},
		{
			name: "Annotations: LargeBinary label (CH default)",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotLabel, arrow.BinaryTypes.LargeBinary,
			),
			wantMode:     timelineModeAnnotations,
			wantColLabel: 1,
		},
		{
			name: "Annotations: LargeString label",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotLabel, arrow.BinaryTypes.LargeString,
			),
			wantMode:     timelineModeAnnotations,
			wantColLabel: 1,
		},
		{
			name: "Intervals with optional lane + intensity",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotTimeEnd, tsNS(),
				timelineSlotLane, arrow.BinaryTypes.String,
				timelineSlotIntensity, arrow.PrimitiveTypes.Float64,
			),
			wantMode:       timelineModeIntervals,
			wantColTime:    0,
			wantColTimeEnd: 1,
			wantColLane:    2,
			wantColInt:     3,
		},
		{
			name: "Points with intensity (int32)",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotIntensity, arrow.PrimitiveTypes.Int32,
			),
			wantMode:    timelineModePoints,
			wantColTime: 0,
			wantColInt:  1,
		},
		{
			name: "Ambiguous: time + time_end + label",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotTimeEnd, tsMS(),
				timelineSlotLabel, arrow.BinaryTypes.String,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: "Ambiguous",
		},
		{
			name: "Orphan _tl_time_end without _tl_time",
			schema: schemaFromFields(
				timelineSlotTimeEnd, tsMS(),
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `"_tl_time_end" requires "_tl_time"`,
		},
		{
			name: "Orphan _tl_label without _tl_time",
			schema: schemaFromFields(
				timelineSlotLabel, arrow.BinaryTypes.String,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `"_tl_label" requires "_tl_time"`,
		},
		{
			name: "Orphan _tl_time_end + _tl_label both without _tl_time",
			schema: schemaFromFields(
				timelineSlotTimeEnd, tsMS(),
				timelineSlotLabel, arrow.BinaryTypes.String,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `both require "_tl_time"`,
		},
		{
			name: "No _tl_* slots at all",
			schema: schemaFromFields(
				"some_other_col", arrow.PrimitiveTypes.Int64,
				"another", arrow.BinaryTypes.String,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `expected a "_tl_time"`,
		},
		{
			name:          "Nil schema",
			schema:        nil,
			wantMode:      timelineModeNone,
			wantRejectSub: `expected a "_tl_time"`,
		},
		{
			name: "Type mismatch on _tl_time (Int64)",
			schema: schemaFromFields(
				timelineSlotTime, arrow.PrimitiveTypes.Int64,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `"_tl_time" must be a Timestamp column`,
		},
		{
			name: "Type mismatch on _tl_time_end (String)",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotTimeEnd, arrow.BinaryTypes.String,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `"_tl_time_end" must be a Timestamp column`,
		},
		{
			name: "Type mismatch on _tl_label (Int64)",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotLabel, arrow.PrimitiveTypes.Int64,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `"_tl_label" must be a String / Binary column`,
		},
		{
			name: "Type mismatch on _tl_lane (Float64)",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotTimeEnd, tsMS(),
				timelineSlotLane, arrow.PrimitiveTypes.Float64,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `"_tl_lane" must be a String / Binary column`,
		},
		{
			name: "Type mismatch on _tl_intensity (String)",
			schema: schemaFromFields(
				timelineSlotTime, tsMS(),
				timelineSlotIntensity, arrow.BinaryTypes.String,
			),
			wantMode:      timelineModeNone,
			wantRejectSub: `"_tl_intensity" must be a numeric column`,
		},
		{
			name: "Time unit preserved (nanosecond)",
			schema: schemaFromFields(
				timelineSlotTime, tsNS(),
			),
			wantMode: timelineModePoints,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := resolveContract(tt.schema)
			assert.Equal(t, tt.wantMode, ct.Mode, "Mode mismatch")
			if tt.wantRejectSub != "" {
				assert.Contains(t, ct.Reject, tt.wantRejectSub,
					"Reject string mismatch")
			} else {
				assert.Empty(t, ct.Reject, "expected no Reject when Mode != None")
			}
			if tt.wantColTime != 0 || strings.Contains(tt.name, "Points") ||
				strings.Contains(tt.name, "Intervals") ||
				strings.Contains(tt.name, "Annotations") {
				if tt.wantMode != timelineModeNone {
					assert.Equal(t, tt.wantColTime, ct.ColTime, "ColTime")
				}
			}
			if tt.wantColTimeEnd != 0 && tt.wantMode != timelineModeNone {
				assert.Equal(t, tt.wantColTimeEnd, ct.ColTimeEnd, "ColTimeEnd")
			}
			if tt.wantColLabel != 0 && tt.wantMode != timelineModeNone {
				assert.Equal(t, tt.wantColLabel, ct.ColLabel, "ColLabel")
			}
			if tt.wantColLane != 0 && tt.wantMode != timelineModeNone {
				assert.Equal(t, tt.wantColLane, ct.ColLane, "ColLane")
			}
			if tt.wantColInt != 0 && tt.wantMode != timelineModeNone {
				assert.Equal(t, tt.wantColInt, ct.ColIntensity, "ColIntensity")
			}
		})
	}
}

func TestResolveContractPreservesUnit(t *testing.T) {
	tests := []struct {
		name     string
		unit     arrow.TimeUnit
		wantUnit arrow.TimeUnit
	}{
		{"second", arrow.Second, arrow.Second},
		{"millisecond", arrow.Millisecond, arrow.Millisecond},
		{"microsecond", arrow.Microsecond, arrow.Microsecond},
		{"nanosecond", arrow.Nanosecond, arrow.Nanosecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := schemaFromFields(
				timelineSlotTime, &arrow.TimestampType{Unit: tt.unit, TimeZone: "UTC"},
				timelineSlotTimeEnd, &arrow.TimestampType{Unit: tt.unit, TimeZone: "UTC"},
			)
			ct := resolveContract(schema)
			assert.Equal(t, timelineModeIntervals, ct.Mode)
			assert.Equal(t, tt.wantUnit, ct.UnitTime)
			assert.Equal(t, tt.wantUnit, ct.UnitTimeEnd)
		})
	}
}

func TestTsToEpochMS(t *testing.T) {
	tests := []struct {
		name string
		v    int64
		unit arrow.TimeUnit
		want int64
	}{
		{"second", 1700000000, arrow.Second, 1700000000000},
		{"millisecond", 1700000000000, arrow.Millisecond, 1700000000000},
		{"microsecond", 1700000000000000, arrow.Microsecond, 1700000000000},
		{"nanosecond", 1700000000000000000, arrow.Nanosecond, 1700000000000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tsToEpochMS(tt.v, tt.unit)
			assert.Equal(t, tt.want, got)
		})
	}
}
