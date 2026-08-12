package readback

// Pins the SQL artefacts' behaviour on a row carrying TWO attributes on one
// claimed [1..1] slot — the R4 gap in
// doc/adr-background-work/leeway-components-consumer-complexity.md: the Go
// read paths refuse surplus uniformly (ADR-0146 D4, pinned in
// marshallreflect_test/arity_enforcement_test.go), but nothing asserted what
// the Projection does when used ALONE, without the Validator beside it.
//
// The pre-D4 probe had Projection taking the first match via indexOf; this
// file records the current behaviour against clickhouse-local, and pins the
// enforcement split: the Validator (and thus the Filter a Scan embeds) is
// where the CH lane refuses surplus — Projection by itself does not validate.

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// psElem / psWriter put N attributes on one verbatim membership in the symbol
// section — the tuple spelling, borrowed from arity_enforcement_test.go.
type psElem struct {
	Label string `lw:"@membership,verbatim"`
	Value string `lw:"symbol:value"`
}

type psWriter struct {
	_        struct{} `kind:"psWriter"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Elems    []psElem `lw:"symbol"`
}

// psReader is the flat component the artefacts are generated for: one
// mandatory scalar on the (symbol, health) slot.
type psReader struct {
	_        struct{} `kind:"psReader"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Health   string   `lw:"health,symbol,verbatim"`
}

func TestProjectionAlone_SurplusAttribute(t *testing.T) {
	rows := []psWriter{
		{ID: 1, Tracking: []byte("SURPLUS"), Elems: []psElem{
			{Label: "health", Value: "first"},
			{Label: "health", Value: "second"},
		}},
		{ID: 2, Tracking: []byte("CLEAN"), Elems: []psElem{
			{Label: "health", Value: "only"},
		}},
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, marshallreflect.Marshal(table, rows, nil))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	require.NotEmpty(t, recs)
	arrowPath := writeArrowFile(t, recs[0])

	plan, err := marshallreflect.PlanFor[psReader]()
	require.NoError(t, err)
	g := NewGenerator(buildAnchorIR(t), NewLookupResolver(marshallreflect.MapLookup{}))
	a, err := g.Generate(plan)
	require.NoError(t, err)

	script := HelperUDFsSQL() + "\nSELECT p.ID, p.Health, pres, val FROM (SELECT " +
		a.Projection + " AS p, " + a.Presence + " AS pres, " + a.Validator + " AS val FROM file('" +
		arrowPath + "', 'Arrow')) ORDER BY p.ID"
	out := runClickHouseLocal(t, script)

	var got [][]string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		require.Len(t, f, 4, "line %q\nscript:\n%s", line, script)
		got = append(got, f)
	}
	require.Len(t, got, 2)

	// The surplus row: presence holds (the membership is there), the Validator
	// refuses (countEqual ≠ 1) — this is where the CH lane enforces D4 — and
	// the Projection, asked anyway, takes the FIRST match. Reading a
	// projection without its Validator is reading unvalidated data.
	require.Equal(t, "1", got[0][0])
	require.Equal(t, "first", got[0][1], "Projection alone takes the first match on a surplus slot")
	require.Equal(t, "1", got[0][2], "presence holds — the membership is present")
	require.Equal(t, "0", got[0][3], "the Validator refuses the surplus row")

	// The clean row passes everything.
	require.Equal(t, []string{"2", "only", "1", "1"}, got[1])

	// The Filter (Presence AND Validator) — the WHERE body a generated Scan
	// embeds — excludes the surplus row entirely.
	countScript := HelperUDFsSQL() + "\nSELECT count() FROM file('" + arrowPath + "', 'Arrow') WHERE " + a.Filter
	require.Equal(t, "1", strings.TrimSpace(runClickHouseLocal(t, countScript)),
		"the Filter must exclude the surplus row and keep the clean one")
}

// psOptionReader is psReader with its only tagged slot widened to [0..1] —
// the R7 rung-1 evolution. Widening the kind's last REQUIRED slot leaves an
// all-optional kind, so its Presence must fall back to the disjunction of its
// slots (ADR-0066 dated update) instead of degrading to the trivial term.
type psOptionReader struct {
	_        struct{}              `kind:"psOptionReader"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Health   option.Option[string] `lw:"health,symbol,verbatim"`
}

func TestPresence_OptionOnlyKindFallsBackToDisjunction(t *testing.T) {
	plan, err := marshallreflect.PlanFor[psOptionReader]()
	require.NoError(t, err)
	g := NewGenerator(buildAnchorIR(t), NewLookupResolver(marshallreflect.MapLookup{}))
	a, err := g.Generate(plan)
	require.NoError(t, err)

	require.NotEqual(t, "1", a.Presence,
		"presence must not degrade to the trivial term — the Filter would match every row")
	require.Contains(t, a.Presence, "'health'",
		"an all-optional kind detects via the disjunction of its slots:\n%s", a.Presence)
}
